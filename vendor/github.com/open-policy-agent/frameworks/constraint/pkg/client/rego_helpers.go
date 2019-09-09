package client

import (
	"errors"
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/ast"
)

var (
	// Currently rules should only access data.inventory
	validDataFields = map[string]bool{
		"inventory": true,
	}
)

func newRegoConformer(allowedDataFields []string) *regoConformer {
	allowed := make(map[string]bool)
	for _, v := range allowedDataFields {
		if !validDataFields[v] {
			continue
		}
		allowed[v] = true
	}
	return &regoConformer{allowedDataFields: allowed}
}

type regoConformer struct {
	allowedDataFields map[string]bool
}

// ensureRegoConformance rewrites the package path and ensures there is no access of `data`
// beyond the whitelisted bits. Note that this rewriting will currently modify the Rego to look
// potentially very different from the input, but it will still be functionally equivalent.
func (rc *regoConformer) ensureRegoConformance(kind, path, rego string) (string, error) {
	if rego == "" {
		return "", errors.New("Rego source code is empty")
	}
	module, err := ast.ParseModule(kind, rego)
	if err != nil {
		return "", err
	}
	if module == nil {
		return "", errors.New("Module could not be parsed")
	}
	if len(module.Imports) != 0 {
		return "", errors.New("Use of the `import` keyword is not allowed")
	}
	// Temporarily unset Package.Path to avoid triggering a "prohibited data field" error
	module.Package.Path = nil
	if err := rc.checkDataAccess(module); err != nil {
		return "", err
	}
	module.Package.Path, err = packageRef(path)
	if err != nil {
		return "", err
	}
	return module.String(), nil
}

// rewritePackage rewrites the package in a rego module
func rewritePackage(path, rego string) (string, error) {
	if rego == "" {
		return "", errors.New("Rego source code is empty")
	}
	module, err := ast.ParseModule(path, rego)
	if err != nil {
		return "", err
	}
	if module == nil {
		return "", errors.New("Module could not be parsed")
	}
	module.Package.Path, err = packageRef(path)
	if err != nil {
		return "", err
	}
	return module.String(), nil
}

// packageRef constructs a Ref to the provided package path string
func packageRef(path string) (ast.Ref, error) {
	pathParts, err := ast.ParseRef(path)
	if err != nil {
		return nil, err
	}
	packageRef := ast.Ref([]*ast.Term{ast.VarTerm("data")})
	return packageRef.Extend(pathParts), nil
}

func makeInvalidRootFieldErr(val ast.Value, allowed map[string]bool) error {
	if len(allowed) == 0 {
		return fmt.Errorf("Template is attempting to access `data.%s`. Access to the data document is disabled", val.String())
	}
	var validFields []string
	for field := range allowed {
		validFields = append(validFields, field)
	}
	return fmt.Errorf("Invalid `data` field: %s. Valid fields are: %s", val.String(), strings.Join(validFields, ", "))
}

var _ error = Errors{}

type Errors []error

func (errs Errors) Error() string {
	s := make([]string, len(errs))
	for _, e := range errs {
		s = append(s, e.Error())
	}
	return strings.Join(s, "\n")
}

// checkDataAccess makes sure that data is only referenced in terms of valid subfields
func (rc *regoConformer) checkDataAccess(module *ast.Module) Errors {
	var errs Errors
	ast.WalkRefs(module, func(r ast.Ref) bool {
		if r.HasPrefix(ast.DefaultRootRef) {
			if len(r) < 2 {
				errs = append(errs, fmt.Errorf("All references to `data` must access a field of `data`: %s", r))
				return false
			}
			if !r[1].IsGround() {
				errs = append(errs, fmt.Errorf("Fields of `data` must be accessed with a literal value (e.g. `data.inventory`, not `data[var]`): %s", r))
				return false
			}
			v := r[1].Value
			if val, ok := v.(ast.String); !ok {
				errs = append(errs, makeInvalidRootFieldErr(v, rc.allowedDataFields))
				return false
			} else {
				if !rc.allowedDataFields[string(val)] {
					errs = append(errs, makeInvalidRootFieldErr(v, rc.allowedDataFields))
					return false
				}
			}
		}
		return false
	})

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// rule name -> arity
type ruleArities map[string]int

// requireRules makes sure the listed rules are specified with the required arity
func requireRules(name, rego string, reqs ruleArities) error {
	if rego == "" {
		return errors.New("Rego source code is empty")
	}
	module, err := ast.ParseModule(name, rego)
	if err != nil {
		return err
	}
	if module == nil {
		return errors.New("Module could not be parsed")
	}

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
