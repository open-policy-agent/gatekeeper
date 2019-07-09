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

func runRegoTests(tt []regoTestCase, t *testing.T) {
	for _, tc := range tt {
		t.Run(tc.Name, func(t *testing.T) {
			path := tc.Path
			if path == "" {
				path = "def.test.path"
			}
			rego, err := ensureRegoConformance("test", path, tc.Rego)
			if (err == nil) && tc.ErrorExpected {
				t.Errorf("err = nil; want non-nil")
			}
			if (err != nil) && !tc.ErrorExpected {
				t.Errorf("err = \"%s\"; want nil", err)
			}
			if tc.ExpectedRego != "" && rego != tc.ExpectedRego {
				t.Errorf("ensureRegoConformance(%s) = %s; want %s", tc.Rego, rego, tc.ExpectedRego)
			}
		})
	}
}

func TestDataAccess(t *testing.T) {
	runRegoTests([]regoTestCase{
		{
			Name:          "Empty String Fails",
			Rego:          "",
			ErrorExpected: true,
		},
		{
			Name:          "No Data Access",
			Rego:          "package hello v{1 == 1}",
			ErrorExpected: false,
		},
		{
			Name:          "Valid Data Access: Inventory",
			Rego:          "package hello v{data.inventory == 1}",
			ErrorExpected: false,
		},
		{
			Name:          "Valid Data Access Field",
			Rego:          `package hello v{data["inventory"] == 1}`,
			ErrorExpected: false,
		},
		{
			Name:          "Valid Data Access Field Variable Assignment",
			Rego:          `package hello v{q := data["inventory"]; q.res == 7}`,
			ErrorExpected: false,
		},
		{
			Name:          "Invalid Data Access",
			Rego:          "package hello v{data.tribble == 1}",
			ErrorExpected: true,
		},
		{
			Name:          "Invalid Data Access Param",
			Rego:          `package hello v[{"here": data.onering}]{1 == 1}`,
			ErrorExpected: true,
		},
		{
			Name:          "Invalid Data Access No Param",
			Rego:          `package hello v{data == 1}`,
			ErrorExpected: true,
		},
		{
			Name:          "Invalid Data Access Variable",
			Rego:          `package hello v{q := "inventory"; data[q] == 1}`,
			ErrorExpected: true,
		},
		{
			Name:          "Invalid Data Access Variable Assignment",
			Rego:          `package hello v{q := data; q.nonono == 1}`,
			ErrorExpected: true,
		},
		{
			Name:          "Invalid Data Access Blank Iterator",
			Rego:          `package hello v{data[_] == 1}`,
			ErrorExpected: true,
		},
		{
			Name:          "Invalid Data Access Object",
			Rego:          `package hello v{data[{"my": _}] == 1}`,
			ErrorExpected: true,
		},
	}, t)
}

func TestNoImportsAllowed(t *testing.T) {
	runRegoTests([]regoTestCase{
		{
			Name:          "No Imports",
			Rego:          "package hello v{1 == 1}",
			ErrorExpected: false,
		},
		{
			Name:          "One Import",
			Rego:          "package hello import data.foo v{1 == 1}",
			ErrorExpected: true,
		},
		{
			Name:          "Three Imports",
			Rego:          "package hello import data.foo import data.test import data.things v{1 == 1}",
			ErrorExpected: true,
		},
	}, t)
}

func TestPackageChange(t *testing.T) {
	runRegoTests([]regoTestCase{
		{
			Name:          "Package Modified",
			Path:          "some.path",
			Rego:          "package hello v{1 == 1}",
			ErrorExpected: false,
			ExpectedRego: `package some.path

v = true { equal(1, 1) }`,
		},
		{
			Name:          "Package Modified Other Path",
			Path:          "different.path",
			Rego:          "package hello v{1 == 1}",
			ErrorExpected: false,
			ExpectedRego: `package different.path

v = true { equal(1, 1) }`,
		},
	}, t)
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
			err := requireRules("foo", tt.Rego, tt.RequiredRules)
			if (err == nil) && tt.ErrorExpected {
				t.Fatalf("err = nil; want non-nil")
			}
			if (err != nil) && !tt.ErrorExpected {
				t.Fatalf("err = \"%s\"; want nil", err)
			}
		})
	}
}
