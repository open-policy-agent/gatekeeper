package regolib

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"text/template"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
)

type testCase struct {
	Template *template.Template
}

func TestRegoExecutes(t *testing.T) {
	tc := []testCase{
		{Template: TargetLib},
	}
	for _, tt := range tc {
		t.Run(tt.Template.Name(), func(t *testing.T) {
			b := &bytes.Buffer{}
			if err := tt.Template.Execute(b, map[string]string{"Target": "foo"}); err != nil {
				t.Fatalf("Could not execute template: %s", tt.Template.Name())
			}
			compiler, err := ast.CompileModules(map[string]string{"foo": b.String()})
			if err != nil {
				t.Fatalf("Could not parse rego for template %s: %s", tt.Template.Name(), err)
			}
			r := rego.New(rego.Query(fmt.Sprintf("data.hooks.foo.%s", strings.ToLower(tt.Template.Name()))), rego.Compiler(compiler))
			if _, err := r.Eval(context.Background()); err != nil {
				t.Fatalf("Could not execute rego for template %s: %s", tt.Template.Name(), err)
			}
		})
	}
}
