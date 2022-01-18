package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"

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
)

type module struct {
	text   string
	parsed *ast.Module
}

type insertParam map[string]*module

func (i insertParam) add(name string, src string) error {
	m, err := ast.ParseModule(name, src)
	if err != nil {
		return fmt.Errorf("%w: %q: %v", ErrParse, name, err)
	}

	i[name] = &module{text: src, parsed: m}
	return nil
}

func New(args ...Arg) drivers.Driver {
	d := &driver{}
	for _, arg := range args {
		arg(d)
	}

	Defaults()(d)

	d.compiler.WithCapabilities(d.capabilities)

	return d
}

var _ drivers.Driver = &driver{}

type driver struct {
	modulesMux    sync.RWMutex
	compiler      *ast.Compiler
	modules       map[string]*ast.Module
	storage       storage.Store
	capabilities  *ast.Capabilities
	traceEnabled  bool
	printEnabled  bool
	printHook     print.Hook
	providerCache *externaldata.ProviderCache
}

func (d *driver) Init() error {
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

func (d *driver) checkModuleName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: module %q has no name",
			ErrModuleName, name)
	}

	if strings.HasPrefix(name, moduleSetPrefix) {
		return fmt.Errorf("%w: module %q has forbidden prefix %q",
			ErrModuleName, name, moduleSetPrefix)
	}

	return nil
}

func (d *driver) checkModuleSetName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: modules name prefix cannot be empty", ErrModulePrefix)
	}

	if strings.Contains(name, moduleSetSep) {
		return fmt.Errorf("%w: modules name prefix not allowed to contain the sequence %q", ErrModulePrefix, moduleSetSep)
	}

	return nil
}

func toModuleSetPrefix(prefix string) string {
	return fmt.Sprintf("%s%s%s", moduleSetPrefix, prefix, moduleSetSep)
}

func toModuleSetName(prefix string, idx int) string {
	return fmt.Sprintf("%s%d", toModuleSetPrefix(prefix), idx)
}

func (d *driver) PutModule(name string, src string) error {
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

// PutModules implements drivers.Driver.
func (d *driver) PutModules(namePrefix string, srcs []string) error {
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
func (d *driver) alterModules(insert insertParam, remove []string) (int, error) {
	// TODO(davis-haba): Remove this Context once it is no longer necessary.
	ctx := context.TODO()

	updatedModules := copyModules(d.modules)
	for _, name := range remove {
		delete(updatedModules, name)
	}

	for name, mod := range insert {
		updatedModules[name] = mod.parsed
	}

	txn, err := d.storage.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return 0, err
	}

	for _, name := range remove {
		if err := d.storage.DeletePolicy(ctx, txn, name); err != nil {
			d.storage.Abort(ctx, txn)
			return 0, err
		}
	}

	c := ast.NewCompiler().WithPathConflictsCheck(storage.NonEmpty(ctx, d.storage, txn)).
		WithCapabilities(d.capabilities).
		WithEnablePrintStatements(d.printEnabled)

	if c.Compile(updatedModules); c.Failed() {
		d.storage.Abort(ctx, txn)
		return 0, fmt.Errorf("%w: %v", ErrCompile, c.Errors)
	}

	for name, mod := range insert {
		if err := d.storage.UpsertPolicy(ctx, txn, name, []byte(mod.text)); err != nil {
			d.storage.Abort(ctx, txn)
			return 0, err
		}
	}

	if err := d.storage.Commit(ctx, txn); err != nil {
		return 0, err
	}

	d.compiler = c
	d.modules = updatedModules

	return len(remove), nil
}

// DeleteModules implements drivers.Driver.
func (d *driver) DeleteModules(namePrefix string) (int, error) {
	if err := d.checkModuleSetName(namePrefix); err != nil {
		return 0, err
	}

	d.modulesMux.Lock()
	defer d.modulesMux.Unlock()

	return d.alterModules(nil, d.listModuleSet(namePrefix))
}

// listModuleSet returns the list of names corresponding to a given module
// prefix.
func (d *driver) listModuleSet(namePrefix string) []string {
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
		return nil, fmt.Errorf("%w: path must begin with '/': %q", ErrPathInvalid, path)
	}
	if len(p) == 0 {
		return nil, fmt.Errorf("%w: path must contain at least one path element: %q", ErrPathInvalid, path)
	}

	return p, nil
}

func (d *driver) PutData(ctx context.Context, path string, data interface{}) error {
	d.modulesMux.RLock()
	defer d.modulesMux.RUnlock()

	p, err := parsePath(path)
	if err != nil {
		return err
	}

	txn, err := d.storage.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTransaction, err)
	}

	if _, err = d.storage.Read(ctx, txn, p); err != nil {
		if storage.IsNotFound(err) {
			if err = storage.MakeDir(ctx, d.storage, txn, p[:len(p)-1]); err != nil {
				return fmt.Errorf("%w: unable to make directory: %v", ErrWrite, err)
			}
		} else {
			d.storage.Abort(ctx, txn)
			return fmt.Errorf("%w: %v", ErrRead, err)
		}
	}

	if err = d.storage.Write(ctx, txn, storage.AddOp, p, data); err != nil {
		d.storage.Abort(ctx, txn)
		return fmt.Errorf("%w: unable to write data: %v", ErrWrite, err)
	}

	// TODO: Determine if this can be removed. No tests exercise this path, and
	//  as far as I can tell storage.MakeDir fails where this might return an error.
	if errs := ast.CheckPathConflicts(d.compiler, storage.NonEmpty(ctx, d.storage, txn)); len(errs) > 0 {
		d.storage.Abort(ctx, txn)
		return fmt.Errorf("%w: %q conflicts with existing path: %v",
			ErrPathConflict, path, errs)
	}

	err = d.storage.Commit(ctx, txn)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTransaction, err)
	}
	return nil
}

// DeleteData deletes data from OPA and returns true if data was found and deleted, false
// if data was not found, and any errors.
func (d *driver) DeleteData(ctx context.Context, path string) (bool, error) {
	d.modulesMux.RLock()
	defer d.modulesMux.RUnlock()

	p, err := parsePath(path)
	if err != nil {
		return false, err
	}

	txn, err := d.storage.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrTransaction, err)
	}

	if err = d.storage.Write(ctx, txn, storage.RemoveOp, p, interface{}(nil)); err != nil {
		d.storage.Abort(ctx, txn)
		if storage.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("%w: unable to write data: %v", ErrWrite, err)
	}

	if err = d.storage.Commit(ctx, txn); err != nil {
		return false, fmt.Errorf("%w: %v", ErrTransaction, err)
	}

	return true, nil
}

func (d *driver) eval(ctx context.Context, path string, input interface{}, cfg *drivers.QueryCfg) (rego.ResultSet, *string, error) {
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

func (d *driver) Query(ctx context.Context, path string, input interface{}, opts ...drivers.QueryOpt) (*types.Response, error) {
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

func (d *driver) Dump(ctx context.Context) (string, error) {
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
