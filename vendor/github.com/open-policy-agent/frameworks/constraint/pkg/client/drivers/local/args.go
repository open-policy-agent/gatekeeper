package local

import (
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown/print"
	opatypes "github.com/open-policy-agent/opa/types"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
)

type Arg func(*Driver) error

func Defaults() Arg {
	return func(d *Driver) error {
		if d.storage.storage == nil {
			d.storage.storage = make(map[string]storage.Store)
		}

		if d.compilers.capabilities == nil {
			d.compilers.capabilities = ast.CapabilitiesForThisVersion()
		}

		if d.compilers.externs == nil {
			for allowed := range validDataFields {
				d.compilers.externs = append(d.compilers.externs, fmt.Sprintf("data.%s", allowed))
			}
		}

		if d.targets == nil {
			d.targets = make(map[string][]string)
		}

		// adding external_data builtin otherwise capabilities get overridden
		// if a capability, like http.send, is disabled
		if d.providerCache != nil {
			newBuiltin := &ast.Builtin{
				Name: "external_data",
				Decl: opatypes.NewFunction(opatypes.Args(opatypes.A), opatypes.A),
			}
			d.compilers.capabilities.Builtins = append(d.compilers.capabilities.Builtins, newBuiltin)
		}

		if d.sendRequestToProvider == nil {
			d.sendRequestToProvider = externaldata.DefaultSendRequestToProvider
		}

		return nil
	}
}

func Tracing(enabled bool) Arg {
	return func(d *Driver) error {
		d.traceEnabled = enabled

		return nil
	}
}

func PrintEnabled(enabled bool) Arg {
	return func(d *Driver) error {
		d.printEnabled = enabled

		return nil
	}
}

func PrintHook(hook print.Hook) Arg {
	return func(d *Driver) error {
		d.printHook = hook

		return nil
	}
}

func Storage(s map[string]storage.Store) Arg {
	return func(d *Driver) error {
		d.storage = storages{storage: s}

		return nil
	}
}

func AddExternalDataProviderCache(providerCache *externaldata.ProviderCache) Arg {
	return func(d *Driver) error {
		d.providerCache = providerCache

		return nil
	}
}

func DisableBuiltins(builtins ...string) Arg {
	return func(d *Driver) error {
		if d.compilers.capabilities == nil {
			d.compilers.capabilities = ast.CapabilitiesForThisVersion()
		}

		disableBuiltins := make(map[string]bool)
		for _, b := range builtins {
			disableBuiltins[b] = true
		}

		var newBuiltins []*ast.Builtin
		for _, b := range d.compilers.capabilities.Builtins {
			if !disableBuiltins[b.Name] {
				newBuiltins = append(newBuiltins, b)
			}
		}

		d.compilers.capabilities.Builtins = newBuiltins

		return nil
	}
}

func AddExternalDataClientCertWatcher(clientCertWatcher *certwatcher.CertWatcher) Arg {
	return func(d *Driver) error {
		d.clientCertWatcher = clientCertWatcher

		return nil
	}
}

func EnableExternalDataClientAuth() Arg {
	return func(d *Driver) error {
		d.enableExternalDataClientAuth = true

		return nil
	}
}

// Externs sets the fields under `data` that Rego in ConstraintTemplates
// can access. If unset, all fields can be accessed. Only fields recognized by
// the system can be enabled.
func Externs(externs ...string) Arg {
	return func(driver *Driver) error {
		fields := make([]string, len(externs))

		for i, field := range externs {
			if !validDataFields[field] {
				return fmt.Errorf("%w: invalid data field %q; allowed fields are: %v",
					errors.ErrCreatingDriver, field, validDataFields)
			}

			fields[i] = fmt.Sprintf("data.%s", field)
		}

		driver.compilers.externs = fields

		return nil
	}
}

// Currently rules should only access data.inventory.
var validDataFields = map[string]bool{
	"inventory": true,
}
