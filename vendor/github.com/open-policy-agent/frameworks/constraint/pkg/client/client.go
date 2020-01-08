package client

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/regolib"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/regorewriter"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/opa/format"
	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const constraintGroup = "constraints.gatekeeper.sh"

type UnrecognizedConstraintError struct {
	s string
}

func (e *UnrecognizedConstraintError) Error() string {
	return fmt.Sprintf("Constraint kind %s is not recognized", e.s)
}

func NewUnrecognizedConstraintError(text string) error {
	return &UnrecognizedConstraintError{text}
}

type ErrorMap map[string]error

func (e ErrorMap) Error() string {
	b := &strings.Builder{}
	for k, v := range e {
		fmt.Fprintf(b, "%s: %s\n", k, v)
	}
	return b.String()
}

type ClientOpt func(*Client) error

// Client options

var targetNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9.]*$`)

func Targets(ts ...TargetHandler) ClientOpt {
	return func(c *Client) error {
		var errs Errors
		handlers := make(map[string]TargetHandler, len(ts))
		for _, t := range ts {
			if t.GetName() == "" {
				errs = append(errs, errors.New("Invalid target: a target is returning an empty string for GetName()"))
			} else if !targetNameRegex.MatchString(t.GetName()) {
				errs = append(errs, fmt.Errorf("Target name \"%s\" is not of the form %s", t.GetName(), targetNameRegex.String()))
			} else {
				handlers[t.GetName()] = t
			}
		}
		c.targets = handlers
		if len(errs) > 0 {
			return errs
		}
		return nil
	}
}

// AllowedDataFields sets the fields under `data` that Rego in ConstraintTemplates
// can access. If unset, all fields can be accessed. Only fields recognized by
// the system can be enabled.
func AllowedDataFields(fields ...string) ClientOpt {
	return func(c *Client) error {
		c.allowedDataFields = fields
		return nil
	}
}

type constraintEntry struct {
	CRD     *apiextensions.CustomResourceDefinition
	Targets []string
}

type Client struct {
	backend           *Backend
	targets           map[string]TargetHandler
	constraintsMux    sync.RWMutex
	constraints       map[string]*constraintEntry
	allowedDataFields []string
}

// createDataPath compiles the data destination: data.external.<target>.<path>
func createDataPath(target, subpath string) string {
	subpaths := strings.Split(subpath, "/")
	p := []string{"external", target}
	p = append(p, subpaths...)

	return "/" + path.Join(p...)
}

// AddData inserts the provided data into OPA for every target that can handle the data.
func (c *Client) AddData(ctx context.Context, data interface{}) (*types.Responses, error) {
	resp := types.NewResponses()
	errMap := make(ErrorMap)
	for target, h := range c.targets {
		handled, path, processedData, err := h.ProcessData(data)
		if err != nil {
			errMap[target] = err
			continue
		}
		if !handled {
			continue
		}
		if err := c.backend.driver.PutData(ctx, createDataPath(target, path), processedData); err != nil {
			errMap[target] = err
			continue
		}
		resp.Handled[target] = true
	}
	if len(errMap) == 0 {
		return resp, nil
	}
	return resp, errMap
}

// RemoveData removes data from OPA for every target that can handle the data.
func (c *Client) RemoveData(ctx context.Context, data interface{}) (*types.Responses, error) {
	resp := types.NewResponses()
	errMap := make(ErrorMap)
	for target, h := range c.targets {
		handled, path, _, err := h.ProcessData(data)
		if err != nil {
			errMap[target] = err
			continue
		}
		if !handled {
			continue
		}
		if _, err := c.backend.driver.DeleteData(ctx, createDataPath(target, path)); err != nil {
			errMap[target] = err
			continue
		}
		resp.Handled[target] = true
	}
	if len(errMap) == 0 {
		return resp, nil
	}
	return resp, errMap
}

// createTemplatePath returns the package path for a given template: templates.<target>.<name>
func createTemplatePath(target, name string) string {
	return fmt.Sprintf(`templates["%s"]["%s"]`, target, name)
}

// templateLibPrefix returns the new lib prefix for the libs that are specified in the CT.
func templateLibPrefix(target, name string) string {
	return fmt.Sprintf("libs.%s.%s", target, name)
}

// validateTargets handles validating the targets section of the CT.
func (c *Client) validateTargets(templ *templates.ConstraintTemplate) (*templates.Target, TargetHandler, error) {
	if err := validateTargets(templ); err != nil {
		return nil, nil, err
	}

	if len(templ.Spec.Targets) != 1 {
		return nil, nil, errors.Errorf("expected exactly 1 item in targets, got %v", templ.Spec.Targets)
	}

	targetSpec := &templ.Spec.Targets[0]
	targetHandler, found := c.targets[targetSpec.Target]
	if !found {
		return nil, nil, fmt.Errorf("target %s not recognized", targetSpec.Target)
	}

	return targetSpec, targetHandler, nil
}

// constraintTemplateArtifacts are the artifacts generated during validation / crd creation / rewrite
// for the constraint template.
type constraintTemplateArtifacts struct {
	// crd is the CustomResourceDefinition created from the CT.
	crd *apiextensions.CustomResourceDefinition

	// modules is the rewritten set of modules that the constraint template declares in Rego and Libs
	modules []string

	// namePrefix is the name prefix by which the modules will be identified during create / delete
	// calls to the drivers.Driver interface.
	namePrefix string

	// targetHandler is the target handler indicated by the CT.  This isn't generated, but is used by
	// consumers of createTemplateArtifacts
	targetHandler TargetHandler
}

// createTemplateArtifacts will validate the CT, create the CRD for the CT's constraints, then
// validate and rewrite the rego sources specified in the CT.
func (c *Client) createTemplateArtifacts(templ *templates.ConstraintTemplate) (*constraintTemplateArtifacts, error) {
	if templ.ObjectMeta.Name == "" {
		return nil, errors.New("Template has no name")
	}
	if templ.ObjectMeta.Name != strings.ToLower(templ.Spec.CRD.Spec.Names.Kind) {
		return nil, fmt.Errorf("Template's name %s is not equal to the lowercase of CRD's Kind: %s", templ.ObjectMeta.Name, strings.ToLower(templ.Spec.CRD.Spec.Names.Kind))
	}

	targetSpec, targetHandler, err := c.validateTargets(templ)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to validate targets for template %s", templ.Name)
	}

	schema, err := c.backend.crd.createSchema(templ, targetHandler)
	if err != nil {
		return nil, err
	}
	crd, err := c.backend.crd.createCRD(templ, schema)
	if err != nil {
		return nil, err
	}
	if err = c.backend.crd.validateCRD(crd); err != nil {
		return nil, err
	}

	var externs []string
	for _, field := range c.allowedDataFields {
		externs = append(externs, fmt.Sprintf("data.%s", field))
	}

	libPrefix := templateLibPrefix(targetHandler.GetName(), crd.Spec.Names.Kind)
	rr, err := regorewriter.New(
		regorewriter.NewPackagePrefixer(libPrefix),
		[]string{"data.lib"},
		externs)
	if err != nil {
		return nil, err
	}

	entryPointPath := createTemplatePath(targetHandler.GetName(), crd.Spec.Names.Kind)

	entryPoint, err := parseModule(entryPointPath, targetSpec.Rego)
	if err != nil {
		return nil, err
	}
	if entryPoint == nil {
		return nil, errors.Errorf("Failed to parse module for unknown reason")
	}

	if err := rewriteModulePackage(entryPointPath, entryPoint); err != nil {
		return nil, err
	}

	req := ruleArities{
		"violation": 1,
	}
	if err := requireRulesModule(entryPoint, req); err != nil {
		return nil, fmt.Errorf("Invalid rego: %s", err)
	}

	rr.AddEntryPointModule(entryPointPath, entryPoint)
	for idx, libSrc := range targetSpec.Libs {
		libPath := fmt.Sprintf(`%s["lib_%d"]`, libPrefix, idx)
		if err := rr.AddLib(libPath, libSrc); err != nil {
			return nil, err
		}
	}

	sources, err := rr.Rewrite()
	if err != nil {
		return nil, err
	}

	var mods []string
	if err := sources.ForEachModule(func(m *regorewriter.Module) error {
		content, err := m.Content()
		if err != nil {
			return err
		}
		mods = append(mods, string(content))
		return nil
	}); err != nil {
		return nil, err
	}

	return &constraintTemplateArtifacts{
		crd:           crd,
		targetHandler: targetHandler,
		namePrefix:    entryPointPath,
		modules:       mods,
	}, nil
}

// CreateCRD creates a CRD from template
func (c *Client) CreateCRD(ctx context.Context, templ *templates.ConstraintTemplate) (*apiextensions.CustomResourceDefinition, error) {
	artifacts, err := c.createTemplateArtifacts(templ)
	if err != nil {
		return nil, err
	}
	return artifacts.crd, nil
}

// AddTemplate adds the template source code to OPA and registers the CRD with the client for
// schema validation on calls to AddConstraint. It also returns a copy of the CRD describing
// the constraint.
func (c *Client) AddTemplate(ctx context.Context, templ *templates.ConstraintTemplate) (*types.Responses, error) {
	resp := types.NewResponses()

	artifacts, err := c.createTemplateArtifacts(templ)
	if err != nil {
		return resp, err
	}

	c.constraintsMux.Lock()
	defer c.constraintsMux.Unlock()

	if err := c.backend.driver.PutModules(ctx, artifacts.namePrefix, artifacts.modules); err != nil {
		return resp, err
	}

	c.constraints[c.constraintsMapKey(artifacts)] = &constraintEntry{
		CRD:     artifacts.crd,
		Targets: []string{artifacts.targetHandler.GetName()},
	}
	resp.Handled[artifacts.targetHandler.GetName()] = true
	return resp, nil
}

// RemoveTemplate removes the template source code from OPA and removes the CRD from the validation
// registry.
func (c *Client) RemoveTemplate(ctx context.Context, templ *templates.ConstraintTemplate) (*types.Responses, error) {
	resp := types.NewResponses()

	artifacts, err := c.createTemplateArtifacts(templ)
	if err != nil {
		return resp, err
	}

	c.constraintsMux.Lock()
	defer c.constraintsMux.Unlock()

	if _, err := c.backend.driver.DeleteModules(ctx, artifacts.namePrefix); err != nil {
		return resp, err
	}

	delete(c.constraints, c.constraintsMapKey(artifacts))
	resp.Handled[artifacts.targetHandler.GetName()] = true
	return resp, nil
}

// constraintsMapKey returns the key for where we will track the constraint template in
// the constraints map.
func (c *Client) constraintsMapKey(artifacts *constraintTemplateArtifacts) string {
	return artifacts.crd.Spec.Names.Kind
}

// createConstraintPath returns the storage path for a given constraint: constraints.<target>.cluster.<group>.<version>.<kind>.<name>
func createConstraintPath(target string, constraint *unstructured.Unstructured) (string, error) {
	if constraint.GetName() == "" {
		return "", errors.New("Constraint has no name")
	}
	gvk := constraint.GroupVersionKind()
	if gvk.Group == "" {
		return "", fmt.Errorf("Empty group for the constrant named %s", constraint.GetName())
	}
	if gvk.Kind == "" {
		return "", fmt.Errorf("Empty kind for the constraint named %s", constraint.GetName())
	}
	return "/" + path.Join("constraints", target, "cluster", gvk.Group, gvk.Kind, constraint.GetName()), nil
}

// getConstraintEntry returns the constraint entry for a given constraint
func (c *Client) getConstraintEntry(constraint *unstructured.Unstructured, lock bool) (*constraintEntry, error) {
	kind := constraint.GetKind()
	if kind == "" {
		return nil, fmt.Errorf("Constraint %s has no kind", constraint.GetName())
	}
	if lock {
		c.constraintsMux.RLock()
		defer c.constraintsMux.RUnlock()
	}
	entry, ok := c.constraints[kind]
	if !ok {
		return nil, NewUnrecognizedConstraintError(kind)
	}
	return entry, nil
}

// AddConstraint validates the constraint and, if valid, inserts it into OPA
func (c *Client) AddConstraint(ctx context.Context, constraint *unstructured.Unstructured) (*types.Responses, error) {
	c.constraintsMux.RLock()
	defer c.constraintsMux.RUnlock()
	resp := types.NewResponses()
	errMap := make(ErrorMap)
	if err := c.validateConstraint(constraint, false); err != nil {
		return resp, err
	}
	entry, err := c.getConstraintEntry(constraint, false)
	if err != nil {
		return resp, err
	}
	for _, target := range entry.Targets {
		path, err := createConstraintPath(target, constraint)
		// If we ever create multi-target constraints we will need to handle this more cleverly.
		// the short-circuiting question, cleanup, etc.
		if err != nil {
			errMap[target] = err
			continue
		}
		if err := c.backend.driver.PutData(ctx, path, constraint.Object); err != nil {
			errMap[target] = err
			continue
		}
		resp.Handled[target] = true
	}
	if len(errMap) == 0 {
		return resp, nil
	}
	return resp, errMap
}

// RemoveConstraint removes a constraint from OPA
func (c *Client) RemoveConstraint(ctx context.Context, constraint *unstructured.Unstructured) (*types.Responses, error) {
	c.constraintsMux.RLock()
	defer c.constraintsMux.RUnlock()
	resp := types.NewResponses()
	errMap := make(ErrorMap)
	entry, err := c.getConstraintEntry(constraint, false)
	if err != nil {
		return resp, err
	}
	for _, target := range entry.Targets {
		path, err := createConstraintPath(target, constraint)
		// If we ever create multi-target constraints we will need to handle this more cleverly.
		// the short-circuiting question, cleanup, etc.
		if err != nil {
			errMap[target] = err
			continue
		}
		if _, err := c.backend.driver.DeleteData(ctx, path); err != nil {
			errMap[target] = err
		}
		resp.Handled[target] = true
	}
	if len(errMap) == 0 {
		return resp, nil
	}
	return resp, errMap
}

// validateConstraint is an internal function that allows us to toggle whether we use a read lock
// when validating a constraint
func (c *Client) validateConstraint(constraint *unstructured.Unstructured, lock bool) error {
	entry, err := c.getConstraintEntry(constraint, lock)
	if err != nil {
		return err
	}
	if err = c.backend.crd.validateCR(constraint, entry.CRD); err != nil {
		return err
	}

	for _, target := range entry.Targets {
		if err := c.targets[target].ValidateConstraint(constraint); err != nil {
			return err
		}
	}
	return nil
}

// ValidateConstraint returns an error if the constraint is not recognized or does not conform to
// the registered CRD for that constraint.
func (c *Client) ValidateConstraint(ctx context.Context, constraint *unstructured.Unstructured) error {
	return c.validateConstraint(constraint, true)
}

// init initializes the OPA backend for the client
func (c *Client) init() error {
	for _, t := range c.targets {
		hooks := fmt.Sprintf(`hooks["%s"]`, t.GetName())
		templMap := map[string]string{"Target": t.GetName()}

		libBuiltin := &bytes.Buffer{}
		if err := regolib.TargetLib.Execute(libBuiltin, templMap); err != nil {
			return err
		}
		if err := c.backend.driver.PutModule(
			context.Background(),
			fmt.Sprintf("%s.hooks_builtin", hooks),
			libBuiltin.String()); err != nil {
			return err
		}

		libTempl := t.Library()
		if libTempl == nil {
			return fmt.Errorf("Target %s has no Rego library template", t.GetName())
		}
		libBuf := &bytes.Buffer{}
		if err := libTempl.Execute(libBuf, map[string]string{
			"ConstraintsRoot": fmt.Sprintf(`data.constraints["%s"].cluster["%s"]`, t.GetName(), constraintGroup),
			"DataRoot":        fmt.Sprintf(`data.external["%s"]`, t.GetName()),
		}); err != nil {
			return err
		}
		lib := libBuf.String()
		req := ruleArities{
			"autoreject_review":                1,
			"matching_reviews_and_constraints": 2,
			"matching_constraints":             1,
		}
		path := fmt.Sprintf("%s.library", hooks)
		libModule, err := parseModule(path, lib)
		if err != nil {
			return errors.Wrapf(err, "failed to parse module")
		}
		if err := requireRulesModule(libModule, req); err != nil {
			return fmt.Errorf("Problem with the below Rego for %s target:\n\n====%s\n====\n%s", t.GetName(), lib, err)
		}
		err = rewriteModulePackage(path, libModule)
		if err != nil {
			return err
		}
		src, err := format.Ast(libModule)
		if err != nil {
			return fmt.Errorf("Could not re-format Rego source: %v", err)
		}
		if err := c.backend.driver.PutModule(context.Background(), path, string(src)); err != nil {
			return fmt.Errorf("Error %s from compiled source:\n%s", err, src)
		}
	}

	return nil
}

// Reset the state of OPA
func (c *Client) Reset(ctx context.Context) error {
	c.constraintsMux.Lock()
	defer c.constraintsMux.Unlock()
	for name := range c.targets {
		if _, err := c.backend.driver.DeleteData(ctx, fmt.Sprintf("/external/%s", name)); err != nil {
			return err
		}
		if _, err := c.backend.driver.DeleteData(ctx, fmt.Sprintf("/constraints/%s", name)); err != nil {
			return err
		}
	}
	for name, v := range c.constraints {
		for _, t := range v.Targets {
			if _, err := c.backend.driver.DeleteModule(ctx, fmt.Sprintf(`templates["%s"]["%s"]`, t, name)); err != nil {
				return err
			}
		}
	}
	c.constraints = make(map[string]*constraintEntry)
	return nil
}

type queryCfg struct {
	enableTracing bool
}

type QueryOpt func(*queryCfg)

func Tracing(enabled bool) QueryOpt {
	return func(cfg *queryCfg) {
		cfg.enableTracing = enabled
	}
}

// Review makes sure the provided object satisfies all stored constraints
func (c *Client) Review(ctx context.Context, obj interface{}, opts ...QueryOpt) (*types.Responses, error) {
	cfg := &queryCfg{}
	for _, opt := range opts {
		opt(cfg)
	}
	responses := types.NewResponses()
	errMap := make(ErrorMap)
TargetLoop:
	for name, target := range c.targets {
		handled, review, err := target.HandleReview(obj)
		// Short-circuiting question applies here as well
		if err != nil {
			errMap[name] = err
			continue
		}
		if !handled {
			continue
		}
		input := map[string]interface{}{"review": review}
		resp, err := c.backend.driver.Query(ctx, fmt.Sprintf(`hooks["%s"].violation`, name), input, drivers.Tracing(cfg.enableTracing))
		if err != nil {
			errMap[name] = err
			continue
		}
		for _, r := range resp.Results {
			if err := target.HandleViolation(r); err != nil {
				errMap[name] = err
				continue TargetLoop
			}
		}
		resp.Target = name
		responses.ByTarget[name] = resp
	}
	if len(errMap) == 0 {
		return responses, nil
	}
	return responses, errMap
}

// Audit makes sure the cached state of the system satisfies all stored constraints
func (c *Client) Audit(ctx context.Context, opts ...QueryOpt) (*types.Responses, error) {
	cfg := &queryCfg{}
	for _, opt := range opts {
		opt(cfg)
	}
	responses := types.NewResponses()
	errMap := make(ErrorMap)
TargetLoop:
	for name, target := range c.targets {
		// Short-circuiting question applies here as well
		resp, err := c.backend.driver.Query(ctx, fmt.Sprintf(`hooks["%s"].audit`, name), nil, drivers.Tracing(cfg.enableTracing))
		if err != nil {
			errMap[name] = err
			continue
		}
		for _, r := range resp.Results {
			if err := target.HandleViolation(r); err != nil {
				errMap[name] = err
				continue TargetLoop
			}
		}
		resp.Target = name
		responses.ByTarget[name] = resp
	}
	if len(errMap) == 0 {
		return responses, nil
	}
	return responses, errMap
}

// Dump dumps the state of OPA to aid in debugging
func (c *Client) Dump(ctx context.Context) (string, error) {
	return c.backend.driver.Dump(ctx)
}
