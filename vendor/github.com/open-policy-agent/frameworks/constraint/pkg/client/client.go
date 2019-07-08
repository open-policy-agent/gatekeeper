package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"sync"
	"text/template"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1alpha1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/regolib"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const constraintGroup = "constraints.gatekeeper.sh"

type Client interface {
	AddData(context.Context, interface{}) (*types.Responses, error)
	RemoveData(context.Context, interface{}) (*types.Responses, error)

	CreateCRD(context.Context, *v1alpha1.ConstraintTemplate) (*apiextensionsv1beta1.CustomResourceDefinition, error)
	AddTemplate(context.Context, *v1alpha1.ConstraintTemplate) (*types.Responses, error)
	RemoveTemplate(context.Context, *v1alpha1.ConstraintTemplate) (*types.Responses, error)

	AddConstraint(context.Context, *unstructured.Unstructured) (*types.Responses, error)
	RemoveConstraint(context.Context, *unstructured.Unstructured) (*types.Responses, error)
	ValidateConstraint(context.Context, *unstructured.Unstructured) error

	// Reset the state of OPA
	Reset(context.Context) error

	// Review makes sure the provided object satisfies all stored constraints
	Review(context.Context, interface{}, ...QueryOpt) (*types.Responses, error)

	// Audit makes sure the cached state of the system satisfies all stored constraints
	Audit(context.Context, ...QueryOpt) (*types.Responses, error)

	// Dump dumps the state of OPA to aid in debugging
	Dump(context.Context) (string, error)
}

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

type ClientOpt func(*client) error

// Client options

var targetNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9.]*$`)

func Targets(ts ...TargetHandler) ClientOpt {
	return func(c *client) error {
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

type MatchSchemaProvider interface {
	// MatchSchema returns the JSON Schema for the `match` field of a constraint
	MatchSchema() apiextensionsv1beta1.JSONSchemaProps
}

type TargetHandler interface {
	MatchSchemaProvider

	GetName() string

	// Library returns the pieces of Rego code required to stitch together constraint evaluation
	// for the target. Current required libraries are `matching_constraints` and
	// `matching_reviews_and_constraints`
	//
	// Libraries are currently templates that have the following parameters:
	//   ConstraintsRoot: The root path under which all constraints for the target are stored
	//   DataRoot: The root path under which all data for the target is stored
	Library() *template.Template

	// ProcessData takes a potential data object and returns:
	//   true if the target handles the data type
	//   the path under which the data should be stored in OPA
	//   the data in an object that can be cast into JSON, suitable for storage in OPA
	ProcessData(interface{}) (bool, string, interface{}, error)

	// HandleReview takes a potential review request and builds the `review` field of the input
	// object. it returns:
	//		true if the target handles the data type
	//		the data for the `review` field
	HandleReview(interface{}) (bool, interface{}, error)

	// HandleViolation allows for post-processing of the result object, which can be mutated directly
	HandleViolation(result *types.Result) error

	// ValidateConstraint returns if the constraint is misconfigured in any way. This allows for
	// non-trivial validation of things like match schema
	ValidateConstraint(*unstructured.Unstructured) error
}

var _ Client = &client{}

type constraintEntry struct {
	CRD     *apiextensionsv1beta1.CustomResourceDefinition
	Targets []string
}

type client struct {
	backend        *Backend
	targets        map[string]TargetHandler
	constraintsMux sync.RWMutex
	constraints    map[string]*constraintEntry
}

// createDataPath compiles the data destination: data.external.<target>.<path>
func createDataPath(target, subpath string) string {
	subpaths := strings.Split(subpath, "/")
	p := []string{"external", target}
	p = append(p, subpaths...)

	return "/" + path.Join(p...)
}

// AddData inserts the provided data into OPA for every target that can handle the data.
func (c *client) AddData(ctx context.Context, data interface{}) (*types.Responses, error) {
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
func (c *client) RemoveData(ctx context.Context, data interface{}) (*types.Responses, error) {
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

// CreateCRD creates a CRD from template
func (c *client) CreateCRD(ctx context.Context, templ *v1alpha1.ConstraintTemplate) (*apiextensionsv1beta1.CustomResourceDefinition, error) {
	if err := validateTargets(templ); err != nil {
		return nil, err
	}
	if templ.ObjectMeta.Name == "" {
		return nil, errors.New("Template has no name")
	}
	if templ.ObjectMeta.Name != templ.Spec.CRD.Spec.Names.Plural {
		return nil, fmt.Errorf("Template's name %s is not equal to the CRD's plural name: %s", templ.ObjectMeta.Name, templ.Spec.CRD.Spec.Names.Plural)
	}
	if templ.ObjectMeta.Name != strings.ToLower(templ.Spec.CRD.Spec.Names.Kind) {
		return nil, fmt.Errorf("Template's name %s is not equal to the lowercase of CRD's Kind: %s", templ.ObjectMeta.Name, strings.ToLower(templ.Spec.CRD.Spec.Names.Kind))
	}

	var src string
	var target TargetHandler
	for _, v := range templ.Spec.Targets {
		k := v.Target
		t, ok := c.targets[k]
		if !ok {
			return nil, fmt.Errorf("Target %s not recognized", k)
		}
		target = t
		src = v.Rego
	}

	schema := createSchema(templ, target)
	crd := c.backend.crd.createCRD(templ, schema)
	if err := c.backend.crd.validateCRD(crd); err != nil {
		return nil, err
	}

	path := createTemplatePath(target.GetName(), crd.Spec.Names.Kind)
	_, err := ensureRegoConformance(crd.Spec.Names.Kind, path, src)
	if err != nil {
		return nil, err
	}

	return crd, nil
}

// AddTemplate adds the template source code to OPA and registers the CRD with the client for
// schema validation on calls to AddConstraint. It also returns a copy of the CRD describing
// the constraint.
func (c *client) AddTemplate(ctx context.Context, templ *v1alpha1.ConstraintTemplate) (*types.Responses, error) {
	resp := types.NewResponses()
	crd, err := c.CreateCRD(ctx, templ)
	if err != nil {
		return resp, err
	}

	var src string
	var target TargetHandler
	for _, v := range templ.Spec.Targets {
		k := v.Target
		t, ok := c.targets[k]
		if !ok {
			return resp, fmt.Errorf("Target %s not recognized", k)
		}
		target = t
		src = v.Rego
	}

	path := createTemplatePath(target.GetName(), crd.Spec.Names.Kind)
	conformingSrc, err := ensureRegoConformance(crd.Spec.Names.Kind, path, src)
	if err != nil {
		return resp, err
	}

	c.constraintsMux.Lock()
	defer c.constraintsMux.Unlock()
	if err := c.backend.driver.PutModule(ctx, path, conformingSrc); err != nil {
		return resp, err
	}

	c.constraints[crd.Spec.Names.Kind] = &constraintEntry{CRD: crd, Targets: []string{target.GetName()}}
	resp.Handled[target.GetName()] = true

	return resp, nil
}

// RemoveTemplate removes the template source code from OPA and removes the CRD from the validation
// registry.
func (c *client) RemoveTemplate(ctx context.Context, templ *v1alpha1.ConstraintTemplate) (*types.Responses, error) {
	resp := types.NewResponses()
	if err := validateTargets(templ); err != nil {
		return resp, err
	}

	var target TargetHandler
	for _, v := range templ.Spec.Targets {
		k := v.Target
		t, ok := c.targets[k]
		if !ok {
			return resp, fmt.Errorf("Target %s not recognized", k)
		}
		target = t
	}

	schema := createSchema(templ, target)
	crd := c.backend.crd.createCRD(templ, schema)
	if err := c.backend.crd.validateCRD(crd); err != nil {
		return resp, err
	}

	path := createTemplatePath(target.GetName(), templ.Spec.CRD.Spec.Names.Kind)

	c.constraintsMux.Lock()
	defer c.constraintsMux.Unlock()
	_, err := c.backend.driver.DeleteModule(ctx, path)
	if err != nil {
		return resp, err
	}
	delete(c.constraints, crd.Spec.Names.Kind)
	resp.Handled[target.GetName()] = true
	return resp, nil
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
	if gvk.Version == "" {
		return "", fmt.Errorf("Empty version for the constraint named %s", constraint.GetName())
	}
	if gvk.Kind == "" {
		return "", fmt.Errorf("Empty kind for the constraint named %s", constraint.GetName())
	}
	return "/" + path.Join("constraints", target, "cluster", gvk.Group, gvk.Version, gvk.Kind, constraint.GetName()), nil
}

// getConstraintEntry returns the constraint entry for a given constraint
func (c *client) getConstraintEntry(constraint *unstructured.Unstructured, lock bool) (*constraintEntry, error) {
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
func (c *client) AddConstraint(ctx context.Context, constraint *unstructured.Unstructured) (*types.Responses, error) {
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
func (c *client) RemoveConstraint(ctx context.Context, constraint *unstructured.Unstructured) (*types.Responses, error) {
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
func (c *client) validateConstraint(constraint *unstructured.Unstructured, lock bool) error {
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
func (c *client) ValidateConstraint(ctx context.Context, constraint *unstructured.Unstructured) error {
	return c.validateConstraint(constraint, true)
}

// init initializes the OPA backend for the client
func (c *client) init() error {
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
			"ConstraintsRoot": fmt.Sprintf(`data.constraints["%s"].cluster["%s"].v1alpha1`, t.GetName(), constraintGroup),
			"DataRoot":        fmt.Sprintf(`data.external["%s"]`, t.GetName()),
		}); err != nil {
			return err
		}
		lib := libBuf.String()
		req := ruleArities{
			"matching_reviews_and_constraints": 2,
			"matching_constraints":             1,
		}
		if err := requireRules(fmt.Sprintf("%s_libraries", t.GetName()), lib, req); err != nil {
			return fmt.Errorf("Problem with the below Rego for %s target:\n\n====%s\n====\n%s", t.GetName(), lib, err)
		}
		path := fmt.Sprintf("%s.library", hooks)
		src, err := rewritePackage(path, lib)
		if err != nil {
			return err
		}
		if err := c.backend.driver.PutModule(context.Background(), path, src); err != nil {
			return fmt.Errorf("Error %s from compiled source:\n%s", err, src)
		}
	}

	return nil
}

func (c *client) Reset(ctx context.Context) error {
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

func (c *client) Review(ctx context.Context, obj interface{}, opts ...QueryOpt) (*types.Responses, error) {
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

func (c *client) Audit(ctx context.Context, opts ...QueryOpt) (*types.Responses, error) {
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

func (c *client) Dump(ctx context.Context) (string, error) {
	return c.backend.driver.Dump(ctx)
}
