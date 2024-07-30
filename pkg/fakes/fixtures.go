package fakes

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func DenyAllRegoTemplate() *templates.ConstraintTemplate {
	return &templates.ConstraintTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: templatesv1beta1.SchemeGroupVersion.String(),
			Kind:       "ConstraintTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "denyall",
		},
		Spec: templates.ConstraintTemplateSpec{
			CRD: templates.CRD{
				Spec: templates.CRDSpec{
					Names: templates.Names{
						Kind: "denyall",
					},
				},
			},
			Targets: []templates.Target{{
				Target: target.Name,
				Code: []templates.Code{{
					Engine: "Rego",
					Source: &templates.Anything{
						Value: map[string]interface{}{"rego": `
package goodrego

violation[{"msg": msg}] {
   msg := "denyall"
}`},
					},
				}},
			}},
		},
	}
}

func DenyAllConstraint() *unstructured.Unstructured {
	return ConstraintFor("denyall")
}

func ScopedConstraintFor(ep string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"enforcementAction": "scoped",
				"scopedEnforcementActions": []interface{}{
					map[string]interface{}{
						"enforcementPoints": []interface{}{
							map[string]interface{}{
								"name": ep,
							},
						},
						"action": string(util.Deny),
					},
					map[string]interface{}{
						"enforcementPoints": []interface{}{
							map[string]interface{}{
								"name": ep,
							},
						},
						"action": string(util.Warn),
					},
				},
			},
		},
	}

	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   constraints.Group,
		Version: "v1beta1",
		Kind:    "denyall",
	})
	u.SetName("constraint")

	return u
}

func ConstraintFor(kind string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}

	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   constraints.Group,
		Version: "v1beta1",
		Kind:    kind,
	})
	u.SetName("constraint")

	return u
}
