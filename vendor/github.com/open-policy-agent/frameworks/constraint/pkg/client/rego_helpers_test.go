package client

import (
	"testing"

	"github.com/open-policy-agent/opa/ast"
)

type regoTestCase struct {
	Name          string
	Rego          string
	Path          string
	ErrorExpected bool
	ExpectedRego  string
	ArityExpected int
	RequiredRules ruleArities
}

func TestGetRuleArity(t *testing.T) {
	tc := []regoTestCase{
		{
			Name:          "Nullary",
			Rego:          `package hello v{1 == 1}`,
			ArityExpected: 0,
		},
		{
			Name:          "Unary",
			Rego:          `package hello v[r]{r == 1}`,
			ArityExpected: 1,
		},
		{
			Name:          "Object is unary",
			Rego:          `package hello v[{"arg": a}]{a == 1}`,
			ArityExpected: 1,
		},
		{
			Name:          "Binary",
			Rego:          `package hello v[[r, d]]{r == 1; d == 2}`,
			ArityExpected: 2,
		},
		{
			Name:          "5-ary",
			Rego:          `package hello v[[a, b, c, d, e]]{a == 1; b == 2; c == 3; d == 4; e == 5 }`,
			ArityExpected: 5,
		},
		{
			Name:          "Object in Array Allowed",
			Rego:          `package hello v[[{"arg": a}, b]]{a == 1; b == 2}`,
			ArityExpected: 2,
		},
		{
			Name:          "No String Key",
			Rego:          `package hello v["q"]{1 == 1}`,
			ErrorExpected: true,
		},
		{
			Name:          "No String Array Entry",
			Rego:          `package hello v[[b, "a"]]{b == 2}`,
			ErrorExpected: true,
		},
		{
			Name:          "No String Array Entry (reversed)",
			Rego:          `package hello v[["a", b]]{b == 2}`,
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			module, err := ast.ParseModule("foo", tt.Rego)
			if err != nil {
				t.Fatalf("Error parsing rego: %s", err)
			}
			arity, err := getRuleArity(module.Rules[0])
			if (err == nil) && tt.ErrorExpected {
				t.Fatalf("err = nil; want non-nil")
			}
			if (err != nil) && !tt.ErrorExpected {
				t.Fatalf("err = \"%s\"; want nil", err)
			}
			if arity != tt.ArityExpected {
				t.Errorf("getRuleArity(%s) = %d, want %d", tt.Rego, arity, tt.ArityExpected)
			}
		})
	}
}

func TestRequireRules(t *testing.T) {
	tc := []regoTestCase{
		{
			Name:          "No Required Rules",
			Rego:          `package hello`,
			ErrorExpected: false,
		},
		{
			Name:          "Bad Rego",
			Rego:          `package hello {dangling bracket`,
			ErrorExpected: true,
		},
		{
			Name:          "Required Rule",
			Rego:          `package hello r{1 == 1}`,
			RequiredRules: ruleArities{"r": 0},
			ErrorExpected: false,
		},
		{
			Name:          "Required Rule Unary",
			Rego:          `package hello r[v]{v == 1}`,
			RequiredRules: ruleArities{"r": 1},
			ErrorExpected: false,
		},
		{
			Name:          "Required Rule Binary",
			Rego:          `package hello r[[v, q]]{v == 1; q == 2}`,
			RequiredRules: ruleArities{"r": 2},
			ErrorExpected: false,
		},
		{
			Name:          "Required Rule Extras",
			Rego:          `package hello r[v]{v == 1} q{3 == 3}`,
			RequiredRules: ruleArities{"r": 1},
			ErrorExpected: false,
		},
		{
			Name:          "Required Rule Multiple",
			Rego:          `package hello r[v]{v == 1} q{3 == 3}`,
			RequiredRules: ruleArities{"r": 1, "q": 0},
			ErrorExpected: false,
		},
		{
			Name:          "Required Rule Missing",
			Rego:          `package hello`,
			RequiredRules: ruleArities{"r": 0},
			ErrorExpected: true,
		},
		{
			Name:          "Required Rule Wrong Arity",
			Rego:          `package hello r{1 == 1}`,
			RequiredRules: ruleArities{"r": 1},
			ErrorExpected: true,
		},
		{
			Name:          "Required Rule Missing, Multiple",
			Rego:          `package hello r{1 == 1}`,
			RequiredRules: ruleArities{"r": 0, "q": 0},
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			mod, err := parseModule("foo", tt.Rego)
			if err == nil {
				err = requireRulesModule(mod, tt.RequiredRules)
			}

			if (err == nil) && tt.ErrorExpected {
				t.Fatalf("err = nil; want non-nil")
			}
			if (err != nil) && !tt.ErrorExpected {
				t.Fatalf("err = \"%s\"; want nil", err)
			}
		})
	}
}
