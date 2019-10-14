package target

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"text/template"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/pkg/errors"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

var _ client.TargetHandler = &K8sValidationTarget{}

type K8sValidationTarget struct{}

func (h *K8sValidationTarget) GetName() string {
	return "admission.k8s.gatekeeper.sh"
}

var libTempl = template.Must(template.New("library").Parse(templSrc))

func (h *K8sValidationTarget) Library() *template.Template {
	return libTempl
}

type WipeData struct{}

func processWipeData() (bool, string, interface{}, error) {
	return true, "", nil, nil
}

func processUnstructured(o *unstructured.Unstructured) (bool, string, interface{}, error) {
	// Namespace will be "" for cluster objects
	gvk := o.GetObjectKind().GroupVersionKind()
	if gvk.Version == "" {
		return true, "", nil, fmt.Errorf("resource %s has no version", o.GetName())
	}
	if gvk.Kind == "" {
		return true, "", nil, fmt.Errorf("resource %s has no kind", o.GetName())
	}

	if o.GetNamespace() == "" {
		return true, path.Join("cluster", url.PathEscape(gvk.GroupVersion().String()), gvk.Kind, o.GetName()), o.Object, nil
	}
	return true, path.Join("namespace", o.GetNamespace(), url.PathEscape(gvk.GroupVersion().String()), gvk.Kind, o.GetName()), o.Object, nil
}

func (h *K8sValidationTarget) ProcessData(obj interface{}) (bool, string, interface{}, error) {
	switch data := obj.(type) {
	case unstructured.Unstructured:
		return processUnstructured(&data)
	case *unstructured.Unstructured:
		return processUnstructured(data)
	case WipeData, *WipeData:
		return processWipeData()
	default:
		return false, "", nil, nil
	}
}

func (h *K8sValidationTarget) HandleReview(obj interface{}) (bool, interface{}, error) {
	switch data := obj.(type) {
	case admissionv1beta1.AdmissionRequest:
		return true, data, nil
	case *admissionv1beta1.AdmissionRequest:
		return true, data, nil
	}
	return false, nil, nil
}

func getString(m map[string]interface{}, k string) (string, error) {
	val, exists, err := unstructured.NestedFieldNoCopy(m, "kind", k)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("review[kind][%s] does not exist", k)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("review[kind][%s] is not a string: %+v", k, val)
	}
	return s, nil
}

func (h *K8sValidationTarget) HandleViolation(result *types.Result) error {
	rmap, ok := result.Review.(map[string]interface{})
	if !ok {
		return fmt.Errorf("could not cast review as map[string]: %+v", result.Review)
	}
	group, err := getString(rmap, "group")
	if err != nil {
		return err
	}
	version, err := getString(rmap, "version")
	if err != nil {
		return err
	}
	kind, err := getString(rmap, "kind")
	if err != nil {
		return err
	}
	var apiVersion string
	if group == "" {
		apiVersion = version
	} else {
		apiVersion = fmt.Sprintf("%s/%s", group, version)
	}

	objMap, found, err := unstructured.NestedMap(rmap, "object")
	if err != nil {
		return errors.Wrap(err, "HandleViolation:NestedMap")
	}
	if !found {
		return errors.New("no object returned in review")
	}
	objMap["apiVersion"] = apiVersion
	objMap["kind"] = kind

	js, err := json.Marshal(objMap)
	if err != nil {
		return errors.Wrap(err, "HandleViolation:Marshal(Object)")
	}
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(js, obj); err != nil {
		return errors.Wrap(err, "HandleViolation:Unmarshal(unstructured)")
	}
	result.Resource = obj
	return nil
}

func (h *K8sValidationTarget) MatchSchema() apiextensions.JSONSchemaProps {
	stringList := &apiextensions.JSONSchemaPropsOrArray{
		Schema: &apiextensions.JSONSchemaProps{Type: "string"}}
	labelSelectorSchema := apiextensions.JSONSchemaProps{
		Properties: map[string]apiextensions.JSONSchemaProps{
			// Map schema validation will only work in kubernetes versions > 1.10. See https://github.com/kubernetes/kubernetes/pull/62333
			//"matchLabels": apiextensions.JSONSchemaProps{
			//	AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
			//		Allows: true,
			//		Schema: &apiextensions.JSONSchemaProps{Type: "string"},
			//	},
			//},
			"matchExpressions": apiextensions.JSONSchemaProps{
				Type: "array",
				Items: &apiextensions.JSONSchemaPropsOrArray{
					Schema: &apiextensions.JSONSchemaProps{
						Properties: map[string]apiextensions.JSONSchemaProps{
							"key": apiextensions.JSONSchemaProps{Type: "string"},
							"operator": apiextensions.JSONSchemaProps{
								Type: "string",
								Enum: []apiextensions.JSON{
									"In",
									"NotIn",
									"Exists",
									"DoesNotExist",
								},
							},
							"values": apiextensions.JSONSchemaProps{
								Type: "array",
								Items: &apiextensions.JSONSchemaPropsOrArray{
									Schema: &apiextensions.JSONSchemaProps{Type: "string"},
								},
							},
						},
					},
				},
			},
		},
	}
	return apiextensions.JSONSchemaProps{
		Properties: map[string]apiextensions.JSONSchemaProps{
			"kinds": apiextensions.JSONSchemaProps{
				Type: "array",
				Items: &apiextensions.JSONSchemaPropsOrArray{
					Schema: &apiextensions.JSONSchemaProps{
						Properties: map[string]apiextensions.JSONSchemaProps{
							"apiGroups": {Items: stringList},
							"kinds":     {Items: stringList},
						},
					},
				},
			},
			"namespaces": apiextensions.JSONSchemaProps{
				Type: "array",
				Items: &apiextensions.JSONSchemaPropsOrArray{
					Schema: &apiextensions.JSONSchemaProps{Type: "string"}}},
			"labelSelector":     labelSelectorSchema,
			"namespaceSelector": labelSelectorSchema,
		},
	}
}

func (h *K8sValidationTarget) ValidateConstraint(u *unstructured.Unstructured) error {
	labelSelector, found, err := unstructured.NestedMap(u.Object, "spec", "match", "labelSelector")
	if err != nil {
		return err
	}

	if found && labelSelector != nil {
		labelSelectorObj, err := convertToLabelSelector(labelSelector)
		if err != nil {
			return err
		}
		errorList := validation.ValidateLabelSelector(labelSelectorObj, field.NewPath("spec", "labelSelector"))
		if len(errorList) > 0 {
			return errorList.ToAggregate()
		}
	}

	namespaceSelector, found, err := unstructured.NestedMap(u.Object, "spec", "match", "namespaceSelector")
	if err != nil {
		return err
	}

	if found && namespaceSelector != nil {
		namespaceSelectorObj, err := convertToLabelSelector(namespaceSelector)
		if err != nil {
			return err
		}
		errorList := validation.ValidateLabelSelector(namespaceSelectorObj, field.NewPath("spec", "labelSelector"))
		if len(errorList) > 0 {
			return errorList.ToAggregate()
		}
	}

	return nil
}

func convertToLabelSelector(object map[string]interface{}) (*metav1.LabelSelector, error) {
	j, err := json.Marshal(object)
	if err != nil {
		return nil, errors.Wrap(err, "Could not convert unknown object to JSON")
	}
	obj := &metav1.LabelSelector{}
	if err := json.Unmarshal(j, obj); err != nil {
		return nil, errors.Wrap(err, "Could not convert JSON to LabelSelector")
	}
	return obj, nil
}
