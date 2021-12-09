package client

import (
	"fmt"
	"sort"

	"github.com/open-policy-agent/opa/ast"
)

// Currently rules should only access data.inventory.
var validDataFields = map[string]bool{
	"inventory": true,
}

// parseModule parses the module and also fails empty modules.
func parseModule(path, rego string) (*ast.Module, error) {
	module, err := ast.ParseModule(path, rego)
	if err != nil {
		return nil, err
	}

	if module == nil {
		return nil, fmt.Errorf("%w: module %q is empty",
			ErrInvalidModule, path)
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
			ErrInvalidModule, missing)
	}

	return nil
}
