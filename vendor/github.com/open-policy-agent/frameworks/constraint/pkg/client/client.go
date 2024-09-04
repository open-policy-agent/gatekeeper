package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/crds"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	regoSchema "github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego/schema"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	clienterrors "github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/handler"
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
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
	// driver priority specifies the preference for which driver should
	// be preferred if a template specifies multiple kinds of source
	// code. It is determined by the order with which drivers are
	// added to the client.
	driverPriority map[string]int

	// ignoreNoReferentialDriverWarning toggles whether we warn the user
	// when there is no registered driver that supports referential data when
	// they call AddData()
	ignoreNoReferentialDriverWarning bool

	// drivers contains the drivers for policy engines understood
	// by the constraint framework client.
	// Does not require mutex locking as Driver is threadsafe
	// and the map should be created during bootstrapping.
	drivers map[string]drivers.Driver
	// targets are the targets supported by this Client.
	// Assumed to be constant after initialization.
	targets map[string]handler.TargetHandler

	// mtx guards reading and writing data outside of Driver.
	mtx sync.RWMutex

	// templates is a map from a Template's name to its entry.
	templates map[string]*templateClient

	// enforcementPoints is array of enforcement points for which this client may be used.
	enforcementPoints []string
}

// driverForTemplate returns the driver to be used for a template according
// to the driver priority in the client. An empty string means the constraint
// template does not contain a language the client has a driver for.
func (c *Client) driverForTemplate(template *templates.ConstraintTemplate) string {
	if len(template.Spec.Targets) == 0 {
		return ""
	}
	language := ""
	for _, v := range template.Spec.Targets[0].Code {
		priority, ok := c.driverPriority[v.Engine]
		if !ok {
			continue
		}
		if priority < c.driverPriority[language] || c.driverPriority[language] == 0 {
			language = v.Engine
		}
	}
	return language
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

	// if there is more than one active driver for the template, there is some cleanup to do
	// from a botched driver swap.
	if cachedCpy != nil && cachedCpy.SemanticEqual(templ) && len(cached.activeDrivers) == 1 {
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

	newDriverN := c.driverForTemplate(templ)

	driver, ok := c.drivers[newDriverN]
	if !ok {
		return resp, fmt.Errorf("%w: available drivers: %v, wanted %q", clienterrors.ErrNoDriver, c.driverPriority, c.driverForTemplate(templ))
	}

	// TODO: because different targets may have different code sets,
	// the driver should be told which targets to load code for.
	// this is moot right now, since templates only have one target
	if err := driver.AddTemplate(ctx, templ); err != nil {
		return resp, err
	}

	templateName := templ.GetName()

	cacheEntry := c.templates[templateName]

	// We don't want to use the usual "if found/ok" idiom here - if the value
	// stored for templateName is nil, we need to update it to be non-nil to avoid
	// a panic.
	if cacheEntry == nil {
		cacheEntry = newTemplateClient()
		c.templates[templateName] = cacheEntry
	}

	cacheEntry.activeDrivers[newDriverN] = true

	// For drivers that require a local cache of constraints, we ensure that
	// cache is current if the active driver has changed.
	if cachedCpy != nil {
		oldDriverN := c.driverForTemplate(cachedCpy)
		if oldDriverN != newDriverN {
			cacheEntry.needsConstraintReplay = true
		}
	}

	if cacheEntry.needsConstraintReplay {
		for _, constraintEntry := range cacheEntry.constraints {
			cstr := constraintEntry.getConstraint()
			if err := driver.AddConstraint(ctx, cstr); err != nil {
				return resp, fmt.Errorf("%w: while replaying constraints", err)
			}
		}
		cacheEntry.needsConstraintReplay = false
	}

	// This state mutation needs to happen after the new driver is fully ready
	// to enforce the template
	cacheEntry.Update(templ, crd, target)

	// Remove old drivers last so that templates can be enforced
	// despite a botched update
	for oldDriverN := range cacheEntry.activeDrivers {
		if oldDriverN == newDriverN {
			continue
		}
		oldDriver, ok := c.drivers[oldDriverN]
		if !ok {
			return resp, fmt.Errorf("%w: while changing drivers", clienterrors.ErrNoDriver)
		}
		if err := oldDriver.RemoveTemplate(ctx, cachedCpy); err != nil {
			return resp, fmt.Errorf("%w: while changing drivers", err)
		}
		delete(cacheEntry.activeDrivers, oldDriverN)
	}

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

	name := templ.GetName()

	cached, found := c.templates[name]
	if !found {
		return resp, nil
	}

	template := cached.getTemplate()

	// remove the template from all active drivers
	// to ensure cleanup in case of a botched update
	for driverN := range cached.activeDrivers {
		driver, ok := c.drivers[driverN]
		if !ok {
			return resp, fmt.Errorf("%w: could not clean up %q", clienterrors.ErrNoDriver, driverN)
		}

		err := driver.RemoveTemplate(ctx, template)
		if err != nil {
			return resp, err
		}
		delete(cached.activeDrivers, driverN)
	}

	delete(c.templates, name)

	for _, target := range cached.targets {
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

// getTemplateClientForKind returns the template entry for a given constraint.
func (c *Client) getTemplateClientForKind(kind string) *templateClient {
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
	cached := c.getTemplateClientForKind(kind)
	if cached == nil {
		templateName := strings.ToLower(kind)
		return resp, templateNotFound(templateName)
	}

	template := cached.getTemplate()

	driver, ok := c.drivers[c.driverForTemplate(template)]
	if !ok {
		return resp, clienterrors.ErrNoDriver
	}

	constraintWithDefaults, err := cached.ApplyDefaultParams(constraint)
	if err != nil {
		return resp, err
	}

	changed, err := cached.AddConstraint(constraintWithDefaults, c.enforcementPoints)
	if err != nil {
		return resp, err
	}

	if changed {
		err = driver.AddConstraint(ctx, constraintWithDefaults)
		if err != nil {
			return resp, err
		}
	}

	for _, target := range cached.targets {
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

	kind := constraint.GetKind()

	cached := c.getTemplateClientForKind(kind)
	if cached == nil {
		// The Template has been deleted, so nothing to do and no reason to return
		// error.
		return resp, nil
	}

	// Remove the constraint from all active drivers
	// in case we are in the middle of a botched update
	for driverN := range cached.activeDrivers {
		driver, ok := c.drivers[driverN]
		if !ok {
			return resp, clienterrors.ErrNoDriver
		}

		err = driver.RemoveConstraint(ctx, constraint)
		if err != nil {
			return nil, err
		}
	}

	for _, target := range cached.targets {
		resp.Handled[target.GetName()] = true
	}

	cached.RemoveConstraint(constraint.GetName())

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
	template := c.getTemplateClientForKind(kind)
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
	template := c.getTemplateClientForKind(kind)
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

		// Round trip data to force untyped JSON, as drivers are not type-aware
		bytes, err := json.Marshal(processedData)
		if err != nil {
			errMap[name] = err

			continue
		}
		var processedDataCpy interface{}
		err = json.Unmarshal(bytes, &processedDataCpy)
		if err != nil {
			errMap[name] = err

			continue
		}

		// To avoid maintaining duplicate caches, only Rego should get its own
		// storage. We should work to remove the need for this special case
		// by building a global storage object. Right now Rego needs its own
		// cache to cache constraints.
		if _, ok := c.drivers[regoSchema.Name]; ok {
			err = c.drivers[regoSchema.Name].AddData(ctx, name, key, processedDataCpy)
			if err != nil {
				errMap[name] = err

				if cache != nil {
					cache.Remove(key)
				}
				continue
			}
		} else if !c.ignoreNoReferentialDriverWarning {
			errMap[name] = ErrNoReferentialDriver
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

		// To avoid maintaining duplicate caches, only Rego should get its own
		// storage. We should work to remove the need for this special case
		// by building a global storage object. Right now Rego needs its own
		// cache to cache constraints.
		if _, ok := c.drivers[regoSchema.Name]; ok {
			err = c.drivers[regoSchema.Name].RemoveData(ctx, target, relPath)
			if err != nil {
				errMap[target] = err
				continue
			}
		} else if !c.ignoreNoReferentialDriverWarning {
			errMap[target] = ErrNoReferentialDriver
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

// Review makes sure the provided object satisfies constraints applicable for specific enforcement points.
// On error, the responses return value will still be populated so that
// partial results can be analyzed.
func (c *Client) Review(ctx context.Context, obj interface{}, opts ...reviews.ReviewOpt) (*types.Responses, error) {
	var eps []string
	cfg := &reviews.ReviewCfg{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.EnforcementPoint == "" {
		cfg.EnforcementPoint = apiconstraints.AllEnforcementPoints
	}
	for _, ep := range c.enforcementPoints {
		if cfg.EnforcementPoint == apiconstraints.AllEnforcementPoints || cfg.EnforcementPoint == ep {
			eps = append(eps, ep)
		}
	}
	if eps == nil {
		return nil, fmt.Errorf("%w, supported enforcement points: %v", ErrUnsupportedEnforcementPoints, c.enforcementPoints)
	}

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
	scopedEnforcementActionsByTarget := make(map[string]map[string][]string)
	enforcementActionByTarget := make(map[string]map[string]string)

	var templateList []*templateClient

	c.mtx.RLock()
	defer c.mtx.RUnlock()

	for _, template := range c.templates {
		templateList = append(templateList, template)
	}

	for target, review := range reviews {
		var targetConstraints []*unstructured.Unstructured
		targetScopedEnforcementActions := make(map[string][]string)
		targetEnforcementAction := make(map[string]string)
		for _, template := range templateList {
			matchingConstraints := template.Matches(target, review, eps)
			for _, matchResult := range matchingConstraints {
				if matchResult.error == nil {
					targetConstraints = append(targetConstraints, matchResult.constraint)
					targetScopedEnforcementActions[matchResult.constraint.GetName()] = matchResult.scopedEnforcementActions
					targetEnforcementAction[matchResult.constraint.GetName()] = matchResult.enforcementAction
				} else {
					autorejections[target] = append(autorejections[target], matchResult)
				}
			}
		}
		constraintsByTarget[target] = targetConstraints
		scopedEnforcementActionsByTarget[target] = targetScopedEnforcementActions
		enforcementActionByTarget[target] = targetEnforcementAction
	}

	for target, review := range reviews {
		constraints := constraintsByTarget[target]

		resp, stats, err := c.review(ctx, target, constraints, review, opts...)
		if err != nil {
			errMap.Add(target, err)
			continue
		}

		for i := range resp.Results {
			if val, ok := scopedEnforcementActionsByTarget[target][resp.Results[i].Constraint.GetName()]; ok {
				resp.Results[i].ScopedEnforcementActions = val
			}
			if val, ok := enforcementActionByTarget[target][resp.Results[i].Constraint.GetName()]; ok {
				resp.Results[i].EnforcementAction = val
			}
		}

		for _, autorejection := range autorejections[target] {
			resp.AddResult(autorejection.ToResult())
		}

		// Ensure deterministic result ordering.
		resp.Sort()

		responses.ByTarget[target] = resp
		if stats != nil {
			// add the target label to these stats for future collation.
			targetLabel := &instrumentation.Label{Name: "target", Value: target}
			for _, stat := range stats {
				if stat.Labels == nil || len(stat.Labels) == 0 {
					stat.Labels = []*instrumentation.Label{targetLabel}
				} else {
					stat.Labels = append(stat.Labels, targetLabel)
				}
			}
			responses.StatsEntries = append(responses.StatsEntries, stats...)
		}
	}

	if len(errMap) == 0 {
		return responses, nil
	}

	return responses, &errMap
}

func (c *Client) review(ctx context.Context, target string, constraints []*unstructured.Unstructured, review interface{}, opts ...reviews.ReviewOpt) (*types.Response, []*instrumentation.StatsEntry, error) {
	var results []*types.Result
	var stats []*instrumentation.StatsEntry
	var tracesBuilder strings.Builder
	errs := &errors.ErrorMap{}

	driverToConstraints := map[string][]*unstructured.Unstructured{}

	for _, constraint := range constraints {
		template, ok := c.templates[strings.ToLower(constraint.GetObjectKind().GroupVersionKind().Kind)]
		if !ok {
			return nil, nil, fmt.Errorf("%w: while loading driver for constraint %s", ErrMissingConstraintTemplate, constraint.GetName())
		}
		driver := c.driverForTemplate(template.template)
		if driver == "" {
			return nil, nil, fmt.Errorf("%w: while loading driver for constraint %s", clienterrors.ErrNoDriver, constraint.GetName())
		}
		driverToConstraints[driver] = append(driverToConstraints[driver], constraint)
	}

	for driverName, driver := range c.drivers {
		if len(driverToConstraints[driverName]) == 0 {
			continue
		}
		qr, err := driver.Query(ctx, target, driverToConstraints[driverName], review, opts...)
		if err != nil {
			errs.Add(driverName, err)
			continue
		}
		if qr != nil {
			results = append(results, qr.Results...)

			stats = append(stats, qr.StatsEntries...)

			if qr.Trace != nil {
				tracesBuilder.WriteString(fmt.Sprintf("DRIVER %s:\n\n", driverName))
				tracesBuilder.WriteString(*qr.Trace)
				tracesBuilder.WriteString("\n\n")
			}
		}
	}

	traceStr := tracesBuilder.String()
	var trace *string
	if len(traceStr) != 0 {
		trace = &traceStr
	}

	// golang idiom is nil on no errors, so we should
	// only return errs if it is non-empty, otherwise
	// we get a non-nil interface (even if errs is nil, since
	// the interface would still hold type info).
	var errRet error
	if len(*errs) > 0 {
		errRet = errs
	}

	return &types.Response{
		Trace:   trace,
		Target:  target,
		Results: results,
	}, stats, errRet
}

// Dump dumps the state of OPA to aid in debugging.
func (c *Client) Dump(ctx context.Context) (string, error) {
	var dumpBuilder strings.Builder
	for driverName, driver := range c.drivers {
		dump, err := driver.Dump(ctx)
		if err != nil {
			return "", err
		}
		dumpBuilder.WriteString(fmt.Sprintf("DRIVER: %s:\n\n", driverName))
		dumpBuilder.WriteString(dump)
		dumpBuilder.WriteString("\n\n")
	}
	return dumpBuilder.String(), nil
}

func (c *Client) GetDescriptionForStat(source instrumentation.Source, statName string) string {
	if source.Type != instrumentation.EngineSourceType {
		// only handle engine source for now
		return instrumentation.UnknownDescription
	}

	driver, ok := c.drivers[source.Value]
	if !ok {
		return instrumentation.UnknownDescription
	}

	desc, err := driver.GetDescriptionForStat(statName)
	if err != nil {
		return instrumentation.UnknownDescription
	}

	return desc
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
