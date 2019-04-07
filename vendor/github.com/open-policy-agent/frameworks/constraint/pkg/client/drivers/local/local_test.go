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
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/opa/rego"
)

const (
	add    = "add"
	remove = "remove"
)

type testCase struct {
	Name             string
	Rules            rules
	Data             []data
	ErrorExpected    bool
	ExpectedVals     []string
	ExpectedResponse []*types.Result
}

type rule []string
type rules []rule

type data map[string]interface{}

type deleteTestCase struct {
	Name    string
	Actions []action
}

type action struct {
	Op            string
	Data          testCase
	ErrorExpected bool
	ExpectedBool  bool
	ExpectedVals  []string
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
				err := d.PutModule(context.Background(), r[0], r[1])
				if (err == nil) && tt.ErrorExpected {
					t.Fatalf("err = nil; want non-nil")
				}
				if (err != nil) && !tt.ErrorExpected {
					t.Fatalf("err = \"%s\"; want nil", err)
				}
			}
			res, _, err := d.eval(context.Background(), "data.hello.r[a]", nil)
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
	tc := []deleteTestCase{
		{
			Name: "Delete One Rule",
			Actions: []action{
				{
					Op: add,
					Data: testCase{
						Rules: rules{{"test1", `package hello r[a] {a = "m"}`}},
					},
					ErrorExpected: false,
					ExpectedVals:  []string{"m"},
				},
				{
					Op: remove,
					Data: testCase{
						Rules: rules{{"test1"}},
					},
					ErrorExpected: false,
					ExpectedBool:  true,
				},
			},
		},
		{
			Name: "Delete One Rule Twice",
			Actions: []action{
				{
					Op: add,
					Data: testCase{
						Rules: rules{{"test1", `package hello r[a] {a = "m"}`}},
					},
					ErrorExpected: false,
					ExpectedVals:  []string{"m"},
				},
				{
					Op: remove,
					Data: testCase{
						Rules: rules{{"test1"}},
					},
					ErrorExpected: false,
					ExpectedBool:  true,
				},
				{
					Op: remove,
					Data: testCase{
						Rules: rules{{"test1"}},
					},
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
				if a.Op == add {
					for _, r := range a.Data.Rules {
						err := d.PutModule(context.Background(), r[0], r[1])
						if (err == nil) && a.ErrorExpected {
							t.Fatalf("PUT err = nil; want non-nil")
						}
						if (err != nil) && !a.ErrorExpected {
							t.Fatalf("PUT err = \"%s\"; want nil", err)
						}
					}
					// remove
				} else {
					for _, r := range a.Data.Rules {
						b, err := d.DeleteModule(context.Background(), r[0])
						if (err == nil) && a.ErrorExpected {
							t.Fatalf("DELETE err = nil; want non-nil")
						}
						if (err != nil) && !a.ErrorExpected {
							t.Fatalf("DELETE err = \"%s\"; want nil", err)
						}
						if b != a.ExpectedBool {
							t.Fatalf("DeleteModule(\"%s\") = %t; want %t", r[0], b, a.ExpectedBool)
						}
					}
				}
				res, _, err := d.eval(context.Background(), "data.hello.r[a]", nil)
				if err != nil {
					t.Errorf("Eval error: %s", err)
				}
				if !resultsEqual(res, a.ExpectedVals, t) {
					fmt.Printf("For Test TestPutModule/%s: modules: %v\n", tt.Name, d.modules)
				}
			}
		})
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
					res, _, err := d.eval(context.Background(), makeDataPath(k), nil)
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
	tc := []deleteTestCase{
		{
			Name: "Delete One Datum",
			Actions: []action{
				{
					Op: add,
					Data: testCase{
						Data: []data{{"/key": "my_value"}},
					},
					ErrorExpected: false,
					ExpectedVals:  []string{"m"},
				},
				{
					Op: remove,
					Data: testCase{
						Data: []data{{"/key": "my_value"}},
					},
					ErrorExpected: false,
					ExpectedBool:  true,
				},
			},
		},
		{
			Name: "Delete Data Twice",
			Actions: []action{
				{
					Op: add,
					Data: testCase{
						Data: []data{{"/key": "my_value"}},
					},
					ErrorExpected: false,
					ExpectedVals:  []string{"m"},
				},
				{
					Op: remove,
					Data: testCase{
						Data: []data{{"/key": "my_value"}},
					},
					ErrorExpected: false,
					ExpectedBool:  true,
				},
				{
					Op: remove,
					Data: testCase{
						Data: []data{{"/key": "my_value"}},
					},
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
				for _, data := range a.Data.Data {
					for k, v := range data {
						if a.Op == add {
							err := d.PutData(context.Background(), k, v)
							if (err == nil) && a.ErrorExpected {
								t.Fatalf("PUT err = nil; want non-nil")
							}
							if (err != nil) && !a.ErrorExpected {
								t.Fatalf("PUT err = \"%s\"; want nil", err)
							}
							res, _, err := d.eval(context.Background(), makeDataPath(k), nil)
							if err != nil {
								t.Errorf("Eval error: %s", err)
							}
							if len(res) == 0 || len(res[0].Expressions) == 0 {
								t.Fatalf("No results: %v", res)
							}
							if !reflect.DeepEqual(res[0].Expressions[0].Value, v) {
								t.Errorf("%v != %v", v, res[0].Expressions[0].Value)
							}
							// remove
						} else {
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
							res, _, err := d.eval(context.Background(), makeDataPath(k), nil)
							if err != nil {
								t.Errorf("Eval error: %s", err)
							}
							if len(res) != 0 {
								t.Fatalf("Got results after delete: %v", res)
							}
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
	intResponses := []interface{}{}
	if err := json.Unmarshal([]byte(rawResponses), &intResponses); err != nil {
		t.Fatalf("Could not parse JSON: %s", err)
	}

	responses := []*types.Result{}
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

		if err := d.PutModule(context.Background(), "test", `package hooks deny[r] { r = data.constraints[_] }`); err != nil {
			t.Fatal(err)
		}
		res, err := d.Query(context.Background(), "hooks.deny", nil)
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
