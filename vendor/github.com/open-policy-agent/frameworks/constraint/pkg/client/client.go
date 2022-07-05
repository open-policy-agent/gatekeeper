package client

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/crds"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	clienterrors "github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/handler"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const statusField = "status"

// Client tracks ConstraintTemplates and Constraints for a set of Targets.
// Allows validating reviews against Constraints.
//
// Threadsafe. Does not support concurrent mutation operations.
//
// Note that adding per-identifier locking would not fix this completely - the
// thread for the first-sent call could be put to sleep while the second is
// allowed to continue running. Thus, this problem can only safely be handled
// by the caller.
type Client struct {
	// driver contains the Rego runtime environments to run queries against.
	// Does not require mutex locking as Driver is threadsafe.
	driver drivers.Driver
	// targets are the targets supported by this Client.
	// Assumed to be constant after initialization.
	targets map[string]handler.TargetHandler

	// mtx guards reading and writing data outside of Driver.
	mtx sync.RWMutex

	// templates is a map from a Template's name to its entry.
	templates map[string]*templateClient
}

// CreateCRD creates a CRD from template.
func (c *Client) CreateCRD(ctx context.Context, templ *templates.ConstraintTemplate) (*apiextensions.CustomResourceDefinition, error) {
	if templ == nil {
		return nil, fmt.Errorf("%w: got nil ConstraintTemplate",
			clienterrors.ErrInvalidConstraintTemplate)
	}

	err := validateTemplateMetadata(templ)
	if err != nil {
		return nil, err
	}

	target, err := c.getTargetHandler(templ)
	if err != nil {
		return nil, err
	}

	return createCRD(ctx, templ, target)
}

// AddTemplate adds the template source code to OPA and registers the CRD with the client for
// schema validation on calls to AddConstraint. On error, the responses return value
// will still be populated so that partial results can be analyzed.
func (c *Client) AddTemplate(ctx context.Context, templ *templates.ConstraintTemplate) (*types.Responses, error) {
	resp := types.NewResponses()

	c.mtx.Lock()
	defer c.mtx.Unlock()

	// Return immediately if no change.
	targetName, err := getTargetName(templ)
	if err != nil {
		return resp, err
	}

	var cachedCpy *templates.ConstraintTemplate
	hasConstraints := false
	var oldTargets []string

	cached := c.templates[templ.GetName()]
	if cached != nil {
		cachedCpy = cached.getTemplate()
		hasConstraints = len(cached.constraints) > 0
		for _, target := range cached.targets {
			oldTargets = append(oldTargets, target.GetName())
		}
	}

	if cachedCpy != nil && cachedCpy.SemanticEqual(templ) {
		resp.Handled[targetName] = true
		return resp, nil
	}

	if hasConstraints {
		var newTargets []string
		for _, target := range templ.Spec.Targets {
			newTargets = append(newTargets, target.Target)
		}

		if len(oldTargets) != len(newTargets) {
			return resp, fmt.Errorf("%w: old targets %v, new targets %v",
				clienterrors.ErrChangeTargets, oldTargets, newTargets)
		}

		sort.Strings(oldTargets)
		sort.Strings(newTargets)

		for i, target := range oldTargets {
			if target != newTargets[i] {
				return resp, fmt.Errorf("%w: old targets %v, new targets %v",
					clienterrors.ErrChangeTargets, oldTargets, newTargets)
			}
		}
	}

	err = validateTemplateMetadata(templ)
	if err != nil {
		return resp, err
	}

	target, err := c.getTargetHandler(templ)
	if err != nil {
		return resp, err
	}

	crd, err := createCRD(ctx, templ, target)
	if err != nil {
		return resp, err
	}

	if err := c.driver.AddTemplate(ctx, templ); err != nil {
		return resp, err
	}

	templateName := templ.GetName()

	template := c.templates[templateName]

	// We don't want to use the usual "if found/ok" idiom here - if the value
	// stored for templateName is nil, we need to update it to be non-nil to avoid
	// a panic.
	if template == nil {
		template = &templateClient{
			constraints: make(map[string]*constraintClient),
		}

		c.templates[templateName] = template
	}

	// This state mutation needs to happen last so that the semantic equal check
	// at the beginning does not incorrectly return true when updating did not
	// succeed previously.
	template.Update(templ, crd, target)

	resp.Handled[targetName] = true
	return resp, nil
}

func getTargetName(templ *templates.ConstraintTemplate) (string, error) {
	targets := templ.Spec.Targets

	if len(targets) != 1 {
		return "", fmt.Errorf("%w: must declare exactly one target",
			clienterrors.ErrInvalidConstraintTemplate)
	}

	if targets[0].Target == "" {
		return "", fmt.Errorf("%w: target name must not be empty",
			clienterrors.ErrInvalidConstraintTemplate)
	}

	return targets[0].Target, nil
}

// RemoveTemplate removes the template source code from OPA and removes the CRD from the validation
// registry. Any constraints relying on the template will also be removed.
// On error, the responses return value will still be populated so that
// partial results can be analyzed.
func (c *Client) RemoveTemplate(ctx context.Context, templ *templates.ConstraintTemplate) (*types.Responses, error) {
	resp := types.NewResponses()

	c.mtx.Lock()
	defer c.mtx.Unlock()

	err := c.driver.RemoveTemplate(ctx, templ)
	if err != nil {
		return resp, err
	}

	name := templ.GetName()

	template, found := c.templates[name]

	if !found {
		return resp, nil
	}

	delete(c.templates, name)

	for _, target := range template.targets {
		resp.Handled[target.GetName()] = true
	}

	return resp, nil
}

func templateNotFound(name string) error {
	return fmt.Errorf("%w: template %q not found",
		ErrMissingConstraintTemplate, name)
}

// GetTemplate gets the currently recognized template.
func (c *Client) GetTemplate(templ *templates.ConstraintTemplate) (*templates.ConstraintTemplate, error) {
	name := templ.GetName()

	c.mtx.RLock()
	defer c.mtx.RUnlock()

	template := c.templates[name]
	if template == nil {
		return nil, templateNotFound(name)
	}

	return template.getTemplate(), nil
}

// getTemplateEntry returns the template entry for a given constraint.
func (c *Client) getTemplateForKind(kind string) *templateClient {
	name := strings.ToLower(kind)

	return c.templates[name]
}

// AddConstraint validates the constraint and, if valid, inserts it into OPA.
// On error, the responses return value will still be populated so that
// partial results can be analyzed.
func (c *Client) AddConstraint(ctx context.Context, constraint *unstructured.Unstructured) (*types.Responses, error) {
	resp := types.NewResponses()

	c.mtx.Lock()
	defer c.mtx.Unlock()

	err := c.validateConstraint(constraint)
	if err != nil {
		return resp, err
	}

	kind := constraint.GetKind()
	template := c.getTemplateForKind(kind)
	if template == nil {
		templateName := strings.ToLower(kind)
		return resp, templateNotFound(templateName)
	}

	changed, err := template.AddConstraint(constraint)
	if err != nil {
		return resp, err
	}

	if changed {
		err = c.driver.AddConstraint(ctx, constraint)
		if err != nil {
			return resp, err
		}
	}

	for _, target := range template.targets {
		resp.Handled[target.GetName()] = true
	}

	return resp, nil
}

// RemoveConstraint removes a constraint from OPA. On error, the responses
// return value will still be populated so that partial results can be analyzed.
func (c *Client) RemoveConstraint(ctx context.Context, constraint *unstructured.Unstructured) (*types.Responses, error) {
	resp := types.NewResponses()

	c.mtx.Lock()
	defer c.mtx.Unlock()

	err := validateConstraintMetadata(constraint)
	if err != nil {
		return resp, err
	}

	err = c.driver.RemoveConstraint(ctx, constraint)
	if err != nil {
		return nil, err
	}

	kind := constraint.GetKind()

	template := c.getTemplateForKind(kind)
	if template == nil {
		// The Template has been deleted, so nothing to do and no reason to return
		// error.
		return resp, nil
	}

	for _, target := range template.targets {
		resp.Handled[target.GetName()] = true
	}

	template.RemoveConstraint(constraint.GetName())

	return resp, nil
}

// GetConstraint gets the currently recognized constraint.
func (c *Client) GetConstraint(constraint *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	err := validateConstraintMetadata(constraint)
	if err != nil {
		return nil, err
	}

	c.mtx.RLock()
	defer c.mtx.RUnlock()

	kind := constraint.GetKind()
	template := c.getTemplateForKind(kind)
	if template == nil {
		templateName := strings.ToLower(kind)
		return nil, templateNotFound(templateName)
	}

	return template.GetConstraint(constraint.GetName())
}

func validateConstraintMetadata(constraint *unstructured.Unstructured) error {
	if constraint.GetName() == "" {
		return fmt.Errorf("%w: missing metadata.name", apiconstraints.ErrInvalidConstraint)
	}

	gk := constraint.GroupVersionKind()
	if gk.Kind == "" {
		return fmt.Errorf("%w: missing kind", apiconstraints.ErrInvalidConstraint)
	}

	if gk.Group != apiconstraints.Group {
		return fmt.Errorf("%w: wrong API Group for Constraint %q, got %q but need %q",
			apiconstraints.ErrInvalidConstraint, constraint.GetName(), gk.Group, apiconstraints.Group)
	}

	return nil
}

func (c *Client) validateConstraint(constraint *unstructured.Unstructured) error {
	err := validateConstraintMetadata(constraint)
	if err != nil {
		return err
	}

	kind := constraint.GetKind()
	template := c.getTemplateForKind(kind)
	if template == nil {
		templateName := strings.ToLower(kind)
		return templateNotFound(templateName)
	}

	return template.ValidateConstraint(constraint)
}

// ValidateConstraint returns an error if the constraint is not recognized or does not conform to
// the registered CRD for that constraint.
func (c *Client) ValidateConstraint(constraint *unstructured.Unstructured) error {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	return c.validateConstraint(constraint)
}

// AddData inserts the provided data into OPA for every target that can handle the data.
// On error, the responses return value will still be populated so that
// partial results can be analyzed.
func (c *Client) AddData(ctx context.Context, data interface{}) (*types.Responses, error) {
	// TODO(#189): Make AddData atomic across all Drivers/Targets.

	resp := types.NewResponses()
	errMap := make(clienterrors.ErrorMap)
	// The set of targets doesn't change after Client initialization, so it is safe
	// to forego locking here. Similarly, - the Driver locks itself on writing data
	// and no state outside of Driver is changed by this operation, so Client
	// needs no locking here.
	for name, target := range c.targets {
		handled, key, processedData, err := target.ProcessData(data)
		if err != nil {
			errMap[name] = err
			continue
		}
		if !handled {
			continue
		}

		var cache handler.Cache
		if cacher, ok := target.(handler.Cacher); ok {
			cache = cacher.GetCache()
		}

		// Add to the target cache first because cache.Remove cannot fail. Thus, we
		// can prevent the system from getting into an inconsistent state.
		if cache != nil {
			err = cache.Add(key, processedData)
			if err != nil {
				// Use a different key than the driver to avoid clobbering errors.
				errMap[name] = err

				continue
			}
		}

		err = c.driver.AddData(ctx, name, key, processedData)
		if err != nil {
			errMap[name] = err

			if cache != nil {
				cache.Remove(key)
			}
			continue
		}

		resp.Handled[name] = true
	}

	if len(errMap) == 0 {
		return resp, nil
	}
	return resp, &errMap
}

// RemoveData removes data from OPA for every target that can handle the data.
// On error, the responses return value will still be populated so that
// partial results can be analyzed.
func (c *Client) RemoveData(ctx context.Context, data interface{}) (*types.Responses, error) {
	resp := types.NewResponses()
	errMap := make(clienterrors.ErrorMap)
	// Similar to AddData - no locking is required here. See AddData for full
	// explanation.
	for target, h := range c.targets {
		handled, relPath, _, err := h.ProcessData(data)
		if err != nil {
			errMap[target] = err
			continue
		}
		if !handled {
			continue
		}

		err = c.driver.RemoveData(ctx, target, relPath)
		if err != nil {
			errMap[target] = err
			continue
		}

		resp.Handled[target] = true

		if cacher, ok := h.(handler.Cacher); ok {
			cache := cacher.GetCache()

			cache.Remove(relPath)
		}
	}

	if len(errMap) == 0 {
		return resp, nil
	}

	return resp, &errMap
}

// Review makes sure the provided object satisfies all stored constraints.
// On error, the responses return value will still be populated so that
// partial results can be analyzed.
func (c *Client) Review(ctx context.Context, obj interface{}, opts ...drivers.QueryOpt) (*types.Responses, error) {
	responses := types.NewResponses()
	errMap := make(clienterrors.ErrorMap)

	ignoredTargets := make(map[string]bool)
	reviews := make(map[string]interface{})
	// The set of targets should not change after Client is initialized, so it
	// is safe to defer locking until after reviews have been created.
	for name, target := range c.targets {
		handled, review, err := target.HandleReview(obj)
		if err != nil {
			errMap.Add(name, fmt.Errorf("%w for target %q: %v", ErrReview, name, err))
			continue
		}

		if !handled {
			ignoredTargets[name] = true
			continue
		}

		reviews[name] = review
	}

	constraintsByTarget := make(map[string][]*unstructured.Unstructured)
	autorejections := make(map[string][]constraintMatchResult)

	var templateList []*templateClient

	c.mtx.RLock()
	defer c.mtx.RUnlock()

	for _, template := range c.templates {
		templateList = append(templateList, template)
	}

	for target, review := range reviews {
		var targetConstraints []*unstructured.Unstructured

		for _, template := range templateList {
			matchingConstraints := template.Matches(target, review)
			for _, matchResult := range matchingConstraints {
				if matchResult.error == nil {
					targetConstraints = append(targetConstraints, matchResult.constraint)
				} else {
					autorejections[target] = append(autorejections[target], matchResult)
				}
			}
		}
		constraintsByTarget[target] = targetConstraints
	}

	for target, review := range reviews {
		constraints := constraintsByTarget[target]

		resp, err := c.review(ctx, target, constraints, review, opts...)
		if err != nil {
			errMap.Add(target, err)
			continue
		}

		for _, autorejection := range autorejections[target] {
			resp.AddResult(autorejection.ToResult())
		}

		// Ensure deterministic result ordering.
		resp.Sort()

		responses.ByTarget[target] = resp
	}

	if len(errMap) == 0 {
		return responses, nil
	}

	return responses, &errMap
}

func (c *Client) review(ctx context.Context, target string, constraints []*unstructured.Unstructured, review interface{}, opts ...drivers.QueryOpt) (*types.Response, error) {
	var results []*types.Result
	var tracesBuilder strings.Builder

	results, trace, err := c.driver.Query(ctx, target, constraints, review, opts...)
	if err != nil {
		return nil, err
	}

	if trace != nil {
		tracesBuilder.WriteString(*trace)
		tracesBuilder.WriteString("\n\n")
	}

	return &types.Response{
		Trace:   trace,
		Target:  target,
		Results: results,
	}, nil
}

// Dump dumps the state of OPA to aid in debugging.
func (c *Client) Dump(ctx context.Context) (string, error) {
	return c.driver.Dump(ctx)
}

// knownTargets returns a sorted list of known target names.
func (c *Client) knownTargets() []string {
	var knownTargets []string
	for known := range c.targets {
		knownTargets = append(knownTargets, known)
	}
	sort.Strings(knownTargets)

	return knownTargets
}

// getTargetHandler returns the TargetHandler for the Template, or an error if
// it does not exist.
//
// The set of targets is assumed to be constant.
func (c *Client) getTargetHandler(templ *templates.ConstraintTemplate) (handler.TargetHandler, error) {
	targetName, err := getTargetName(templ)
	if err != nil {
		return nil, err
	}

	targetHandler, found := c.targets[targetName]

	if !found {
		knownTargets := c.knownTargets()

		return nil, fmt.Errorf("%w: target %q not recognized, known targets %v",
			clienterrors.ErrInvalidConstraintTemplate, targetName, knownTargets)
	}

	return targetHandler, nil
}

// createCRD creates the Template's CRD and validates the result.
func createCRD(ctx context.Context, templ *templates.ConstraintTemplate, target handler.TargetHandler) (*apiextensions.CustomResourceDefinition, error) {
	sch := crds.CreateSchema(templ, target)

	crd, err := crds.CreateCRD(templ, sch)
	if err != nil {
		return nil, err
	}

	err = crds.ValidateCRD(ctx, crd)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", clienterrors.ErrInvalidConstraintTemplate, err)
	}

	return crd, nil
}

func validateTemplateMetadata(templ *templates.ConstraintTemplate) error {
	kind := templ.Spec.CRD.Spec.Names.Kind
	if kind == "" {
		return fmt.Errorf("%w: ConstraintTemplate %q does not specify CRD Kind",
			clienterrors.ErrInvalidConstraintTemplate, templ.GetName())
	}

	if !strings.EqualFold(templ.ObjectMeta.Name, kind) {
		return fmt.Errorf("%w: the ConstraintTemplate's name %q is not equal to the lowercase of CRD's Kind: %q",
			clienterrors.ErrInvalidConstraintTemplate, templ.ObjectMeta.Name, strings.ToLower(kind))
	}

	return nil
}
