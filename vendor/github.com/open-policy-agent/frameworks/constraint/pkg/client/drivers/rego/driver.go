package rego

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego/schema"
	clienterrors "github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/print"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
)

const (
	libRoot   = "data.lib"
	violation = "violation"

	templateRunTimeNS     = "templateRunTimeNS"
	templateRunTimeNsDesc = "the number of nanoseconds it took to evaluate all constraints for a template"

	constraintCountName        = "constraintCount"
	constraintCountDescription = "the number of constraints that were evaluated for the given constraint kind"

	tracingEnabledLabelName = "TracingEnabled"
	printEnabledLabelName   = "PrintEnabled"
)

var _ drivers.Driver = &Driver{}

// Driver is a threadsafe Rego environment for compiling Rego in ConstraintTemplates,
// registering Constraints, and executing queries.
type Driver struct {
	// compilers is a store of Rego Compilers for each Template.
	compilers Compilers

	// mtx guards access to the storage and target maps.
	mtx sync.RWMutex

	storage storages

	// targets is a map from each Template's kind to the targets for that Template.
	targets map[string][]string

	// traceEnabled is whether tracing is enabled for Rego queries by default.
	// If enabled, individual queries cannot disable tracing.
	traceEnabled bool

	// printEnabled is whether print statements are allowed in Rego. If disabled,
	// print statements are removed from modules at compile-time.
	printEnabled bool

	// printHook specifies where to send the output of Rego print() statements.
	printHook print.Hook

	// providerCache allows Rego to read from external_data in Rego queries.
	providerCache *externaldata.ProviderCache

	// providerResponseCache allows to cache responses from external_data providers.
	providerResponseCache *externaldata.ProviderResponseCache

	// sendRequestToProvider allows Rego to send requests to the provider specified in external_data.
	sendRequestToProvider externaldata.SendRequestToProvider

	// enableExternalDataClientAuth enables the injection of a TLS certificate into an HTTP client
	// that is used to communicate with providers.
	enableExternalDataClientAuth bool

	// clientCertWatcher is a watcher for the TLS certificate used to communicate with providers.
	clientCertWatcher *certwatcher.CertWatcher

	// gatherStats controls whether the driver gathers any stats around its API calls.
	gatherStats bool
}

// Name returns the name of the driver.
func (d *Driver) Name() string {
	return schema.Name
}

// AddTemplate adds templ to Driver. Normalizes modules into usable forms for
// use in queries.
func (d *Driver) AddTemplate(ctx context.Context, templ *templates.ConstraintTemplate) error {
	var targets []string
	for _, target := range templ.Spec.Targets {
		// Ensure storage for each of this Template's targets exists.
		_, err := d.storage.getStorage(ctx, target.Target)
		if err != nil {
			return err
		}
		targets = append(targets, target.Target)
	}

	kind := templ.Spec.CRD.Spec.Names.Kind

	d.mtx.Lock()
	defer d.mtx.Unlock()

	d.targets[kind] = targets
	return d.compilers.addTemplate(templ, d.printEnabled)
}

// RemoveTemplate removes all Compilers and Constraints for templ.
// Returns nil if templ does not exist.
func (d *Driver) RemoveTemplate(ctx context.Context, templ *templates.ConstraintTemplate) error {
	kind := templ.Spec.CRD.Spec.Names.Kind

	constraintParent := storage.Path{"constraints", kind}

	d.mtx.Lock()
	defer d.mtx.Unlock()

	if err := d.storage.removeDataEach(ctx, constraintParent); err != nil {
		return err
	}

	d.compilers.removeTemplate(kind)
	delete(d.targets, kind)
	return nil
}

// AddConstraint adds Constraint to Rego storage. Future calls to Query will
// be evaluated against Constraint if the Constraint's key is passed.
func (d *Driver) AddConstraint(ctx context.Context, constraint *unstructured.Unstructured) error {
	// Note that this discards "status" as we only copy spec.parameters.
	params, _, err := unstructured.NestedFieldNoCopy(constraint.Object, "spec", "parameters")
	if err != nil {
		return fmt.Errorf("%w: %v", constraints.ErrInvalidConstraint, err)
	}

	// default .spec.parameters so that we don't need to default this in Rego.
	if params == nil {
		params = make(map[string]interface{})
	}

	key := drivers.ConstraintKeyFrom(constraint)
	path := key.StoragePath()

	d.mtx.Lock()
	defer d.mtx.Unlock()

	targets := d.targets[key.Kind]
	for _, target := range targets {
		err := d.storage.addData(ctx, target, path, params)
		if err != nil {
			return err
		}
	}

	return nil
}

// RemoveConstraint removes Constraint from Rego storage. Future calls to Query
// will not be evaluated against the constraint. Queries which specify the
// constraint's key will silently not evaluate the Constraint.
func (d *Driver) RemoveConstraint(ctx context.Context, constraint *unstructured.Unstructured) error {
	path := drivers.ConstraintKeyFrom(constraint).StoragePath()

	d.mtx.Lock()
	defer d.mtx.Unlock()

	return d.storage.removeDataEach(ctx, path)
}

// AddData adds data to Rego storage at data.inventory.path.
func (d *Driver) AddData(ctx context.Context, target string, path storage.Path, data interface{}) error {
	path = inventoryPath(path)
	return d.storage.addData(ctx, target, path, data)
}

// RemoveData deletes data from Rego storage at data.inventory.path.
func (d *Driver) RemoveData(ctx context.Context, target string, path storage.Path) error {
	path = inventoryPath(path)
	return d.storage.removeData(ctx, target, path)
}

// eval runs a query against compiler.
// path is the path to evaluate.
// input is the already-parsed Rego Value to use as input.
// Returns the Rego results, the trace if requested, or an error if there was
// a problem executing the query.
func (d *Driver) eval(ctx context.Context, compiler *ast.Compiler, target string, path []string, input ast.Value, opts ...reviews.ReviewOpt) (rego.ResultSet, *string, error) {
	cfg := &reviews.ReviewCfg{}
	for _, opt := range opts {
		opt(cfg)
	}

	queryPath := strings.Builder{}
	queryPath.WriteString("data")
	for _, p := range path {
		queryPath.WriteString(".")
		queryPath.WriteString(p)
	}

	store, err := d.storage.getStorage(ctx, target)
	if err != nil {
		return nil, nil, err
	}

	args := []func(*rego.Rego){
		rego.Compiler(compiler),
		rego.Store(store),
		rego.ParsedInput(input),
		rego.Query(queryPath.String()),
		rego.EnablePrintStatements(d.printEnabled),
		rego.PrintHook(d.printHook),
	}

	buf := topdown.NewBufferTracer()
	if d.traceEnabled || cfg.TracingEnabled {
		args = append(args, rego.QueryTracer(buf))
	}

	r := rego.New(args...)
	res, err := r.Eval(ctx)

	var t *string
	if d.traceEnabled || cfg.TracingEnabled {
		b := &bytes.Buffer{}
		topdown.PrettyTrace(b, *buf)
		t = ptr.To[string](b.String())
	}

	return res, t, err
}

func (d *Driver) Query(ctx context.Context, target string, constraints []*unstructured.Unstructured, review interface{}, opts ...reviews.ReviewOpt) (*drivers.QueryResponse, error) {
	if len(constraints) == 0 {
		return nil, nil
	}

	constraintsByKind := toConstraintsByKind(constraints)

	traceBuilder := strings.Builder{}
	constraintsMap := drivers.KeyMap(constraints)
	path := []string{"hooks", "violation[result]"}

	var results []*types.Result

	// Round-trip review through JSON so that the review object is round-tripped
	// once per call to Query instead of once per compiler.
	reviewMap, err := toInterfaceMap(review)
	if err != nil {
		return nil, err
	}

	d.mtx.RLock()
	defer d.mtx.RUnlock()

	cfg := &reviews.ReviewCfg{}
	for _, opt := range opts {
		opt(cfg)
	}

	var statsEntries []*instrumentation.StatsEntry

	for kind, kindConstraints := range constraintsByKind {
		evalStartTime := time.Now()
		compiler := d.compilers.getCompiler(target, kind)
		if compiler == nil {
			// The Template was just removed, so the Driver is in an inconsistent
			// state with Client. Raise this as an error rather than attempting to
			// continue.
			return nil, fmt.Errorf("missing Template %q for target %q", kind, target)
		}

		// Parse input into an ast.Value to avoid round-tripping through JSON when
		// possible.
		parsedInput, err := toParsedInput(target, kindConstraints, reviewMap)
		if err != nil {
			return nil, err
		}

		resultSet, trace, err := d.eval(ctx, compiler, target, path, parsedInput, opts...)
		evalEndTime := time.Since(evalStartTime)
		if err != nil {
			resultSet = make(rego.ResultSet, 0, len(kindConstraints))
			for _, constraint := range kindConstraints {
				resultSet = append(resultSet, rego.Result{
					Bindings: map[string]interface{}{
						"result": map[string]interface{}{
							"msg": err.Error(),
							"key": map[string]interface{}{
								"kind": constraint.GetKind(),
								"name": constraint.GetName(),
							},
						},
					},
				})
			}
		}
		if trace != nil {
			traceBuilder.WriteString(*trace)
		}

		kindResults, err := drivers.ToResults(constraintsMap, resultSet)
		if err != nil {
			return nil, err
		}

		results = append(results, kindResults...)

		if d.gatherStats || (cfg != nil && cfg.StatsEnabled) {
			statsEntries = append(statsEntries,
				&instrumentation.StatsEntry{
					Scope:    instrumentation.TemplateScope,
					StatsFor: kind,
					Stats: []*instrumentation.Stat{
						{
							Name:  templateRunTimeNS,
							Value: uint64(evalEndTime.Nanoseconds()),
							Source: instrumentation.Source{
								Type:  instrumentation.EngineSourceType,
								Value: schema.Name,
							},
						},
						{
							Name:  constraintCountName,
							Value: len(kindConstraints),
							Source: instrumentation.Source{
								Type:  instrumentation.EngineSourceType,
								Value: schema.Name,
							},
						},
					},
					Labels: []*instrumentation.Label{
						{
							Name:  tracingEnabledLabelName,
							Value: d.traceEnabled || cfg.TracingEnabled,
						},
						{
							Name:  printEnabledLabelName,
							Value: d.printEnabled,
						},
					},
				})
		}
	}

	traceString := traceBuilder.String()
	if len(traceString) != 0 {
		return &drivers.QueryResponse{Results: results, Trace: &traceString, StatsEntries: statsEntries}, nil
	}

	return &drivers.QueryResponse{Results: results, StatsEntries: statsEntries}, nil
}

func (d *Driver) Dump(ctx context.Context) (string, error) {
	// we want to create:
	// targetName.modules.kind.moduleName = contents
	// targetName.data = data
	dt := make(map[string]map[string]interface{})

	compilers := d.compilers.list()
	for targetName, targetCompilers := range compilers {
		targetModules := make(map[string]map[string]string)

		for kind, compiler := range targetCompilers {
			kindModules := make(map[string]string)
			for modname, contents := range compiler.Modules {
				kindModules[modname] = contents.String()
			}
			targetModules[kind] = kindModules
		}
		dt[targetName] = map[string]interface{}{}
		dt[targetName]["modules"] = targetModules

		emptyCompiler := ast.NewCompiler().WithCapabilities(d.compilers.capabilities)

		rs, _, err := d.eval(ctx, emptyCompiler, targetName, []string{}, nil)
		if err != nil {
			return "", err
		}

		if len(rs) != 0 && len(rs[0].Expressions) != 0 {
			dt[targetName]["data"] = rs[0].Expressions[0].Value
		}
	}

	b, err := json.MarshalIndent(dt, "", "   ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func (d *Driver) GetDescriptionForStat(statName string) (string, error) {
	switch statName {
	case templateRunTimeNS:
		return templateRunTimeNsDesc, nil
	case constraintCountName:
		return constraintCountDescription, nil
	default:
		return "", fmt.Errorf("unknown stat name")
	}
}

func (d *Driver) getTLSCertificate() (*tls.Certificate, error) {
	if !d.enableExternalDataClientAuth {
		return nil, nil
	}

	if d.clientCertWatcher == nil {
		return nil, fmt.Errorf("certWatcher should not be nil when enableExternalDataClientAuth is true")
	}

	return d.clientCertWatcher.GetCertificate(nil)
}

// rewriteModulePackage rewrites the module's package path to path.
func rewriteModulePackage(module *ast.Module) error {
	pathParts := ast.Ref([]*ast.Term{ast.VarTerm(templatePath)})

	packageRef := ast.Ref([]*ast.Term{ast.VarTerm("data")})
	newPath := packageRef.Extend(pathParts)
	module.Package.Path = newPath
	return nil
}

// requireModuleRules makes sure the module contains all of the specified
// requiredRules.
func requireModuleRules(module *ast.Module, requiredRules map[string]struct{}) error {
	ruleSets := make(map[string]struct{}, len(module.Rules))
	for _, rule := range module.Rules {
		ruleSets[string(rule.Head.Name)] = struct{}{}
	}

	var missing []string
	for name := range requiredRules {
		_, ok := ruleSets[name]
		if !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		return fmt.Errorf("%w: missing required rules: %v",
			clienterrors.ErrInvalidModule, missing)
	}

	return nil
}

func toInterfaceMap(obj interface{}) (map[string]interface{}, error) {
	jsn, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	result := make(map[string]interface{})
	err = json.Unmarshal(jsn, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func toKeySlice(constraints []*unstructured.Unstructured) []interface{} {
	var keys []interface{}
	for _, constraint := range constraints {
		key := drivers.ConstraintKeyFrom(constraint)
		keys = append(keys, map[string]interface{}{
			"kind": key.Kind,
			"name": key.Name,
		})
	}

	return keys
}

func toConstraintsByKind(constraints []*unstructured.Unstructured) map[string][]*unstructured.Unstructured {
	constraintsByKind := make(map[string][]*unstructured.Unstructured)
	for _, constraint := range constraints {
		kind := constraint.GetKind()
		constraintsByKind[kind] = append(constraintsByKind[kind], constraint)
	}

	return constraintsByKind
}

func toParsedInput(target string, constraints []*unstructured.Unstructured, review map[string]interface{}) (ast.Value, error) {
	// Store constraint keys in a format InterfaceToValue does not need to
	// round-trip through JSON.
	constraintKeys := toKeySlice(constraints)

	input := map[string]interface{}{
		"target":      target,
		"constraints": constraintKeys,
		"review":      review,
	}

	// Parse input into an ast.Value to avoid round-tripping through JSON when
	// possible.
	return ast.InterfaceToValue(input)
}
