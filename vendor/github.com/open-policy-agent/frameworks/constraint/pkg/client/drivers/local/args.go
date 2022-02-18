package local

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/frameworks/constraint/pkg/handler"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/storage/inmem"
	"github.com/open-policy-agent/opa/topdown/print"
	opatypes "github.com/open-policy-agent/opa/types"
)

type Arg func(*Driver)

func Defaults() Arg {
	return func(d *Driver) {
		if d.compiler == nil {
			d.compiler = ast.NewCompiler()
		}

		if d.modules == nil {
			d.modules = make(map[string]*ast.Module)
		}
		if d.templateHandler == nil {
			d.templateHandler = make(map[string][]string)
		}
		if d.storage == nil {
			d.storage = inmem.New()
		}

		if d.capabilities == nil {
			d.capabilities = ast.CapabilitiesForThisVersion()
		}

		// adding external_data builtin otherwise capabilities get overridden
		// if a capability, like http.send, is disabled
		if d.providerCache != nil {
			d.capabilities.Builtins = append(d.capabilities.Builtins, &ast.Builtin{
				Name: "external_data",
				Decl: opatypes.NewFunction(opatypes.Args(opatypes.A), opatypes.A),
			})
		}
	}
}

func Tracing(enabled bool) Arg {
	return func(d *Driver) {
		d.traceEnabled = enabled
	}
}

func PrintEnabled(enabled bool) Arg {
	return func(d *Driver) {
		d.printEnabled = enabled
	}
}

func PrintHook(hook print.Hook) Arg {
	return func(d *Driver) {
		d.printHook = hook
	}
}

func Modules(modules map[string]*ast.Module) Arg {
	return func(d *Driver) {
		d.modules = modules
	}
}

func Storage(s storage.Store) Arg {
	return func(d *Driver) {
		d.storage = s
	}
}

func AddExternalDataProviderCache(providerCache *externaldata.ProviderCache) Arg {
	return func(d *Driver) {
		d.providerCache = providerCache
	}
}

func DisableBuiltins(builtins ...string) Arg {
	return func(d *Driver) {
		if d.capabilities == nil {
			d.capabilities = ast.CapabilitiesForThisVersion()
		}

		disableBuiltins := make(map[string]bool)
		for _, b := range builtins {
			disableBuiltins[b] = true
		}

		var nb []*ast.Builtin
		builtins := d.capabilities.Builtins
		for i, b := range builtins {
			if !disableBuiltins[b.Name] {
				nb = append(nb, builtins[i])
			}
		}

		d.capabilities.Builtins = nb
	}
}

func Handlers(handlers ...handler.TargetHandler) Arg {
	return func(d *Driver) {
		if d.handlers == nil {
			d.handlers = make(map[string]handler.TargetHandler)
		}
		for _, h := range handlers {
			d.handlers[h.GetName()] = h
		}
	}
}
