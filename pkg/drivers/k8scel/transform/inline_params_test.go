package transform

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	celgo "github.com/google/cel-go/cel"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/webhookconfig/webhookconfigcache"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/policy/validating"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
	"k8s.io/apiserver/pkg/cel/environment"
	"k8s.io/utils/ptr"
)

func TestConstraintParametersExpressionDynamicNull(t *testing.T) {
	for _, object := range []map[string]interface{}{
		{}, {"spec": map[string]interface{}{}}, {"spec": map[string]interface{}{"parameters": nil}},
	} {
		expression, err := constraintParametersExpression(&unstructured.Unstructured{Object: object})
		if err != nil {
			t.Fatal(err)
		}
		if expression != "dyn(null)" {
			t.Fatalf("null params expression = %q", expression)
		}
		env, err := celgo.NewEnv()
		if err != nil {
			t.Fatal(err)
		}
		_, issues := env.Compile("(" + expression + ") != null && has((" + expression + ").exemptImages)")
		if issues.Err() != nil {
			t.Fatal(issues.Err())
		}
	}
}

func TestCompactParametersExpression(t *testing.T) {
	values := []interface{}{
		map[string]interface{}{},
		map[string]interface{}{"one": "value"},
		map[string]interface{}{"labels": []interface{}{map[string]interface{}{"key": "owner", "allowedRegex": "^bench$"}}},
		map[string]interface{}{"one": int64(1), "two": "text", "three": nil, "four": true},
		map[string]interface{}{"items": []interface{}{int64(1), "two", nil, true, []interface{}{}, map[string]interface{}{}}},
		map[string]interface{}{"items": []interface{}{map[string]interface{}{"count": int64(2), "name": "x"}, map[string]interface{}{"count": "two", "name": nil}}},
		map[string]interface{}{"items": []interface{}{[]interface{}{int64(1)}, []interface{}{"one"}, []interface{}{}}},
		map[string]interface{}{"items": []interface{}{map[string]interface{}{"count": int64(2)}, map[string]interface{}{"count": "two"}}},
		map[string]interface{}{"signed": int64(math.MaxInt64), "unsigned": uint64(math.MaxUint64), "double": 1.0, "float": float32(1.25), "jsonInt": json.Number("9007199254740993"), "jsonDouble": json.Number("1.0")},
		map[string]interface{}{"foo-bar": []interface{}{"\u30c6\u30b9\u30c8", "quote\"\\"}, "namespace": map[string]interface{}{"value": true}},
	}
	env, err := celgo.NewEnv(celgo.HomogeneousAggregateLiterals())
	if err != nil {
		t.Fatal(err)
	}
	for index, value := range values {
		t.Run(fmt.Sprintf("case-%d", index), func(t *testing.T) {
			original, err := jsonValueToCEL(value)
			if err != nil {
				t.Fatal(err)
			}
			compact, err := compactParametersExpression(value)
			if err != nil {
				t.Fatal(err)
			}
			parsed, issues := env.Compile("(" + original + ") == (" + compact + ")")
			if issues.Err() != nil {
				t.Fatal(issues.Err())
			}
			program, err := env.Program(parsed)
			if err != nil {
				t.Fatal(err)
			}
			result, _, err := program.Eval(map[string]interface{}{})
			if err != nil || result.Value() != true {
				t.Fatalf("encoded values differ: %v (%v)", result, err)
			}
			if len(compact) > len(original) {
				t.Fatal("compaction increased expression size")
			}
			if compact != original {
				parsed, issues := env.Compile(compact)
				if issues.Err() != nil || !parsed.OutputType().IsExactType(celgo.DynType) {
					t.Fatalf("compacted root must preserve dynamic typing: %v", issues.Err())
				}
			}
		})
	}
	for _, value := range []interface{}{math.Inf(1), math.NaN(), make(chan int)} {
		if _, err := compactParametersExpression(value); err == nil {
			t.Fatalf("accepted unsupported parameter %T", value)
		}
	}
}

func TestJSONValueToCEL(t *testing.T) {
	value := map[string]interface{}{
		"nullable": nil,
		"message":  "required\nlabel",
		"labels": []interface{}{
			map[string]interface{}{"key": "owner"},
		},
		"enabled": true,
		"count":   int64(2),
		"ratio":   1.0,
	}

	expression, err := jsonValueToCEL(value)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"count": dyn(2), "enabled": dyn(true), "labels": dyn([dyn({"key": dyn("owner")})]), "message": dyn("required\nlabel"), "nullable": dyn(null), "ratio": dyn(1.0)}`
	if expression != want {
		t.Fatalf("jsonValueToCEL() = %s, want %s", expression, want)
	}

	env, err := celgo.NewEnv(celgo.HomogeneousAggregateLiterals())
	if err != nil {
		t.Fatal(err)
	}
	_, issues := env.Compile(expression)
	if issues.Err() != nil {
		t.Fatalf("generated expression does not compile: %v", issues.Err())
	}
}

func TestInlineParametersPreserveMapKeys(t *testing.T) {
	value := map[string]interface{}{
		"foo-bar":         "dash",
		"foo__dash__bar":  "literal escape",
		"namespace":       "reserved",
		"123":             "numeric",
		"example.com/key": "label",
		"":                "empty",
		"nested":          map[string]interface{}{"foo-bar": true},
	}
	env, err := celgo.NewEnv(celgo.HomogeneousAggregateLiterals(), celgo.Variable("original", celgo.DynType))
	if err != nil {
		t.Fatal(err)
	}
	for name, encode := range map[string]func(interface{}) (string, error){
		"original": jsonValueToCEL,
		"compact":  compactParametersExpression,
	} {
		t.Run(name, func(t *testing.T) {
			expression, err := encode(value)
			if err != nil {
				t.Fatal(err)
			}
			parsed, issues := env.Compile("(" + expression + ") == original")
			if issues.Err() != nil {
				t.Fatal(issues.Err())
			}
			program, err := env.Program(parsed)
			if err != nil {
				t.Fatal(err)
			}
			result, _, err := program.Eval(map[string]interface{}{"original": value})
			if err != nil || result.Value() != true {
				t.Fatalf("encoding changed map data: %v (%v): %s", result, err, expression)
			}
		})
	}
}

func TestJSONValueToCELRejectsUnsupportedValues(t *testing.T) {
	_, err := jsonValueToCEL(make(chan int))
	if err == nil || !strings.Contains(err.Error(), "unsupported JSON value type") {
		t.Fatalf("jsonValueToCEL() error = %v, want unsupported type", err)
	}
}

func TestReplaceIdentifierPreservesMacrosAndStrings(t *testing.T) {
	expression := `params.spec.match.kinds.exists(groupKinds, "params" != "" && groupKinds.kinds.exists(kind, kind == request.kind.kind))`
	replacement := `{"spec": dyn({"match": dyn({"kinds": dyn([dyn({"kinds": dyn([dyn("Pod")])})])})})}`

	rewritten, err := replaceIdentifier(expression, "params", replacement)
	if err != nil {
		t.Fatal(err)
	}
	referencesParams, err := expressionReferencesIdentifier(rewritten, "params")
	if err != nil {
		t.Fatal(err)
	}
	if referencesParams {
		t.Fatalf("rewritten expression still references top-level params: %s", rewritten)
	}
	if !strings.Contains(rewritten, `"params"`) {
		t.Fatalf("rewritten expression changed string literal: %s", rewritten)
	}

	env, err := celgo.NewEnv(
		celgo.HomogeneousAggregateLiterals(),
		celgo.Variable("request", celgo.DynType),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, issues := env.Compile(rewritten)
	if issues.Err() != nil {
		t.Fatalf("rewritten expression does not compile: %v\n%s", issues.Err(), rewritten)
	}
}

func TestInlineIdentifierScopes(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		want       string
	}{
		{name: "local", expression: `[{"enabled": true}].exists(params, has(params.enabled))`},
		{name: "nested", expression: `[1].all(params, [params].all(item, item == params))`},
		{name: "map iterator", expression: `{"key": true}.all(params, params == "key")`},
		{name: "second iterator", expression: `{"key": true}.all(key, params, params)`},
		{name: "iteration range", expression: `params.values.all(params, params == "x")`, want: `(inlined).values.all(params, params == "x")`},
		{name: "outside local scope", expression: `[1].all(params, params > 0) && params.enabled`, want: `[1].all(params, params > 0) && (inlined).enabled`},
		{name: "qualified", expression: `.params.enabled`, want: `(inlined).enabled`},
		{name: "qualified whitespace", expression: `. params.enabled`, want: `(inlined).enabled`},
		{name: "qualified newline", expression: ".\nparams.enabled", want: `(inlined).enabled`},
		{name: "qualified comment", expression: ". // another . in a comment\n params.enabled", want: `(inlined).enabled`},
		{name: "qualified inside local scope", expression: `[true].all(params, .params.enabled)`, want: `[true].all(params, (inlined).enabled)`},
		{name: "optional object", expression: `object.metadata.?labels.hasValue()`},
		{name: "optional parameter", expression: `params.?enabled.orValue(false)`, want: `(inlined).?enabled.orValue(false)`},
		{name: "unicode prefix", expression: "\"\u30c6\u30b9\u30c8\" != \"\" && params.enabled", want: "\"\u30c6\u30b9\u30c8\" != \"\" && (inlined).enabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			found, err := expressionReferencesIdentifier(test.expression, "params")
			if err != nil {
				t.Fatal(err)
			}
			if found != (test.want != "") {
				t.Fatalf("free params reference = %v, expression = %s", found, test.expression)
			}
			got, err := replaceIdentifier(test.expression, "params", "inlined")
			if err != nil {
				t.Fatal(err)
			}
			want := test.want
			if want == "" {
				want = test.expression
			}
			if got != want {
				t.Fatalf("rewritten = %s, want %s", got, want)
			}
		})
	}
}

func TestConstraintToPolicyDefinitionNativeParity(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		parameters map[string]interface{}
		want       bool
	}{
		{name: "map key denial", expression: `!("foo-bar" in variables.params.rules)`, parameters: map[string]interface{}{"rules": map[string]interface{}{"foo-bar": true}}},
		{name: "distinct escaped-looking key", expression: `variables.params.rules["foo-bar"] && !variables.params.rules["foo__dash__bar"]`, parameters: map[string]interface{}{"rules": map[string]interface{}{"foo-bar": true, "foo__dash__bar": false}}, want: true},
		{name: "optional labels", expression: `object.metadata.?labels.hasValue()`, want: true},
		{name: "optional absent field", expression: `object.metadata.?annotations.hasValue()`},
		{name: "local variable", expression: `["x"].all(params, params == "x")`, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			template := newInlineTestTemplate(&schema.Source{FailurePolicy: ptr.To("Fail"), Validations: []schema.Validation{{Expression: test.expression}}})
			constraint := newTestConstraint("deny", nil, nil, &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"parameters": test.parameters,
					"match":      map[string]interface{}{"labelSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"123": "x"}}},
				},
			}})
			legacy, err := TemplateToPolicyDefinition(template)
			if err != nil {
				t.Fatal(err)
			}
			inlined, err := ConstraintToPolicyDefinitionWithWebhookConfig(template, constraint, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, policy := range []*admissionregistrationv1beta1.ValidatingAdmissionPolicy{legacy, inlined} {
				got := evaluateInlinePolicyExpression(t, policy, policy.Spec.Validations[0].Expression, constraint)
				if got != test.want {
					t.Fatalf("policy %s result = %v, want %v", policy.Name, got, test.want)
				}
			}
		})
	}
}

func TestConstraintToPolicyDefinitionAuditAnnotations(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, includeSuccess := range []bool{false, true} {
			t.Run(fmt.Sprintf("enabled=%t/success=%t", enabled, includeSuccess), func(t *testing.T) {
				for name, value := range map[string]bool{
					util.EmitAdmissionAuditAnnotationsFlag:           enabled,
					util.AdmissionAuditAnnotationsIncludeSuccessFlag: includeSuccess,
				} {
					previous := flag.Lookup(name).Value.String()
					if err := flag.Set(name, fmt.Sprint(value)); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if err := flag.Set(name, previous); err != nil {
							t.Error(err)
						}
					})
				}
				template := newInlineTestTemplate(&schema.Source{FailurePolicy: ptr.To("Fail"), Validations: []schema.Validation{{Expression: "true"}}})
				constraint := newTestConstraint(string(util.Deny), nil, nil, &unstructured.Unstructured{})
				policy, err := ConstraintToPolicyDefinitionWithWebhookConfig(template, constraint, nil, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if policy.Spec.ParamKind != nil {
					t.Fatal("inlined policy must not depend on a parameter kind")
				}
				if enabled && includeSuccess {
					if len(policy.Spec.AuditAnnotations) != 1 || policy.Spec.AuditAnnotations[0].Key != vapEvaluationAuditAnnotationKey {
						t.Fatalf("audit annotations = %#v, want evaluation marker", policy.Spec.AuditAnnotations)
					}
					expression := "(" + policy.Spec.AuditAnnotations[0].ValueExpression + ") == 'true'"
					if !evaluateInlinePolicyExpression(t, policy, expression, constraint) {
						t.Fatal("inlined evaluation marker must evaluate without top-level params")
					}
				} else if len(policy.Spec.AuditAnnotations) != 0 {
					t.Fatalf("unexpected audit annotations: %#v", policy.Spec.AuditAnnotations)
				}
				binding, err := ConstraintToInlinedBinding(constraint, []string{string(util.Deny)})
				if err != nil {
					t.Fatal(err)
				}
				wantActions := []admissionregistrationv1beta1.ValidationAction{admissionregistrationv1beta1.Deny}
				if enabled {
					wantActions = append(wantActions, admissionregistrationv1beta1.Audit)
				}
				if binding.Spec.ParamRef != nil || !reflect.DeepEqual(binding.Spec.ValidationActions, wantActions) {
					t.Fatalf("inlined binding = %#v, want no ParamRef and actions %v", binding.Spec, wantActions)
				}
			})
		}
	}
}

func TestConstraintToPolicyDefinitionInheritedConditionScopes(t *testing.T) {
	expression := `[{"enabled": true}].exists(params, has(params.enabled))`
	template := newInlineTestTemplate(&schema.Source{FailurePolicy: ptr.To("Fail"), Validations: []schema.Validation{{Expression: "false"}}})
	constraint := newTestConstraint("deny", nil, nil, &unstructured.Unstructured{})
	webhook := &webhookconfigcache.WebhookMatchingConfig{
		Rules: []admissionregistrationv1.RuleWithOperations{{
			Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
			Rule:       admissionregistrationv1.Rule{APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}},
		}},
		MatchConditions: []admissionregistrationv1.MatchCondition{{Name: "local-binding", Expression: expression}},
	}
	legacy, err := TemplateToPolicyDefinitionWithWebhookConfig(template, webhook, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	inlined, err := ConstraintToPolicyDefinitionWithWebhookConfig(template, constraint, webhook, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range []*admissionregistrationv1beta1.ValidatingAdmissionPolicy{legacy, inlined} {
		found := false
		for _, condition := range policy.Spec.MatchConditions {
			if condition.Name != "local-binding" {
				continue
			}
			found = true
			if condition.Expression != expression || !evaluateInlinePolicyExpression(t, policy, condition.Expression, constraint) {
				t.Fatalf("policy %s changed local binding: %s", policy.Name, condition.Expression)
			}
		}
		if !found || evaluateInlinePolicyExpression(t, policy, policy.Spec.Validations[0].Expression, constraint) {
			t.Fatalf("policy %s must retain its match condition and false validation", policy.Name)
		}
	}
}

func evaluateInlinePolicyExpression(t *testing.T, policy *admissionregistrationv1beta1.ValidatingAdmissionPolicy, expression string, constraint *unstructured.Unstructured) bool {
	t.Helper()
	compiler, err := cel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
	if err != nil {
		t.Fatal(err)
	}
	declarations := cel.OptionalVariableDeclarations{HasParams: policy.Spec.ParamKind != nil}
	variables := make([]cel.NamedExpressionAccessor, 0, len(policy.Spec.Variables))
	for _, variable := range policy.Spec.Variables {
		variables = append(variables, &validating.Variable{Name: variable.Name, Expression: variable.Expression})
	}
	compiler.CompileAndStoreVariables(variables, declarations, environment.StoredExpressions)
	evaluator := compiler.CompileCondition([]cel.ExpressionAccessor{&matchconditions.MatchCondition{Name: "parity", Expression: expression}}, declarations, environment.StoredExpressions)
	if errs := evaluator.CompilationErrors(); len(errs) != 0 {
		t.Fatalf("native compilation: %v", errs)
	}
	attributes, err := RequestToVersionedAttributes(&admissionv1.AdmissionRequest{
		Kind:   metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
		Object: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"parity","labels":{"123":"x"}}}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var parameters runtime.Object
	if declarations.HasParams {
		parameters = constraint
	}
	matcher := matchconditions.NewMatcher(evaluator, ptr.To(admissionregistrationv1.Fail), "test", "native", "parity")
	result := matcher.Match(context.Background(), attributes, parameters, nil)
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	return result.Matches
}

func TestConstraintToPolicyDefinitionWithWebhookConfig(t *testing.T) {
	template := newInlineTestTemplate(&schema.Source{
		FailurePolicy: ptr.To("Fail"),
		MatchConditions: []schema.MatchCondition{
			{Name: "always", Expression: "true"},
		},
		Variables: []schema.Variable{
			{Name: "requiredLabels", Expression: "variables.params.labels"},
		},
		Validations: []schema.Validation{
			{Expression: "variables.requiredLabels.size() > 0", MessageExpression: `"required: " + variables.params.message`},
		},
	})
	constraint := newTestConstraint("deny", nil, nil, &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"match": map[string]interface{}{
				"kinds": []interface{}{map[string]interface{}{
					"apiGroups": []interface{}{strings.Clone("")},
					"kinds":     []interface{}{"ConfigMap"},
				}},
			},
			"parameters": map[string]interface{}{
				"message": "owner is required",
				"labels":  []interface{}{map[string]interface{}{"key": "owner"}},
			},
		},
	}})

	policy, err := ConstraintToPolicyDefinitionWithWebhookConfig(template, constraint, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Name != "gatekeeper-footemplate-foo-name-vap" {
		t.Fatalf("policy name = %q", policy.Name)
	}
	if policy.Spec.ParamKind != nil {
		t.Fatalf("ParamKind = %#v, want nil", policy.Spec.ParamKind)
	}
	if len(policy.Spec.Variables) != 3 {
		t.Fatalf("variables = %d, want 3", len(policy.Spec.Variables))
	}
	if got := policy.Spec.Variables[1]; got.Name != schema.ParamsName || !strings.Contains(got.Expression, `"labels"`) {
		t.Fatalf("inlined params variable = %#v", got)
	}
	if got := policy.Spec.Variables[2].Expression; got != "variables.params.labels" {
		t.Fatalf("user variable changed to %q", got)
	}
	for _, condition := range policy.Spec.MatchConditions {
		referencesParams, err := expressionReferencesIdentifier(condition.Expression, "params")
		if err != nil {
			t.Fatalf("parse condition %q: %v", condition.Name, err)
		}
		if referencesParams {
			t.Fatalf("condition %q still references params: %s", condition.Name, condition.Expression)
		}
		if strings.Contains(condition.Expression, "owner is required") {
			t.Fatalf("condition %q duplicates Constraint parameters: %s", condition.Name, condition.Expression)
		}
	}
}

func TestSpecializeMatchConditionsOmitsAbsentFields(t *testing.T) {
	tests := []struct {
		name  string
		match map[string]interface{}
		want  int
	}{
		{name: "absent", want: 0},
		{name: "empty match", match: map[string]interface{}{}, want: 0},
		{name: "kinds only", match: map[string]interface{}{"kinds": []interface{}{map[string]interface{}{"kinds": []interface{}{"ConfigMap"}}}}, want: 1},
		{name: "empty kinds", match: map[string]interface{}{"kinds": []interface{}{}}, want: 1},
		{name: "null kinds", match: map[string]interface{}{"kinds": nil}, want: 1},
		{name: "empty namespaces", match: map[string]interface{}{"namespaces": []interface{}{}}, want: 1},
		{name: "all fields", match: map[string]interface{}{"kinds": nil, "name": "", "namespaces": nil, "excludedNamespaces": nil}, want: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			constraint := &unstructured.Unstructured{Object: map[string]interface{}{"spec": map[string]interface{}{}}}
			if test.match != nil {
				if err := unstructured.SetNestedMap(constraint.Object, test.match, "spec", "match"); err != nil {
					t.Fatal(err)
				}
			}
			custom := admissionregistrationv1beta1.MatchCondition{Name: MatchKindsV1Beta1().Name, Expression: "false"}
			conditions := append(AllMatchersV1Beta1(), custom)
			got, err := specializeMatchConditions(conditions, constraint)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != test.want+1 {
				t.Fatalf("conditions = %d, want %d", len(got), test.want+1)
			}
			if got[len(got)-1] != custom {
				t.Fatalf("custom condition changed: %#v", got[len(got)-1])
			}
		})
	}
}

func TestSpecializedMatchParity(t *testing.T) {
	fields := []struct {
		condition admissionregistrationv1beta1.MatchCondition
		field     string
		values    []interface{}
	}{
		{condition: MatchKindsV1Beta1(), field: "kinds", values: []interface{}{
			nil,
			[]interface{}{},
			"invalid",
			[]interface{}{nil},
			[]interface{}{map[string]interface{}{"kinds": nil}},
			[]interface{}{map[string]interface{}{"kinds": []interface{}{"*", int64(1)}}},
			[]interface{}{map[string]interface{}{}},
			[]interface{}{map[string]interface{}{"kinds": []interface{}{"ConfigMap"}, "apiGroups": []interface{}{""}}},
			[]interface{}{map[string]interface{}{"kinds": []interface{}{"Pod"}, "apiGroups": []interface{}{""}}, map[string]interface{}{"kinds": []interface{}{"ConfigMap"}, "apiGroups": []interface{}{"example.com"}}},
		}},
		{condition: MatchNameGlobV1Beta1(), field: "name", values: []interface{}{nil, int64(1), "", "test", "test*", "*test*", "\u30c6\u30b9\u30c8*", "a\"b*", "["}},
		{condition: MatchNamespacesGlobV1Beta1(), field: "namespaces", values: []interface{}{nil, "invalid", []interface{}{}, []interface{}{nil}, []interface{}{"test"}, []interface{}{"test*", "*-suffix"}, []interface{}{"*", "["}}},
		{condition: MatchExcludedNamespacesGlobV1Beta1(), field: "excludedNamespaces", values: []interface{}{nil, "invalid", []interface{}{}, []interface{}{nil}, []interface{}{"test"}, []interface{}{"test*", "*-suffix"}, []interface{}{"*", "["}}},
	}
	objects := []map[string]interface{}{
		nil,
		{},
		{"metadata": map[string]interface{}{}},
		{"metadata": map[string]interface{}{"name": "test", "namespace": "test"}},
		{"metadata": map[string]interface{}{"name": "other", "namespace": "other"}},
		{"metadata": map[string]interface{}{"generateName": "test", "namespace": "test-suffix"}},
		{"metadata": map[string]interface{}{"name": "test", "namespace": ""}},
	}
	for _, object := range objects {
		if object != nil {
			object["apiVersion"] = "v1"
			object["kind"] = "ConfigMap"
		}
	}
	for _, field := range fields {
		t.Run(field.field, func(t *testing.T) {
			compiler, err := cel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
			if err != nil {
				t.Fatal(err)
			}
			accessor := &matchconditions.MatchCondition{Name: field.condition.Name, Expression: field.condition.Expression}
			legacy := matchconditions.NewMatcher(compiler.CompileCondition([]cel.ExpressionAccessor{accessor}, cel.OptionalVariableDeclarations{HasParams: true}, environment.StoredExpressions), ptr.To(admissionregistrationv1.Fail), "test", "legacy", field.field)
			for _, value := range field.values {
				constraint := &unstructured.Unstructured{Object: map[string]interface{}{"spec": map[string]interface{}{"match": map[string]interface{}{field.field: value}}}}
				original := constraint.DeepCopy()
				input := []admissionregistrationv1beta1.MatchCondition{field.condition}
				if _, err := specializeMatchConditions(input, constraint); err != nil {
					t.Fatal(err)
				}
				if input[0] != field.condition || !reflect.DeepEqual(original, constraint) {
					t.Fatal("specialization changed its input")
				}
				for _, object := range objects {
					for _, oldObject := range objects {
						request := &admissionv1.AdmissionRequest{Kind: metav1.GroupVersionKind{Kind: "ConfigMap", Version: "v1"}}
						if object != nil {
							encoded, err := json.Marshal(object)
							if err != nil {
								t.Fatal(err)
							}
							request.Object = runtime.RawExtension{Raw: encoded}
						}
						if oldObject != nil {
							encoded, err := json.Marshal(oldObject)
							if err != nil {
								t.Fatal(err)
							}
							request.OldObject = runtime.RawExtension{Raw: encoded}
						}
						attributes, err := RequestToVersionedAttributes(request)
						if err != nil {
							t.Fatal(err)
						}
						want := legacy.Match(context.Background(), attributes, constraint, nil)
						checkSpecializedMatch(t, field.condition, constraint, request, want.Matches, want.Error != nil)
					}
				}
			}
		})
	}
}

func TestSpecializeNamespaceMatchLargeList(t *testing.T) {
	namespaces := make([]interface{}, 2000)
	for index := range namespaces {
		namespaces[index] = fmt.Sprintf("namespace-%04d", index)
	}
	request := &admissionv1.AdmissionRequest{
		Kind:   metav1.GroupVersionKind{Kind: "ConfigMap", Version: "v1"},
		Object: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test","namespace":"namespace-1999"}}`)},
	}
	for _, field := range []string{"namespaces", "excludedNamespaces"} {
		condition := MatchNamespacesGlobV1Beta1()
		want := true
		if field == "excludedNamespaces" {
			condition = MatchExcludedNamespacesGlobV1Beta1()
			want = false
		}
		constraint := &unstructured.Unstructured{Object: map[string]interface{}{"spec": map[string]interface{}{"match": map[string]interface{}{field: namespaces}}}}
		specialized, err := specializeMatchConditions([]admissionregistrationv1beta1.MatchCondition{condition}, constraint)
		if err != nil {
			t.Fatal(err)
		}
		if len(specialized[0].Expression) >= 100000 {
			t.Fatalf("large namespace expression expanded to %d bytes", len(specialized[0].Expression))
		}
		checkSpecializedMatch(t, condition, constraint, request, want, false)
	}
}

func TestConstraintToPolicyDefinitionRejectsDirectParams(t *testing.T) {
	tests := map[string]*schema.Source{
		"match condition": {MatchConditions: []schema.MatchCondition{{Name: "uses-params", Expression: "params.spec.parameters.enabled"}}},
		"variable":        {Variables: []schema.Variable{{Name: "usesParams", Expression: "params.spec.parameters.enabled"}}},
		"validation":      {Validations: []schema.Validation{{Expression: "params.spec.parameters.enabled"}}},
		"message":         {Validations: []schema.Validation{{Expression: "true", MessageExpression: `string(params.spec.parameters.message)`}}},
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			template := newInlineTestTemplate(source)
			constraint := newTestConstraint("deny", nil, nil, &unstructured.Unstructured{})
			_, err := ConstraintToPolicyDefinitionWithWebhookConfig(template, constraint, nil, nil, nil)
			if !errors.Is(err, ErrDirectParameterReference) {
				t.Fatalf("error = %v, want %v", err, ErrDirectParameterReference)
			}
		})
	}
}

func TestConstraintToInlinedBinding(t *testing.T) {
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"benchmark": "true"}}
	constraint := newTestConstraint("deny", nil, selector, &unstructured.Unstructured{})

	binding, err := ConstraintToInlinedBinding(constraint, []string{"deny"})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Name != "gatekeeper-footemplate-foo-name" {
		t.Fatalf("binding name = %q", binding.Name)
	}
	if binding.Spec.PolicyName != "gatekeeper-footemplate-foo-name-vap" {
		t.Fatalf("policy name = %q", binding.Spec.PolicyName)
	}
	if binding.Spec.ParamRef != nil {
		t.Fatalf("ParamRef = %#v, want nil", binding.Spec.ParamRef)
	}
	if binding.Spec.MatchResources.ObjectSelector == nil || binding.Spec.MatchResources.ObjectSelector.MatchLabels["benchmark"] != "true" {
		t.Fatalf("object selector = %#v", binding.Spec.MatchResources.ObjectSelector)
	}
}

func TestGetConstraintVAPName(t *testing.T) {
	if got := GetConstraintVAPName("K8sRequiredLabels", "team-labels"); got != "gatekeeper-k8srequiredlabels-team-labels-vap" {
		t.Fatalf("GetConstraintVAPName() = %q", got)
	}
	longName := strings.Repeat("name", 80)
	got := GetConstraintVAPName("K8sRequiredLabels", longName)
	if len(got) != 253 {
		t.Fatalf("name length = %d, want 253", len(got))
	}
	if got != GetConstraintVAPName("K8sRequiredLabels", longName) {
		t.Fatal("truncated VAP name is not deterministic")
	}
	if got == GetVAPBindingName("K8sRequiredLabels", longName) {
		t.Fatal("VAP and VAPBinding names must remain distinct")
	}
}

func newInlineTestTemplate(source *schema.Source) *templates.ConstraintTemplate {
	return &templates.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "footemplate"},
		Spec: templates.ConstraintTemplateSpec{
			CRD: templates.CRD{Spec: templates.CRDSpec{Names: templates.Names{Kind: "FooTemplate"}}},
			Targets: []templates.Target{{
				Target: "admission.k8s.gatekeeper.sh",
				Code: []templates.Code{{
					Engine: schema.Name,
					Source: &templates.Anything{Value: source.MustToUnstructured()},
				}},
			}},
		},
	}
}
