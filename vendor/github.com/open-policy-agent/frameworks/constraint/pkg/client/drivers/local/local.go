package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/storage/inmem"
	"github.com/open-policy-agent/opa/topdown"
)

type arg func(*driver)

func Tracing(enabled bool) arg {
	return func(d *driver) {
		d.traceEnabled = enabled
	}
}

func New(args ...arg) drivers.Driver {
	d := &driver{
		compiler: ast.NewCompiler(),
		modules:  make(map[string]*ast.Module),
		storage:  inmem.New(),
	}
	for _, arg := range args {
		arg(d)
	}
	return d
}

var _ drivers.Driver = &driver{}

type driver struct {
	modulesMux   sync.RWMutex
	compiler     *ast.Compiler
	modules      map[string]*ast.Module
	storage      storage.Store
	traceEnabled bool
}

func (d *driver) Init(ctx context.Context) error {
	return nil
}

func copyModules(modules map[string]*ast.Module, filter string) map[string]*ast.Module {
	m := make(map[string]*ast.Module, len(modules))
	for k, v := range modules {
		if filter != "" && k == filter {
			continue
		}
		m[k] = v
	}
	return m
}

func (d *driver) PutModule(ctx context.Context, name string, src string) error {
	d.modulesMux.Lock()
	defer d.modulesMux.Unlock()
	module, err := ast.ParseModule(name, src)
	if err != nil {
		return err
	}
	modules := copyModules(d.modules, "")
	modules[name] = module
	txn, err := d.storage.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return err
	}
	c := ast.NewCompiler().WithPathConflictsCheck(storage.NonEmpty(ctx, d.storage, txn))
	if c.Compile(modules); c.Failed() {
		d.storage.Abort(ctx, txn)
		return c.Errors
	}
	if err := d.storage.UpsertPolicy(ctx, txn, name, []byte(src)); err != nil {
		d.storage.Abort(ctx, txn)
		return err
	}
	if err := d.storage.Commit(ctx, txn); err != nil {
		return err
	}
	d.modules[name] = module
	d.compiler = c
	return nil
}

// DeleteModule deletes a rule from OPA and returns true if a rule was found and deleted, false
// if a rule was not found, and any errors
func (d *driver) DeleteModule(ctx context.Context, name string) (bool, error) {
	d.modulesMux.Lock()
	defer d.modulesMux.Unlock()
	if _, ok := d.modules[name]; !ok {
		return false, nil
	}
	modules := copyModules(d.modules, name)
	txn, err := d.storage.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return false, err
	}
	if err := d.storage.DeletePolicy(ctx, txn, name); err != nil {
		d.storage.Abort(ctx, txn)
		return false, err
	}
	c := ast.NewCompiler()
	if c.Compile(modules); c.Failed() {
		d.storage.Abort(ctx, txn)
		return false, err
	}
	if err := d.storage.Commit(ctx, txn); err != nil {
		return false, err
	}
	d.compiler = c
	delete(d.modules, name)
	return true, nil
}

func parsePath(path string) ([]string, error) {
	p, ok := storage.ParsePathEscaped(path)
	if !ok {
		return nil, fmt.Errorf("Bad data path: %s", path)
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
		return err
	}
	if _, err := d.storage.Read(ctx, txn, p); err != nil {
		if storage.IsNotFound(err) {
			storage.MakeDir(ctx, d.storage, txn, p[:len(p)-1])
		} else {
			d.storage.Abort(ctx, txn)
			return err
		}
	}
	if err := d.storage.Write(ctx, txn, storage.AddOp, p, data); err != nil {
		d.storage.Abort(ctx, txn)
		return err
	}
	if err := ast.CheckPathConflicts(d.compiler, storage.NonEmpty(ctx, d.storage, txn)); len(err) > 0 {
		d.storage.Abort(ctx, txn)
		return err
	}
	if err := d.storage.Commit(ctx, txn); err != nil {
		return err
	}
	return nil
}

// DeleteData deletes data from OPA and returns true if data was found and deleted, false
// if data was not found, and any errors
func (d *driver) DeleteData(ctx context.Context, path string) (bool, error) {
	d.modulesMux.RLock()
	defer d.modulesMux.RUnlock()
	p, err := parsePath(path)
	if err != nil {
		return false, err
	}
	txn, err := d.storage.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return false, err
	}
	if err := d.storage.Write(ctx, txn, storage.RemoveOp, p, interface{}(nil)); err != nil {
		d.storage.Abort(ctx, txn)
		if storage.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if err := d.storage.Commit(ctx, txn); err != nil {
		return false, err
	}
	return true, nil
}

func (d *driver) eval(ctx context.Context, path string, input interface{}) (rego.ResultSet, *string, error) {
	d.modulesMux.RLock()
	defer d.modulesMux.RUnlock()
	args := []func(*rego.Rego){
		rego.Compiler(d.compiler),
		rego.Store(d.storage),
		rego.Input(input),
		rego.Query(path),
	}
	if d.traceEnabled {
		buf := topdown.NewBufferTracer()
		args = append(args, rego.Tracer(buf))
		rego := rego.New(args...)
		res, err := rego.Eval(ctx)
		b := &bytes.Buffer{}
		topdown.PrettyTrace(b, *buf)
		t := b.String()
		return res, &t, err
	}
	rego := rego.New(args...)
	res, err := rego.Eval(ctx)
	return res, nil, err
}

func (d *driver) Query(ctx context.Context, path string, input interface{}) (*types.Response, error) {
	inp, err := json.MarshalIndent(input, "", "   ")
	if err != nil {
		return nil, err
	}
	// Add a variable binding to the path
	path = fmt.Sprintf("data.%s[result]", path)
	rs, trace, err := d.eval(ctx, path, input)
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
	i := string(inp)
	return &types.Response{
		Trace:   trace,
		Results: results,
		Input:   &i,
	}, nil
}

func (d *driver) Dump(ctx context.Context) (string, error) {
	d.modulesMux.RLock()
	defer d.modulesMux.RUnlock()
	mods := make(map[string]string, len(d.modules))
	for k, v := range d.modules {
		mods[k] = v.String()
	}
	data, _, err := d.eval(ctx, "data", nil)
	if err != nil {
		return "", err
	}
	var dt interface{}
	// There should be only 1 or 0 expression values
	if len(data) > 1 {
		return "", errors.New("Too many dump results")
	}
	for _, da := range data {
		if len(data) > 1 {
			return "", errors.New("Too many expressions results")
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
