package local

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/opa/rego"
)

const (
	addModule     = "addModule"
	deleteModule  = "deleteModule"
	putModules    = "putModules"
	deleteModules = "deleteModules"
	addData       = "addData"
	deleteData    = "deleteData"
)

// testCase is a legacy test case type that performs a single call on the
// driver.
type testCase struct {
	Name          string
	Rules         rules
	Data          []data
	ErrorExpected bool
	ExpectedVals  []string
}

// rule corresponds to a rego snippet from the constraint template or other
type rule struct {
	Path    string
	Content string
}

// rules is a list of rules
type rules []rule

func (r rules) srcs() []string {
	var srcs []string
	for _, rule := range r {
		srcs = append(srcs, rule.Content)
	}
	return srcs
}

type data map[string]interface{}

// compositeTestCase is a testcase that consists of one or more API calls
type compositeTestCase struct {
	Name    string
	Actions []action
}

// action corresponds to a method call for compositeTestCase
type action struct {
	Op              string
	RuleNamePrefix  string // Used in PutModules/DeleteModules
	EvalPath        string // Path to evaluate
	Rules           rules
	Data            []data
	ErrorExpected   bool
	ExpectedBool    bool // Checks against DeleteModule returned bool
	WantDeleteCount int  // Checks against DeleteModules returned count
	ExpectedVals    []string
}

func (tt *compositeTestCase) run(t *testing.T) {
	dr := New()
	d := dr.(*driver)
	for idx, a := range tt.Actions {
		t.Run(fmt.Sprintf("action idx %d", idx), func(t *testing.T) {
			ctx := context.Background()
			switch a.Op {
			case addModule:
				for _, r := range a.Rules {
					err := d.PutModule(ctx, r.Path, r.Content)
					if (err == nil) && a.ErrorExpected {
						t.Fatalf("PUT err = nil; want non-nil")
					}
					if (err != nil) && !a.ErrorExpected {
						t.Fatalf("PUT err = \"%s\"; want nil", err)
					}
				}

			case deleteModule:
				for _, r := range a.Rules {
					b, err := d.DeleteModule(ctx, r.Path)
					if (err == nil) && a.ErrorExpected {
						t.Fatalf("DELETE err = nil; want non-nil")
					}
					if (err != nil) && !a.ErrorExpected {
						t.Fatalf("DELETE err = \"%s\"; want nil", err)
					}
					if b != a.ExpectedBool {
						t.Fatalf("DeleteModule(\"%s\") = %t; want %t", r.Path, b, a.ExpectedBool)
					}
				}

			case putModules:
				err := d.PutModules(ctx, a.RuleNamePrefix, a.Rules.srcs())
				if (err == nil) && a.ErrorExpected {
					t.Fatalf("PutModules err = nil; want non-nil")
				}
				if (err != nil) && !a.ErrorExpected {
					t.Fatalf("PutModules err = \"%s\"; want nil", err)
				}

			case deleteModules:
				count, err := d.DeleteModules(ctx, a.RuleNamePrefix)
				if (err == nil) && a.ErrorExpected {
					t.Fatalf("DeleteModules err = nil; want non-nil")
				}
				if (err != nil) && !a.ErrorExpected {
					t.Fatalf("DeleteModules err = \"%s\"; want nil", err)
				}
				if count != a.WantDeleteCount {
					t.Fatalf("DeleteModules(\"%s\") = %d; want %d", a.RuleNamePrefix, count, a.WantDeleteCount)
				}

			default:
				t.Fatalf("unsupported op: %s", a.Op)
			}

			evalPath := "data.hello.r[a]"
			if a.EvalPath != "" {
				evalPath = a.EvalPath
			}

			res, _, err := d.eval(context.Background(), evalPath, nil, &drivers.QueryCfg{})
			if err != nil {
				t.Errorf("Eval error: %s", err)
			}
			if !resultsEqual(res, a.ExpectedVals, t) {
				fmt.Printf("For Test TestPutModule/%s: modules: %v\n", tt.Name, d.modules)
			}
		})
	}
}

func resultsEqual(res rego.ResultSet, exp []string, t *testing.T) bool {
	ev := []string{}
	for _, r := range res {
		i, ok := r.Bindings["a"].(string)
		if !ok {
			t.Fatalf("Unexpected result format: %v", r.Bindings)
		}
		ev = append(ev, i)
	}
	if len(ev) == 0 && len(exp) == 0 {
		return true
	}
	sort.Strings(ev)
	sort.Strings(exp)
	if !reflect.DeepEqual(ev, exp) {
		t.Errorf("Wanted results %v, got %v", exp, ev)
		return false
	}
	return true
}

func TestModules(t *testing.T) {
	tc := []compositeTestCase{
		{
			Name: "PutModules then DeleteModules",
			Actions: []action{
				{
					Op:             putModules,
					RuleNamePrefix: "test1",
					Rules: rules{
						{Content: `package hello r[a] { data.world.r[a] }`},
						{Content: `package world r[a] { data.foobar.r[a] }`},
						{Content: `package foobar r[a] {a = "m"}`},
					},
					ExpectedVals: []string{"m"},
				},
				// attempt to interfere with modules/module stuff
				{
					Op:            deleteModule,
					Rules:         rules{{Path: "test1"}},
					ErrorExpected: false,
					ExpectedBool:  false,
					ExpectedVals:  []string{"m"},
				},
				{
					Op:              deleteModules,
					RuleNamePrefix:  "test1",
					WantDeleteCount: 3,
				},
			},
		},
		{
			Name: "PutModules with invalid empty string name",
			Actions: []action{
				{
					Op: putModules,
					Rules: rules{
						{Content: `package hello r[a] { data.world.r[a] }`},
						{Content: `package world r[a] {a = "m"}`},
					},
					ErrorExpected: true,
				},
			},
		},
		{
			Name: "PutModules with invalid sequence",
			Actions: []action{
				{
					Op:             putModules,
					RuleNamePrefix: "test1_idx_",
					Rules: rules{
						{Content: `package hello r[a] { data.world.r[a] }`},
						{Content: `package world r[a] {a = "m"}`},
					},
					ErrorExpected: true,
				},
			},
		},
		{
			Name: "PutModule with invalid prefix",
			Actions: []action{
				{
					Op:            addModule,
					Rules:         rules{{"__modset_test1", `package hello r[a] {a = "m"}`}},
					ErrorExpected: true,
				},
			},
		},
		{
			Name: "PutModules twice, decrease src count",
			Actions: []action{
				{
					Op:             putModules,
					RuleNamePrefix: "test1",
					Rules: rules{
						{Content: `package hello r[a] { data.world.r[a] }`},
						{Content: `package world r[a] { data.foobar.r[a] }`},
						{Content: `package foobar r[a] {a = "m"}`},
					},
					ExpectedVals: []string{"m"},
				},
				{
					Op:             putModules,
					RuleNamePrefix: "test1",
					Rules: rules{
						{Content: `package hello r[a] { data.foobar.r[a] }`},
						{Content: `package foobar r[a] {a = "a"}`},
					},
					ExpectedVals: []string{"a"},
				},
			},
		},
		{
			Name: "PutModules twice, increase src count",
			Actions: []action{
				{
					Op:             putModules,
					RuleNamePrefix: "test1",
					Rules: rules{
						{Content: `package hello r[a] { data.foobar.r[a] }`},
						{Content: `package foobar r[a] {a = "a"}`},
					},
					ExpectedVals: []string{"a"},
				},
				{
					Op:             putModules,
					RuleNamePrefix: "test1",
					Rules: rules{
						{Content: `package hello r[a] { data.world.r[a] }`},
						{Content: `package world r[a] { data.foobar.r[a] }`},
						{Content: `package foobar r[a] {a = "m"}`},
					},
					ExpectedVals: []string{"m"},
				},
			},
		},
		{
			Name: "DeleteModules twice",
			Actions: []action{
				{
					Op:             putModules,
					RuleNamePrefix: "test1",
					Rules: rules{
						{Content: `package hello r[a] { data.world.r[a] }`},
						{Content: `package world r[a] { data.foobar.r[a] }`},
						{Content: `package foobar r[a] {a = "m"}`},
					},
					ExpectedVals: []string{"m"},
				},
				{
					Op:              deleteModules,
					RuleNamePrefix:  "test1",
					WantDeleteCount: 3,
				},
				{
					Op:              deleteModules,
					RuleNamePrefix:  "test1",
					WantDeleteCount: 0,
				},
			},
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, tt.run)
	}
}

func TestPutModule(t *testing.T) {
	tc := []testCase{
		{
			Name:          "Put One Rule",
			Rules:         rules{{"test", `package hello r[a] {a = "1"}`}},
			ErrorExpected: false,
			ExpectedVals:  []string{"1"},
		},
		{
			Name:          "Put Duplicate Rules",
			Rules:         rules{{"test", `package hello r[a] {a = "q"}`}, {"test", `package hello r[a] {a = "v"}`}},
			ErrorExpected: false,
			ExpectedVals:  []string{"v"},
		},
		{
			Name:          "Put Multiple Rules",
			Rules:         rules{{"test", `package hello r[a] {a = "b"}`}, {"test2", `package hello r[a] {a = "v"}`}},
			ErrorExpected: false,
			ExpectedVals:  []string{"b", "v"},
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			dr := New()
			d := dr.(*driver)
			for _, r := range tt.Rules {
				err := d.PutModule(context.Background(), r.Path, r.Content)
				if (err == nil) && tt.ErrorExpected {
					t.Fatalf("err = nil; want non-nil")
				}
				if (err != nil) && !tt.ErrorExpected {
					t.Fatalf("err = \"%s\"; want nil", err)
				}
			}
			res, _, err := d.eval(context.Background(), "data.hello.r[a]", nil, &drivers.QueryCfg{})
			if err != nil {
				t.Errorf("Eval error: %s", err)
			}
			if !resultsEqual(res, tt.ExpectedVals, t) {
				fmt.Printf("For Test TestPutModule/%s: modules: %v\n", tt.Name, d.modules)
			}
		})
	}
}

func TestDeleteModule(t *testing.T) {
	tc := []compositeTestCase{
		{
			Name: "Delete One Rule",
			Actions: []action{
				{
					Op:    addModule,
					Rules: rules{{"test1", `package hello r[a] {a = "m"}`}},

					ErrorExpected: false,
					ExpectedVals:  []string{"m"},
				},
				{
					Op:            deleteModule,
					Rules:         rules{{Path: "test1"}},
					ErrorExpected: false,
					ExpectedBool:  true,
				},
			},
		},
		{
			Name: "Delete One Rule Twice",
			Actions: []action{
				{
					Op:            addModule,
					Rules:         rules{{"test1", `package hello r[a] {a = "m"}`}},
					ErrorExpected: false,
					ExpectedVals:  []string{"m"},
				},
				{
					Op:            deleteModule,
					Rules:         rules{{Path: "test1"}},
					ErrorExpected: false,
					ExpectedBool:  true,
				},
				{
					Op:            deleteModule,
					Rules:         rules{{Path: "test1"}},
					ErrorExpected: false,
					ExpectedBool:  false,
				},
			},
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, tt.run)
	}
}

func makeDataPath(s string) string {
	s = strings.Replace(s, "/", ".", -1)
	return "data." + s[1:]
}

func TestPutData(t *testing.T) {
	tc := []testCase{
		{
			Name:          "Put One Datum",
			Data:          []data{{"/key": "my_value"}},
			ErrorExpected: false,
		},
		{
			Name:          "Overwrite Data",
			Data:          []data{{"/key": "my_value"}, {"/key": "new_value"}},
			ErrorExpected: false,
		},
		{
			Name:          "Multiple Data",
			Data:          []data{{"/key": "my_value", "/other_key": "new_value"}},
			ErrorExpected: false,
		},
		{
			Name:          "Add Some Depth",
			Data:          []data{{"/key/is/really/deep": "my_value"}},
			ErrorExpected: false,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			dr := New()
			d := dr.(*driver)
			for _, data := range tt.Data {
				for k, v := range data {
					err := d.PutData(context.Background(), k, v)
					if (err == nil) && tt.ErrorExpected {
						t.Fatalf("err = nil; want non-nil")
					}
					if (err != nil) && !tt.ErrorExpected {
						t.Fatalf("err = \"%s\"; want nil", err)
					}
					res, _, err := d.eval(context.Background(), makeDataPath(k), nil, &drivers.QueryCfg{})
					if err != nil {
						t.Errorf("Eval error: %s", err)
					}
					if len(res) == 0 || len(res[0].Expressions) == 0 {
						t.Fatalf("No results: %v", res)
					}
					if !reflect.DeepEqual(res[0].Expressions[0].Value, v) {
						t.Errorf("%v != %v", v, res[0].Expressions[0].Value)
					}
				}
			}
		})
	}
}

func TestDeleteData(t *testing.T) {
	tc := []compositeTestCase{
		{
			Name: "Delete One Datum",
			Actions: []action{
				{
					Op:            addData,
					Data:          []data{{"/key": "my_value"}},
					ErrorExpected: false,
					ExpectedVals:  []string{"m"},
				},
				{
					Op:            deleteData,
					Data:          []data{{"/key": "my_value"}},
					ErrorExpected: false,
					ExpectedBool:  true,
				},
			},
		},
		{
			Name: "Delete Data Twice",
			Actions: []action{
				{
					Op:            addData,
					Data:          []data{{"/key": "my_value"}},
					ErrorExpected: false,
					ExpectedVals:  []string{"m"},
				},
				{
					Op:            deleteData,
					Data:          []data{{"/key": "my_value"}},
					ErrorExpected: false,
					ExpectedBool:  true,
				},
				{
					Op:            deleteData,
					Data:          []data{{"/key": "my_value"}},
					ErrorExpected: false,
					ExpectedBool:  false,
				},
			},
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			dr := New()
			d := dr.(*driver)
			for _, a := range tt.Actions {
				for _, data := range a.Data {
					for k, v := range data {
						switch a.Op {
						case addData:
							err := d.PutData(context.Background(), k, v)
							if (err == nil) && a.ErrorExpected {
								t.Fatalf("PUT err = nil; want non-nil")
							}
							if (err != nil) && !a.ErrorExpected {
								t.Fatalf("PUT err = \"%s\"; want nil", err)
							}
							res, _, err := d.eval(context.Background(), makeDataPath(k), nil, &drivers.QueryCfg{})
							if err != nil {
								t.Errorf("Eval error: %s", err)
							}
							if len(res) == 0 || len(res[0].Expressions) == 0 {
								t.Fatalf("No results: %v", res)
							}
							if !reflect.DeepEqual(res[0].Expressions[0].Value, v) {
								t.Errorf("%v != %v", v, res[0].Expressions[0].Value)
							}
						case deleteData:
							b, err := d.DeleteData(context.Background(), k)
							if (err == nil) && a.ErrorExpected {
								t.Fatalf("DELETE err = nil; want non-nil")
							}
							if (err != nil) && !a.ErrorExpected {
								t.Fatalf("DELETE err = \"%s\"; want nil", err)
							}
							if b != a.ExpectedBool {
								t.Fatalf("DeleteModule(\"%s\") = %t; want %t", k, b, a.ExpectedBool)
							}
							res, _, err := d.eval(context.Background(), makeDataPath(k), nil, &drivers.QueryCfg{})
							if err != nil {
								t.Errorf("Eval error: %s", err)
							}
							if len(res) != 0 {
								t.Fatalf("Got results after delete: %v", res)
							}
						default:
							t.Fatalf("unsupported op: %s", a.Op)
						}
					}
				}
			}
		})
	}
}

func TestQuery(t *testing.T) {
	rawResponses := `
[
	{
		"msg": "totally invalid",
		"metadata": {"details": {"not": "good"}},
		"constraint": {
			"apiVersion": "constraints.gatekeeper.sh/v1",
			"kind": "RequiredLabels",
			"metadata": {
				"name": "require-a-label"
			},
			"spec": {
				"parameters": {"hello": "world"}
			}
		},
		"resource": {"hi": "there"}
	},
	{
		"msg": "yep"
	}
]
`
	var intResponses []interface{}
	if err := json.Unmarshal([]byte(rawResponses), &intResponses); err != nil {
		t.Fatalf("Could not parse JSON: %s", err)
	}

	var responses []*types.Result
	if err := json.Unmarshal([]byte(rawResponses), &responses); err != nil {
		t.Fatalf("Could not parse JSON: %s", err)
	}

	t.Run("Parse Response", func(t *testing.T) {
		d := New()

		for i, v := range intResponses {
			if err := d.PutData(context.Background(), fmt.Sprintf("/constraints/%d", i), v); err != nil {
				t.Fatal(err)
			}
		}

		if err := d.PutModule(context.Background(), "test", `package hooks violation[r] { r = data.constraints[_] }`); err != nil {
			t.Fatal(err)
		}
		res, err := d.Query(context.Background(), "hooks.violation", nil)
		if err != nil {
			t.Fatal(err)
		}
		sort.SliceStable(res.Results, func(i, j int) bool {
			return res.Results[i].Msg < res.Results[j].Msg
		})
		sort.SliceStable(responses, func(i, j int) bool {
			return responses[i].Msg < responses[j].Msg
		})
		if !reflect.DeepEqual(res.Results, responses) {
			t.Errorf("%s != %s", spew.Sprint(res), spew.Sprint(responses))
		}

	})
}
