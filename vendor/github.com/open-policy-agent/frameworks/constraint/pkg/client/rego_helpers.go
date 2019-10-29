package client

import (
	"fmt"

	"github.com/open-policy-agent/opa/ast"
	"github.com/pkg/errors"
)

var (
	// Currently rules should only access data.inventory
	validDataFields = map[string]bool{
		"inventory": true,
	}
)

// parseModule parses the module and also fails empty modules.
func parseModule(path, rego string) (*ast.Module, error) {
	module, err := ast.ParseModule(path, rego)
	if err != nil {
		return nil, err
	}
	if module == nil {
		return nil, errors.New("Empty module")
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

// rule name -> arity
type ruleArities map[string]int

// requireRulesModule makes sure the listed rules are specified with the required arity
func requireRulesModule(module *ast.Module, reqs ruleArities) error {
	arities := make(ruleArities, len(module.Rules))
	for _, rule := range module.Rules {
		name := string(rule.Head.Name)
		arity, err := getRuleArity(rule)
		if err != nil {
			return err
		}
		arities[name] = arity
	}

	var errs Errors
	for name, arity := range reqs {
		actual, ok := arities[name]
		if !ok {
			errs = append(errs, fmt.Errorf("Missing required rule: %s", name))
			continue
		}
		if arity != actual {
			errs = append(errs, fmt.Errorf("Rule %s has arity %d, want %d", name, actual, arity))
		}
	}
	if len(errs) != 0 {
		return errs
	}
	return nil
}

// getRuleArity returns the arity of a rule, assuming only no variables, a single variable, or
// an array of variables
func getRuleArity(r *ast.Rule) (int, error) {
	t := r.Head.Key
	if t == nil {
		return 0, nil
	}
	switch v := t.Value.(type) {
	case ast.Var:
		return 1, nil
	case ast.Object:
		return 1, nil
	case ast.Array:
		errs := false
		for _, e := range v {
			if _, ok := e.Value.(ast.Var); !ok {
				// for multi-arity args, a dev may be building the review object in the head of the rule
				if _, ok := e.Value.(ast.Object); !ok {
					errs = true
				}
			}
		}
		if errs {
			return 0, fmt.Errorf("Invalid rule signature: only single variables or arrays of variables or objects allowed: %s", v.String())
		}
		return len(v), nil
	}
	return 0, fmt.Errorf("Invalid rule signature, only variables or arrays allowed: %s", t.String())
}
