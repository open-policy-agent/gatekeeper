package target

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"text/template"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/pkg/errors"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// This pattern is meant to match:
//
//   REGULAR NAMESPACES
//   - These are defined by this pattern: [a-z0-9]([-a-z0-9]*[a-z0-9])?
//   - You'll see that this is the first two-thirds or so of the pattern below
//
//   PREFIX OR SUFFIX BASED WILDCARDS
//   - A typical namespace must end in an alphanumeric character.  A prefixed wildcard
//     can end in "*" (like `kube*`) or "-*" (like `kube-*`), and a suffixed wildcard
//     can start with "*" (like `*system`) or "*-" (like `*-system`).
//   - To implement this, we add either (\*|\*-)? as a prefix or (\*|-\*)? as a suffix.
//     Using both prefixed wildcards and suffixed wildcards at once is not supported.  Therefore,
//     this _does not_ allow the value to start _and_ end in a wildcard (like `*-*`).
//   - Crucially, this _does not_ allow the value to start or end in a dash (like `-system` or `kube-`).
//     That is not a valid namespace and not a wildcard, so it's disallowed.
//
//   Notably, this disallows other uses of the "*" character like:
//   - *
//   - k*-system
//
// See the following regexr to test this regex: https://regexr.com/6dgdj
const wildcardNSPattern = `^(\*|\*-)?[a-z0-9]([-a-z0-9]*[a-z0-9])?$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\*|-\*)?$`

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

type AugmentedReview struct {
	AdmissionRequest *admissionv1.AdmissionRequest
	Namespace        *corev1.Namespace
}

type gkReview struct {
	*admissionv1.AdmissionRequest
	Unstable *unstable `json:"_unstable,omitempty"`
}

type AugmentedUnstructured struct {
	Object    unstructured.Unstructured
	Namespace *corev1.Namespace
}

type unstable struct {
	Namespace *corev1.Namespace `json:"namespace,omitempty"`
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
	case admissionv1.AdmissionRequest:
		return true, data, nil
	case *admissionv1.AdmissionRequest:
		return true, data, nil
	case AugmentedReview:
		return true, &gkReview{AdmissionRequest: data.AdmissionRequest, Unstable: &unstable{Namespace: data.Namespace}}, nil
	case *AugmentedReview:
		return true, &gkReview{AdmissionRequest: data.AdmissionRequest, Unstable: &unstable{Namespace: data.Namespace}}, nil
	case AugmentedUnstructured:
		admissionRequest, err := augmentedUnstructuredToAdmissionRequest(data)
		if err != nil {
			return false, nil, err
		}
		return true, admissionRequest, nil
	case *AugmentedUnstructured:
		admissionRequest, err := augmentedUnstructuredToAdmissionRequest(*data)
		if err != nil {
			return false, nil, err
		}
		return true, admissionRequest, nil
	case unstructured.Unstructured:
		admissionRequest, err := unstructuredToAdmissionRequest(data)
		if err != nil {
			return false, nil, err
		}
		return true, admissionRequest, nil
	case *unstructured.Unstructured:
		admissionRequest, err := unstructuredToAdmissionRequest(*data)
		if err != nil {
			return false, nil, err
		}
		return true, admissionRequest, nil
	}
	return false, nil, nil
}

func augmentedUnstructuredToAdmissionRequest(obj AugmentedUnstructured) (gkReview, error) {
	req, err := unstructuredToAdmissionRequest(obj.Object)
	if err != nil {
		return gkReview{}, err
	}

	review := gkReview{AdmissionRequest: &req, Unstable: &unstable{Namespace: obj.Namespace}}

	if obj.Namespace != nil {
		review.Namespace = obj.Namespace.Name
	}

	return review, nil
}

func unstructuredToAdmissionRequest(obj unstructured.Unstructured) (admissionv1.AdmissionRequest, error) {
	resourceJSON, err := json.Marshal(obj.Object)
	if err != nil {
		return admissionv1.AdmissionRequest{}, errors.New("Unable to marshal JSON encoding of object")
	}

	req := admissionv1.AdmissionRequest{
		Kind: metav1.GroupVersionKind{
			Group:   obj.GetObjectKind().GroupVersionKind().Group,
			Version: obj.GetObjectKind().GroupVersionKind().Version,
			Kind:    obj.GetObjectKind().GroupVersionKind().Kind,
		},
		Object: runtime.RawExtension{
			Raw: resourceJSON,
		},
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}

	return req, nil
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

// nestedMap augments unstructured.NestedMap to interpret a nil-valued field
// as missing.
func nestedMap(rmap map[string]interface{}, field string) (map[string]interface{}, bool, error) {
	objMap, found, err := unstructured.NestedMap(rmap, field)
	if err != nil || !found {
		if val, found, err2 := unstructured.NestedFieldNoCopy(rmap, field); val == nil && found && err2 == nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	return objMap, true, nil
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

	objMap, found, err := nestedMap(rmap, "object")
	if err != nil {
		return errors.Wrap(err, "HandleViolation:NestedMap")
	}
	if !found {
		objMap, found, err = nestedMap(rmap, "oldObject")
		if err != nil {
			return errors.Wrap(err, "HandleViolation:NestedMapOldObj")
		}
		if !found {
			return errors.New("no object or oldObject returned in review")
		}
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

func propsWithDescription(props *apiextensions.JSONSchemaProps, description string) *apiextensions.JSONSchemaProps {
	propCopy := props.DeepCopy()
	propCopy.Description = description
	return propCopy
}

func (h *K8sValidationTarget) MatchSchema() apiextensions.JSONSchemaProps {
	// Define some repeatedly used sections
	wildcardNSList := apiextensions.JSONSchemaProps{
		Type: "array",
		Items: &apiextensions.JSONSchemaPropsOrArray{
			Schema: &apiextensions.JSONSchemaProps{Type: "string", Pattern: wildcardNSPattern},
		},
	}

	nullableStringList := apiextensions.JSONSchemaProps{
		Type: "array",
		Items: &apiextensions.JSONSchemaPropsOrArray{
			Schema: &apiextensions.JSONSchemaProps{Type: "string", Nullable: true},
		},
	}

	trueBool := true
	labelSelectorSchema := apiextensions.JSONSchemaProps{
		Type: "object",
		Properties: map[string]apiextensions.JSONSchemaProps{
			"matchLabels": {
				Type:        "object",
				Description: "A mapping of label keys to sets of allowed label values for those keys.  A selected resource will match all of these expressions.",
				AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
					Allows: true,
					Schema: &apiextensions.JSONSchemaProps{Type: "string"},
				},
				XPreserveUnknownFields: &trueBool,
			},
			"matchExpressions": {
				Type:        "array",
				Description: "a list of label selection expressions. A selected resource will match all of these expressions.",
				Items: &apiextensions.JSONSchemaPropsOrArray{
					Schema: &apiextensions.JSONSchemaProps{
						Type:        "object",
						Description: "a selector that specifies a label key, a set of label values, an operator that defines the relationship between the two that will match the selector.",
						Properties: map[string]apiextensions.JSONSchemaProps{
							"key": {
								Description: "the label key that the selector applies to.",
								Type:        "string",
							},
							"operator": {
								Type:        "string",
								Description: "the relationship between the label and value set that defines a matching selection.",
								Enum: []apiextensions.JSON{
									"In",
									"NotIn",
									"Exists",
									"DoesNotExist",
								},
							},
							"values": {
								Type:        "array",
								Description: "a set of label values.",
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

	// Make sure to copy description changes into pkg/mutation/match/match.go's `Match` struct.
	return apiextensions.JSONSchemaProps{
		Type: "object",
		Properties: map[string]apiextensions.JSONSchemaProps{
			"kinds": {
				Type: "array",
				Items: &apiextensions.JSONSchemaPropsOrArray{
					Schema: &apiextensions.JSONSchemaProps{
						Type:        "object",
						Description: "The Group and Kind of objects that should be matched.  If multiple groups/kinds combinations are specified, an incoming resource need only match one to be in scope.",
						Properties: map[string]apiextensions.JSONSchemaProps{
							"apiGroups": nullableStringList,
							"kinds":     nullableStringList,
						},
					},
				},
			},
			"namespaces":         *propsWithDescription(&wildcardNSList, "`namespaces` is a list of namespace names. If defined, a constraint only applies to resources in a listed namespace.  Namespaces also supports a prefix-based glob.  For example, `namespaces: [kube-*]` matches both `kube-system` and `kube-public`."),
			"excludedNamespaces": *propsWithDescription(&wildcardNSList, "`excludedNamespaces` is a list of namespace names. If defined, a constraint only applies to resources not in a listed namespace. ExcludedNamespaces also supports a prefix-based glob.  For example, `excludedNamespaces: [kube-*]` matches both `kube-system` and `kube-public`."),
			"labelSelector":      *propsWithDescription(&labelSelectorSchema, "`labelSelector` is the combination of two optional fields: `matchLabels` and `matchExpressions`.  These two fields provide different methods of selecting or excluding k8s objects based on the label keys and values included in object metadata.  All selection expressions from both sections are ANDed to determine if an object meets the cumulative requirements of the selector."),
			"namespaceSelector":  *propsWithDescription(&labelSelectorSchema, "`namespaceSelector` is a label selector against an object's containing namespace or the object itself, if the object is a namespace."),
			"scope": {
				Type:        "string",
				Description: "`scope` determines if cluster-scoped and/or namespaced-scoped resources are matched.  Accepts `*`, `Cluster`, or `Namespaced`. (defaults to `*`)",
				Enum: []apiextensions.JSON{
					"*",
					"Cluster",
					"Namespaced",
				},
			},
			"name": {
				Type:        "string",
				Description: "`name` is the name of an object.  If defined, it matches against objects with the specified name.  Name also supports a prefix-based glob.  For example, `name: pod-*` matches both `pod-a` and `pod-b`.",
				Pattern:     wildcardNSPattern,
			},
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

func (h *K8sValidationTarget) ToMatcher(u *unstructured.Unstructured) (constraints.Matcher, error) {
	panic("not implemented")
}
