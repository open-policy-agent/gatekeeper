package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"

	clienterrors "github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/handler"
	"github.com/open-policy-agent/frameworks/constraint/pkg/regorewriter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/print"
	opatypes "github.com/open-policy-agent/opa/types"
	"k8s.io/utils/pointer"
)

const (
	moduleSetPrefix = "__modset_"
	moduleSetSep    = "_idx_"
	libRoot         = "data.lib"
	violation       = "violation"
)

type module struct {
	text   string
	parsed *ast.Module
}

type insertParam map[string]*module

func (i insertParam) add(name string, src string) error {
	m, err := ast.ParseModule(name, src)
	if err != nil {
		return fmt.Errorf("%w: %q: %v", clienterrors.ErrParse, name, err)
	}

	i[name] = &module{text: src, parsed: m}
	return nil
}

func New(args ...Arg) drivers.Driver {
	d := &Driver{}
	for _, arg := range args {
		arg(d)
	}

	Defaults()(d)

	d.compiler.WithCapabilities(d.capabilities)

	return d
}

var _ drivers.Driver = &Driver{}

type Driver struct {
	modulesMux sync.RWMutex
	compiler   *ast.Compiler
	modules    map[string]*ast.Module
	// thMux guards the access to templateHandler
	thMux sync.RWMutex
	// templateHandler stores the template kind and the mapping target handlers
	templateHandler map[string][]string
	storage         storage.Store
	capabilities    *ast.Capabilities
	traceEnabled    bool
	printEnabled    bool
	printHook       print.Hook
	providerCache   *externaldata.ProviderCache
	externs         []string
	// handlers is a map from handler name to the respective handler
	handlers map[string]handler.TargetHandler
}

func (d *Driver) Init() error {
	if d.providerCache != nil {
		rego.RegisterBuiltin1(
			&rego.Function{
				Name:    "external_data",
				Decl:    opatypes.NewFunction(opatypes.Args(opatypes.A), opatypes.A),
				Memoize: true,
			},
			func(bctx rego.BuiltinContext, regorequest *ast.Term) (*ast.Term, error) {
				var regoReq externaldata.RegoRequest
				if err := ast.As(regorequest.Value, &regoReq); err != nil {
					return nil, err
				}

				provider, err := d.providerCache.Get(regoReq.ProviderName)
				if err != nil {
					return externaldata.HandleError(http.StatusBadRequest, err)
				}

				externaldataRequest := externaldata.NewProviderRequest(regoReq.Keys)
				reqBody, err := json.Marshal(externaldataRequest)
				if err != nil {
					return externaldata.HandleError(http.StatusInternalServerError, err)
				}

				req, err := http.NewRequest("POST", provider.Spec.URL, bytes.NewBuffer(reqBody))
				if err != nil {
					return externaldata.HandleError(http.StatusInternalServerError, err)
				}
				req.Header.Set("Content-Type", "application/json")

				ctx, cancel := context.WithDeadline(bctx.Context, time.Now().Add(time.Duration(provider.Spec.Timeout)*time.Second))
				defer cancel()

				resp, err := http.DefaultClient.Do(req.WithContext(ctx))
				if err != nil {
					return externaldata.HandleError(http.StatusInternalServerError, err)
				}
				defer resp.Body.Close()
				respBody, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					return externaldata.HandleError(http.StatusInternalServerError, err)
				}

				var externaldataResponse externaldata.ProviderResponse
				if err := json.Unmarshal(respBody, &externaldataResponse); err != nil {
					return externaldata.HandleError(http.StatusInternalServerError, err)
				}

				regoResponse := externaldata.NewRegoResponse(resp.StatusCode, &externaldataResponse)
				return externaldata.PrepareRegoResponse(regoResponse)
			},
		)
	}
	return nil
}

func copyModules(modules map[string]*ast.Module) map[string]*ast.Module {
	m := make(map[string]*ast.Module, len(modules))
	for k, v := range modules {
		m[k] = v
	}
	return m
}

func (d *Driver) checkModuleName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: module %q has no name",
			clienterrors.ErrModuleName, name)
	}

	if strings.HasPrefix(name, moduleSetPrefix) {
		return fmt.Errorf("%w: module %q has forbidden prefix %q",
			clienterrors.ErrModuleName, name, moduleSetPrefix)
	}

	return nil
}

func (d *Driver) checkModuleSetName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: modules name prefix cannot be empty", clienterrors.ErrModulePrefix)
	}

	if strings.Contains(name, moduleSetSep) {
		return fmt.Errorf("%w: modules name prefix not allowed to contain the sequence %q", clienterrors.ErrModulePrefix, moduleSetSep)
	}

	return nil
}

func toModuleSetPrefix(prefix string) string {
	return fmt.Sprintf("%s%s%s", moduleSetPrefix, prefix, moduleSetSep)
}

func toModuleSetName(prefix string, idx int) string {
	return fmt.Sprintf("%s%d", toModuleSetPrefix(prefix), idx)
}

func (d *Driver) PutModule(name string, src string) error {
	if err := d.checkModuleName(name); err != nil {
		return err
	}

	insert := insertParam{}
	if err := insert.add(name, src); err != nil {
		return err
	}

	d.modulesMux.Lock()
	defer d.modulesMux.Unlock()

	_, err := d.alterModules(insert, nil)
	return err
}

// putModules upserts a number of modules under a given prefix.
func (d *Driver) putModules(namePrefix string, srcs []string) error {
	if err := d.checkModuleSetName(namePrefix); err != nil {
		return err
	}

	insert := insertParam{}

	for idx, src := range srcs {
		name := toModuleSetName(namePrefix, idx)
		if err := insert.add(name, src); err != nil {
			return err
		}
	}

	d.modulesMux.Lock()
	defer d.modulesMux.Unlock()

	var remove []string
	for _, name := range d.listModuleSet(namePrefix) {
		if _, found := insert[name]; !found {
			remove = append(remove, name)
		}
	}

	_, err := d.alterModules(insert, remove)
	return err
}

// alterModules alters the modules in the driver by inserting and removing
// the provided modules then returns the count of modules removed.
// alterModules expects that the caller is holding the modulesMux lock.
func (d *Driver) alterModules(insert insertParam, remove []string) (int, error) {
	updatedModules := copyModules(d.modules)
	for _, name := range remove {
		delete(updatedModules, name)
	}

	for name, mod := range insert {
		updatedModules[name] = mod.parsed
	}

	c := ast.NewCompiler().
		WithCapabilities(d.capabilities).
		WithEnablePrintStatements(d.printEnabled)

	if c.Compile(updatedModules); c.Failed() {
		return 0, fmt.Errorf("%w: %v", clienterrors.ErrCompile, c.Errors)
	}

	d.compiler = c
	d.modules = updatedModules

	return len(remove), nil
}

// deleteModules deletes all modules under a given prefix and returns the
// count of modules deleted.  Deletion of non-existing prefix will
// result in 0, nil being returned.
func (d *Driver) deleteModules(namePrefix string) (int, error) {
	if err := d.checkModuleSetName(namePrefix); err != nil {
		return 0, err
	}

	d.modulesMux.Lock()
	defer d.modulesMux.Unlock()

	return d.alterModules(nil, d.listModuleSet(namePrefix))
}

// listModuleSet returns the list of names corresponding to a given module
// prefix.
func (d *Driver) listModuleSet(namePrefix string) []string {
	prefix := toModuleSetPrefix(namePrefix)

	var names []string
	for name := range d.modules {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}

	return names
}

func parsePath(path string) ([]string, error) {
	p, ok := storage.ParsePathEscaped(path)
	if !ok {
		return nil, fmt.Errorf("%w: path must begin with '/': %q", clienterrors.ErrPathInvalid, path)
	}
	if len(p) == 0 {
		return nil, fmt.Errorf("%w: path must contain at least one path element: %q", clienterrors.ErrPathInvalid, path)
	}

	return p, nil
}

func (d *Driver) PutData(ctx context.Context, path string, data interface{}) error {
	d.modulesMux.RLock()
	defer d.modulesMux.RUnlock()

	p, err := parsePath(path)
	if err != nil {
		return err
	}

	txn, err := d.storage.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return fmt.Errorf("%w: %v", clienterrors.ErrTransaction, err)
	}

	if _, err = d.storage.Read(ctx, txn, p); err != nil {
		if storage.IsNotFound(err) {
			if err = storage.MakeDir(ctx, d.storage, txn, p[:len(p)-1]); err != nil {
				return fmt.Errorf("%w: unable to make directory: %v", clienterrors.ErrWrite, err)
			}
		} else {
			d.storage.Abort(ctx, txn)
			return fmt.Errorf("%w: %v", clienterrors.ErrRead, err)
		}
	}

	if err = d.storage.Write(ctx, txn, storage.AddOp, p, data); err != nil {
		d.storage.Abort(ctx, txn)
		return fmt.Errorf("%w: unable to write data: %v", clienterrors.ErrWrite, err)
	}

	// TODO: Determine if this can be removed. No tests exercise this path, and
	//  as far as I can tell storage.MakeDir fails where this might return an error.
	if errs := ast.CheckPathConflicts(d.compiler, storage.NonEmpty(ctx, d.storage, txn)); len(errs) > 0 {
		d.storage.Abort(ctx, txn)
		return fmt.Errorf("%w: %q conflicts with existing path: %v",
			clienterrors.ErrPathConflict, path, errs)
	}

	err = d.storage.Commit(ctx, txn)
	if err != nil {
		return fmt.Errorf("%w: %v", clienterrors.ErrTransaction, err)
	}
	return nil
}

// DeleteData deletes data from OPA and returns true if data was found and deleted, false
// if data was not found, and any errors.
func (d *Driver) DeleteData(ctx context.Context, path string) (bool, error) {
	d.modulesMux.RLock()
	defer d.modulesMux.RUnlock()

	p, err := parsePath(path)
	if err != nil {
		return false, err
	}

	txn, err := d.storage.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return false, fmt.Errorf("%w: %v", clienterrors.ErrTransaction, err)
	}

	if err = d.storage.Write(ctx, txn, storage.RemoveOp, p, interface{}(nil)); err != nil {
		d.storage.Abort(ctx, txn)
		if storage.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("%w: unable to write data: %v", clienterrors.ErrWrite, err)
	}

	if err = d.storage.Commit(ctx, txn); err != nil {
		return false, fmt.Errorf("%w: %v", clienterrors.ErrTransaction, err)
	}

	return true, nil
}

func (d *Driver) eval(ctx context.Context, path string, input interface{}, cfg *drivers.QueryCfg) (rego.ResultSet, *string, error) {
	d.modulesMux.RLock()
	defer d.modulesMux.RUnlock()

	args := []func(*rego.Rego){
		rego.Compiler(d.compiler),
		rego.Store(d.storage),
		rego.Input(input),
		rego.Query(path),
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
		t = pointer.StringPtr(b.String())
	}

	return res, t, err
}

func (d *Driver) Query(ctx context.Context, path string, input interface{}, opts ...drivers.QueryOpt) (*types.Response, error) {
	cfg := &drivers.QueryCfg{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Add a variable binding to the path.
	path = fmt.Sprintf("data.%s[result]", path)

	rs, trace, err := d.eval(ctx, path, input, cfg)
	if err != nil {
		return nil, err
	}

	var results []*types.Result
	for _, r := range rs {
		result := &types.Result{}
		b, err := json.Marshal(r.Bindings["result"])
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, result); err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	inp, err := json.MarshalIndent(input, "", "   ")
	if err != nil {
		return nil, err
	}

	return &types.Response{
		Trace:   trace,
		Results: results,
		Input:   pointer.StringPtr(string(inp)),
	}, nil
}

func (d *Driver) Dump(ctx context.Context) (string, error) {
	d.modulesMux.RLock()
	defer d.modulesMux.RUnlock()

	mods := make(map[string]string, len(d.modules))
	for k, v := range d.modules {
		mods[k] = v.String()
	}

	data, _, err := d.eval(ctx, "data", nil, &drivers.QueryCfg{})
	if err != nil {
		return "", err
	}

	var dt interface{}
	// There should be only 1 or 0 expression values
	if len(data) > 1 {
		return "", errors.New("too many dump results")
	}

	for _, da := range data {
		if len(data) > 1 {
			return "", errors.New("too many expressions results")
		}

		for _, e := range da.Expressions {
			dt = e.Value
		}
	}

	resp := map[string]interface{}{
		"modules": mods,
		"data":    dt,
	}

	b, err := json.MarshalIndent(resp, "", "   ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// ValidateConstraintTemplate validates the rego in template target by parsing
// rego modules.
func (d *Driver) ValidateConstraintTemplate(templ *templates.ConstraintTemplate) (string, []string, error) {
	if err := validateTargets(templ); err != nil {
		return "", nil, err
	}
	targetSpec := templ.Spec.Targets[0]
	targetHandler := targetSpec.Target
	kind := templ.Spec.CRD.Spec.Names.Kind
	pkgPrefix := templateLibPrefix(targetHandler, kind)

	rr, err := regorewriter.New(
		regorewriter.NewPackagePrefixer(pkgPrefix),
		[]string{libRoot},
		d.externs)
	if err != nil {
		return "", nil, fmt.Errorf("creating rego rewriter: %w", err)
	}

	namePrefix := createTemplatePath(targetHandler, kind)
	entryPoint, err := parseModule(namePrefix, templ.Spec.Targets[0].Rego)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", clienterrors.ErrInvalidConstraintTemplate, err)
	}

	if entryPoint == nil {
		return "", nil, fmt.Errorf("%w: failed to parse module for unknown reason",
			clienterrors.ErrInvalidConstraintTemplate)
	}

	if err = rewriteModulePackage(namePrefix, entryPoint); err != nil {
		return "", nil, err
	}

	req := map[string]struct{}{violation: {}}

	if err = requireModuleRules(entryPoint, req); err != nil {
		return "", nil, fmt.Errorf("%w: invalid rego: %v",
			clienterrors.ErrInvalidConstraintTemplate, err)
	}

	rr.AddEntryPointModule(namePrefix, entryPoint)
	for idx, libSrc := range targetSpec.Libs {
		libPath := fmt.Sprintf(`%s["lib_%d"]`, pkgPrefix, idx)
		if err = rr.AddLib(libPath, libSrc); err != nil {
			return "", nil, fmt.Errorf("%w: %v",
				clienterrors.ErrInvalidConstraintTemplate, err)
		}
	}

	sources, err := rr.Rewrite()
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v",
			clienterrors.ErrInvalidConstraintTemplate, err)
	}

	var mods []string
	err = sources.ForEachModule(func(m *regorewriter.Module) error {
		content, err2 := m.Content()
		if err2 != nil {
			return err2
		}
		mods = append(mods, string(content))
		return nil
	})
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v",
			clienterrors.ErrInvalidConstraintTemplate, err)
	}
	return namePrefix, mods, nil
}

// AddTemplate implements drivers.Driver.
func (d *Driver) AddTemplate(templ *templates.ConstraintTemplate) error {
	namePrefix, mods, err := d.ValidateConstraintTemplate(templ)
	if err != nil {
		return err
	}
	if err = d.putModules(namePrefix, mods); err != nil {
		return fmt.Errorf("%w: %v", clienterrors.ErrCompile, err)
	}
	d.thMux.Lock()
	defer d.thMux.Unlock()
	d.templateHandler[templ.Spec.CRD.Spec.Names.Kind] = []string{
		templ.Spec.Targets[0].Target,
	}
	return nil
}

// RemoveTemplate implements driver.Driver.
func (d *Driver) RemoveTemplate(templ *templates.ConstraintTemplate) error {
	if len(templ.Spec.Targets) != 1 {
		// TODO: Properly handle multi-target deletes.
		//  This is likely trivially covered by compiler sharding since we'll
		//  control how data is stored per-Template.
		//  https://github.com/open-policy-agent/frameworks/issues/197
		// The template has an unexpected number of targets at removal time, so do
		// nothing.
		return nil
	}

	targetHandler := templ.Spec.Targets[0].Target
	kind := templ.Spec.CRD.Spec.Names.Kind
	namePrefix := createTemplatePath(targetHandler, kind)
	_, err := d.deleteModules(namePrefix)
	d.thMux.Lock()
	defer d.thMux.Unlock()
	delete(d.templateHandler, templ.Spec.CRD.Spec.Names.Kind)
	return err
}

func (d *Driver) AddConstraint(ctx context.Context, constraint *unstructured.Unstructured) error {
	handlers, err := d.getTargetHandlers(constraint)
	if err != nil {
		return err
	}

	for _, target := range handlers {
		relPath, err := createConstraintPath(target, constraint)
		// If we ever create multi-target constraints we will need to handle this more cleverly.
		// the short-circuiting question, cleanup, etc.
		if err != nil {
			return err
		}
		if err := d.PutData(ctx, relPath, constraint.Object); err != nil {
			return err
		}
	}
	return nil
}

func (d *Driver) RemoveConstraint(ctx context.Context, constraint *unstructured.Unstructured) error {
	handlers, err := d.getTargetHandlers(constraint)
	if err != nil {
		if !errors.Is(err, clienterrors.ErrMissingConstraintTemplate) {
			return err
		}
	}

	for _, target := range handlers {
		relPath, err := createConstraintPath(target, constraint)
		// If we ever create multi-target constraints we will need to handle this more cleverly.
		// the short-circuiting question, cleanup, etc.
		if err != nil {
			return err
		}
		if _, err := d.DeleteData(ctx, relPath); err != nil {
			return err
		}
	}
	return nil
}

// templateLibPrefix returns the new lib prefix for the libs that are specified in the CT.
func templateLibPrefix(target, name string) string {
	return fmt.Sprintf("libs.%s.%s", target, name)
}

// createTemplatePath returns the package path for a given template: templates.<target>.<name>.
func createTemplatePath(target, name string) string {
	return fmt.Sprintf(`templates["%s"]["%s"]`, target, name)
}

// parseModule parses the module and also fails empty modules.
func parseModule(path, rego string) (*ast.Module, error) {
	module, err := ast.ParseModule(path, rego)
	if err != nil {
		return nil, err
	}

	if module == nil {
		return nil, fmt.Errorf("%w: module %q is empty",
			clienterrors.ErrInvalidModule, path)
	}

	return module, nil
}

// rewriteModulePackage rewrites the module's package path to path.
func rewriteModulePackage(path string, module *ast.Module) error {
	pathParts, err := ast.ParseRef(path)
	if err != nil {
		return err
	}

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

// validateTargets ensures that the targets field has the appropriate values.
func validateTargets(templ *templates.ConstraintTemplate) error {
	if templ == nil {
		return fmt.Errorf(`%w: ConstraintTemplate is nil`,
			clienterrors.ErrInvalidConstraintTemplate)
	}
	targets := templ.Spec.Targets
	if targets == nil {
		return fmt.Errorf(`%w: field "targets" not specified in ConstraintTemplate spec`,
			clienterrors.ErrInvalidConstraintTemplate)
	}

	switch len(targets) {
	case 0:
		return fmt.Errorf("%w: no targets specified: ConstraintTemplate must specify one target",
			clienterrors.ErrInvalidConstraintTemplate)
	case 1:
		return nil
	default:
		return fmt.Errorf("%w: multi-target templates are not currently supported",
			clienterrors.ErrInvalidConstraintTemplate)
	}
}

// createConstraintGKPath returns the subpath for given a constraint GK.
func createConstraintGKSubPath(gk schema.GroupKind) string {
	return "/" + path.Join("cluster", gk.Group, gk.Kind)
}

// createConstraintSubPath returns the key where we will store the constraint
// for each target: cluster.<group>.<kind>.<name>.
func createConstraintSubPath(constraint *unstructured.Unstructured) (string, error) {
	if constraint.GetName() == "" {
		return "", fmt.Errorf("%w: missing name", clienterrors.ErrInvalidConstraint)
	}

	gvk := constraint.GroupVersionKind()
	if gvk.Group != constraints.Group {
		return "", fmt.Errorf("%w: expect group %q for constrant %q, got %q",
			clienterrors.ErrInvalidConstraint, constraints.Group, constraint.GetName(), gvk.Group)
	}

	if gvk.Kind == "" {
		return "", fmt.Errorf("%w: empty kind for constraint %q",
			clienterrors.ErrInvalidConstraint, constraint.GetName())
	}

	return path.Join(createConstraintGKSubPath(gvk.GroupKind()), constraint.GetName()), nil
}

// constraintPathMerge is a shared function for creating constraint paths to
// ensure uniformity, it is not meant to be called directly.
func constraintPathMerge(target, subpath string) string {
	return "/" + path.Join("constraints", target, subpath)
}

// createConstraintPath returns the storage path for a given constraint: constraints.<target>.cluster.<group>.<kind>.<name>.
func createConstraintPath(target string, constraint *unstructured.Unstructured) (string, error) {
	p, err := createConstraintSubPath(constraint)
	if err != nil {
		return "", err
	}
	return constraintPathMerge(target, p), nil
}

func (d *Driver) getTargetHandlers(constraint *unstructured.Unstructured) ([]string, error) {
	d.thMux.RLock()
	defer d.thMux.RUnlock()

	kind := constraint.GetKind()
	handlers, ok := d.templateHandler[kind]
	if !ok {
		var known []string
		for k := range d.templateHandler {
			known = append(known, k)
		}
		return nil, fmt.Errorf("%w: Constraint kind %q is not recognized, known kinds %v",
			clienterrors.ErrMissingConstraintTemplate, kind, known)
	}
	return handlers, nil
}

func (d *Driver) SetExterns(fields []string) {
	d.externs = fields
}
