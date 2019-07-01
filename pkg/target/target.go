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
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
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

const templSrc = `
package target

##################
# Required Hooks #
##################

matching_constraints[constraint] {
	trace(sprintf("INPUT IS: %v", [input]))
	constraint := {{.ConstraintsRoot}}[_][_]
	spec := get_default(constraint, "spec", {})
	match := get_default(spec, "match", {})

  any_kind_selector_matches(match)

	matches_namespaces(match)

	matches_nsselector(match)

  label_selector := get_default(match, "labelSelector", {})
	obj := get_default(input.review, "object", {})
	metadata := get_default(obj, "metadata", {})
  labels := get_default(metadata, "labels", {})
	matches_label_selector(label_selector, labels)
}

# Namespace-scoped objects
matching_reviews_and_constraints[[review, constraint]] {
	obj = {{.DataRoot}}.namespace[namespace][api_version][kind][name]
	r := make_review(obj, api_version, kind, name)
	review := add_field(r, "namespace", namespace)
	matching_constraints[constraint] with input as {"review": review}
}

# Cluster-scoped objects
matching_reviews_and_constraints[[review, constraint]] {
	obj = {{.DataRoot}}.cluster[api_version][kind][name]
	review = make_review(obj, api_version, kind, name)
	matching_constraints[constraint] with input as {"review": review}
}

make_review(obj, api_version, kind, name) = review {
	[group, version] := make_group_version(api_version)
	review := {
		"kind": {"group": group, "version": version, "kind": kind},
		"name": name,
		"operation": "CREATE",
		"object": obj
	}
}

########
# Util #
########

make_group_version(api_version) = [group, version] {
	contains(api_version, "/")
	[group, version] := split(api_version, "/")
}

make_group_version(api_version) = [group, version] {
	not contains(api_version, "/")
	group := ""
	version := api_version
}

add_field(object, key, value) = ret {
	keys := {k | object[k]}
	allKeys = keys | {key}
	ret := {k: v | v = get_default(object, k, value); allKeys[k]}
}

# has_field returns whether an object has a field
has_field(object, field) = true {
  object[field]
}

# False is a tricky special case, as false responses would create an undefined document unless
# they are explicitly tested for
has_field(object, field) = true {
  object[field] == false
}

has_field(object, field) = false {
  not object[field]
  not object[field] == false
}

# get_default returns the value of an object's field or the provided default value.
# It avoids creating an undefined state when trying to access an object attribute that does
# not exist
get_default(object, field, _default) = output {
  has_field(object, field)
  output = object[field]
}

get_default(object, field, _default) = output {
  has_field(object, field) == false
  output = _default
}

#######################
# Kind Selector Logic #
#######################

any_kind_selector_matches(match) {
	kind_selectors := get_default(match, "kinds", [{"apiGroups": ["*"], "kinds": ["*"]}])
  ks := kind_selectors[_]
  kind_selector_matches(ks)
}

kind_selector_matches(ks) {
	group_matches(ks)
  kind_matches(ks)
}

group_matches(ks) {
	ks.apiGroups[_] == "*"
}

group_matches(ks) {
	ks.apiGroups[_] == input.review.kind.group
}

kind_matches(ks) {
	ks.kinds[_] == "*"
}

kind_matches(ks) {
	ks.kinds[_] == input.review.kind.kind
}

########################
# Label Selector Logic #
########################

# match_expression_violated checks to see if a match expression is violated.
match_expression_violated("In", labels, key, values) = true {
  has_field(labels, key) == false
}

match_expression_violated("In", labels, key, values) = true {
  # values array must be non-empty for rule to be valid
  count(values) > 0
  valueSet := {v | v = values[_]}
  count({labels[key]} - valueSet) != 0
}

# No need to check if labels has the key, because a missing key is automatic non-violation
match_expression_violated("NotIn", labels, key, values) = true {
  # values array must be non-empty for rule to be valid
  count(values) > 0
  valueSet := {v | v = values[_]}
  count({labels[key]} - valueSet) == 0
}

match_expression_violated("Exists", labels, key, values) = true {
  has_field(labels, key) == false
}

match_expression_violated("DoesNotExist", labels, key, values) = true {
  has_field(labels, key) == true
}


# Checks to see if a kubernetes LabelSelector matches a given set of labels
# A non-existent selector or labels should be represented by an empty object ("{}")
matches_label_selector(selector, labels) {
  keys := {key | labels[key]}
  matchLabels := get_default(selector, "matchLabels", {})
  satisfiedMatchLabels := {key | matchLabels[key] == labels[key]}
  count(satisfiedMatchLabels) == count(matchLabels)

  matchExpressions := get_default(selector, "matchExpressions", [])

  mismatches := {failure | failure = true; failure = match_expression_violated(
    matchExpressions[i]["operator"],
    labels,
    matchExpressions[i]["key"],
    get_default(matchExpressions[i], "values", []))}

  any(mismatches) == false
}

############################
# Namespace Selector Logic #
############################

matches_namespaces(match) {
	not has_field(match, "namespaces")
}

matches_namespaces(match) {
	has_field(match, "namespaces")
	ns := {n | n = match.namespaces[_]}
	count({input.review.namespace} - ns) == 0
}

matches_nsselector(match) {
	not has_field(match, "namespaceSelector")
}

matches_nsselector(match) {
	has_field(match, "namespaceSelector")
	ns := {{.DataRoot}}.cluster["v1"]["Namespace"][input.review.namespace]
	matches_namespace_selector(match, ns)
}

matches_namespace_selector(match, ns) {
	metadata := get_default(ns, "metadata", {})
    nslabels := get_default(metadata, "labels", {})
	namespace_selector := get_default(match, "namespaceSelector", {})
	matches_label_selector(namespace_selector, nslabels)
}

`

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

func (h *K8sValidationTarget) MatchSchema() apiextensionsv1beta1.JSONSchemaProps {
	stringList := &apiextensionsv1beta1.JSONSchemaPropsOrArray{
		Schema: &apiextensionsv1beta1.JSONSchemaProps{Type: "string"}}
	return apiextensionsv1beta1.JSONSchemaProps{
		Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
			"kinds": apiextensionsv1beta1.JSONSchemaProps{
				Type: "array",
				Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
					Schema: &apiextensionsv1beta1.JSONSchemaProps{
						Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
							"apiGroups": {Items: stringList},
							"kinds":     {Items: stringList},
						},
					},
				},
			},
			"namespaces": apiextensionsv1beta1.JSONSchemaProps{
				Type: "array",
				Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
					Schema: &apiextensionsv1beta1.JSONSchemaProps{Type: "string"}}},
			"labelSelector": apiextensionsv1beta1.JSONSchemaProps{
				Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
					// Map schema validation will only work in kubernetes versions > 1.10. See https://github.com/kubernetes/kubernetes/pull/62333
					//"matchLabels": apiextensionsv1beta1.JSONSchemaProps{
					//	AdditionalProperties: &apiextensionsv1beta1.JSONSchemaPropsOrBool{
					//		Allows: true,
					//		Schema: &apiextensionsv1beta1.JSONSchemaProps{Type: "string"},
					//	},
					//},
					"matchExpressions": apiextensionsv1beta1.JSONSchemaProps{
						Type: "array",
						Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
							Schema: &apiextensionsv1beta1.JSONSchemaProps{
								Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
									"key": apiextensionsv1beta1.JSONSchemaProps{Type: "string"},
									"operator": apiextensionsv1beta1.JSONSchemaProps{
										Type: "string",
										Enum: []apiextensionsv1beta1.JSON{
											apiextensionsv1beta1.JSON{Raw: []byte(`"In"`)},
											apiextensionsv1beta1.JSON{Raw: []byte(`"NotIn"`)},
											apiextensionsv1beta1.JSON{Raw: []byte(`"Exists"`)},
											apiextensionsv1beta1.JSON{Raw: []byte(`"DoesNotExist"`)},
										}},
									"values": apiextensionsv1beta1.JSONSchemaProps{
										Type: "array",
										Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
											Schema: &apiextensionsv1beta1.JSONSchemaProps{Type: "string"},
										},
									},
								},
							},
						},
					},
				},
			},
			"namespaceSelector": apiextensionsv1beta1.JSONSchemaProps{
				Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
					// Map schema validation will only work in kubernetes versions > 1.10. See https://github.com/kubernetes/kubernetes/pull/62333
					//"matchLabels": apiextensionsv1beta1.JSONSchemaProps{
					//	AdditionalProperties: &apiextensionsv1beta1.JSONSchemaPropsOrBool{
					//		Allows: true,
					//		Schema: &apiextensionsv1beta1.JSONSchemaProps{Type: "string"},
					//	},
					//},
					"matchExpressions": apiextensionsv1beta1.JSONSchemaProps{
						Type: "array",
						Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
							Schema: &apiextensionsv1beta1.JSONSchemaProps{
								Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
									"key": apiextensionsv1beta1.JSONSchemaProps{Type: "string"},
									"operator": apiextensionsv1beta1.JSONSchemaProps{
										Type: "string",
										Enum: []apiextensionsv1beta1.JSON{
											apiextensionsv1beta1.JSON{Raw: []byte(`"In"`)},
											apiextensionsv1beta1.JSON{Raw: []byte(`"NotIn"`)},
											apiextensionsv1beta1.JSON{Raw: []byte(`"Exists"`)},
											apiextensionsv1beta1.JSON{Raw: []byte(`"DoesNotExist"`)},
										}},
									"values": apiextensionsv1beta1.JSONSchemaProps{
										Type: "array",
										Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
											Schema: &apiextensionsv1beta1.JSONSchemaProps{Type: "string"},
										},
									},
								},
							},
						},
					},
				},
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
