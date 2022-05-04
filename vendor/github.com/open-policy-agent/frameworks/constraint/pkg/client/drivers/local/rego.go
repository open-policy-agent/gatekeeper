package local

import "github.com/open-policy-agent/opa/ast"

const (
	// templatePath is the path the Template's Rego code is stored.
	// Must match the "data.xxx.violation[r]" path in hookModule.
	templatePath = "template"

	// templateLibPrefix is the path under which library Rego code is stored.
	// Must match "data.xxx.[library package]" path.
	templateLibPrefix = "libs"

	// hookModulePath.
	hookModulePath = "hooks.hooks_builtin"

	// hookModule specifies how Template violations are run in Rego.
	// This removes boilerplate that would otherwise need to be present in every
	// Template's Rego code. The violation's response is written to a standard
	// location we can read from to see if any violations occurred.
	hookModuleRego = `
package hooks

# Determine if the object under review violates any passed Constraints.
violation[response] {
  # Iterate over all keys to Constraints in storage.
  key := input.constraints[_]

  # Construct the input object from the Constraint and temporary object in storage.
  # Silently exits if the Constraint no longer exists.
  inp := {
    "review": input.review,
    "parameters": data.constraints[key.kind][key.name],
  }

  # Run the Template with Constraint.
  inventory[inv]
  data.template.violation[r] with input as inp with data.inventory as inv

  # Construct the response, defaulting "details" to empty object if it is not
  # specified.
  response := {
    "key": key,
    "details": object.get(r, "details", {}),
    "msg": r.msg,
  }
}

inventory[inv] {
	inv = data.external
}
inventory[{}] {
	not data.external
}
`
)

var hookModule *ast.Module

func init() {
	var err error
	hookModule, err = parseModule(hookModulePath, hookModuleRego)
	if err != nil {
		panic(err)
	}
}
