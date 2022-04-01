package target

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/handler"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/opa/storage"
	"github.com/pkg/errors"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// Name is the name of Gatekeeper's Kubernetes validation target.
const Name = "admission.k8s.gatekeeper.sh"

var _ handler.TargetHandler = &K8sValidationTarget{}

type K8sValidationTarget struct {
	cache nsCache
}

func (h *K8sValidationTarget) GetName() string {
	return Name
}

type wipeData struct{}

func WipeData() interface{} {
	return wipeData{}
}

func IsWipeData(o interface{}) bool {
	_, ok := o.(wipeData)
	return ok
}

type AugmentedReview struct {
	AdmissionRequest *admissionv1.AdmissionRequest
	Namespace        *corev1.Namespace
}

type gkReview struct {
	admissionv1.AdmissionRequest
	Unstable unstable `json:"_unstable,omitempty"`
}

type AugmentedUnstructured struct {
	Object    unstructured.Unstructured
	Namespace *corev1.Namespace
}

type unstable struct {
	Namespace *corev1.Namespace `json:"namespace,omitempty"`
}

func (h *K8sValidationTarget) processUnstructured(o *unstructured.Unstructured) (bool, storage.Path, interface{}, error) {
	// Namespace will be "" for cluster objects
	gvk := o.GetObjectKind().GroupVersionKind()
	if gvk.Version == "" {
		return true, nil, nil, fmt.Errorf("%w: resource %s has no version", ErrRequestObject, o.GetName())
	}
	if gvk.Kind == "" {
		return true, nil, nil, fmt.Errorf("%w: resource %s has no kind", ErrRequestObject, o.GetName())
	}

	var path []string
	if o.GetNamespace() == "" {
		path = clusterScopedKey(gvk, o.GetName())
	} else {
		path = namespaceScopedKey(o.GetNamespace(), gvk, o.GetName())
	}

	return true, path, o.Object, nil
}

func clusterScopedKey(gvk schema.GroupVersionKind, name string) storage.Path {
	return []string{"cluster", gvk.GroupVersion().String(), gvk.Kind, name}
}

func namespaceScopedKey(namespace string, gvk schema.GroupVersionKind, name string) storage.Path {
	return []string{"namespace", namespace, gvk.GroupVersion().String(), gvk.Kind, name}
}

func (h *K8sValidationTarget) ProcessData(obj interface{}) (bool, storage.Path, interface{}, error) {
	switch data := obj.(type) {
	case unstructured.Unstructured:
		return h.processUnstructured(&data)
	case *unstructured.Unstructured:
		return h.processUnstructured(data)
	case wipeData, *wipeData:
		return true, nil, nil, nil
	default:
		return false, nil, nil, nil
	}
}

func (h *K8sValidationTarget) HandleReview(obj interface{}) (bool, interface{}, error) {
	return h.handleReview(obj)
}

// handleReview returns a complete *gkReview to pass to the Client.
func (h *K8sValidationTarget) handleReview(obj interface{}) (bool, *gkReview, error) {
	var err error
	var review *gkReview

	switch data := obj.(type) {
	case admissionv1.AdmissionRequest:
		review = &gkReview{AdmissionRequest: data}
	case *admissionv1.AdmissionRequest:
		review = &gkReview{AdmissionRequest: *data}
	case AugmentedReview:
		review = &gkReview{AdmissionRequest: *data.AdmissionRequest, Unstable: unstable{Namespace: data.Namespace}}
	case *AugmentedReview:
		review = &gkReview{AdmissionRequest: *data.AdmissionRequest, Unstable: unstable{Namespace: data.Namespace}}
	case AugmentedUnstructured:
		review, err = augmentedUnstructuredToAdmissionRequest(data)
		if err != nil {
			return false, nil, err
		}
	case *AugmentedUnstructured:
		review, err = augmentedUnstructuredToAdmissionRequest(*data)
		if err != nil {
			return false, nil, err
		}
	case unstructured.Unstructured:
		review, err = unstructuredToAdmissionRequest(&data)
		if err != nil {
			return false, nil, err
		}
	case *unstructured.Unstructured:
		review, err = unstructuredToAdmissionRequest(data)
		if err != nil {
			return false, nil, err
		}
	default:
		return false, nil, nil
	}

	return true, review, nil
}

func augmentedUnstructuredToAdmissionRequest(obj AugmentedUnstructured) (*gkReview, error) {
	review, err := unstructuredToAdmissionRequest(&obj.Object)
	if err != nil {
		return nil, err
	}

	review.Unstable = unstable{Namespace: obj.Namespace}

	return review, nil
}

func unstructuredToAdmissionRequest(obj *unstructured.Unstructured) (*gkReview, error) {
	resourceJSON, err := obj.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("%w: unable to marshal JSON encoding of object", ErrRequestObject)
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

	return &gkReview{AdmissionRequest: req}, nil
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

// TODO: can we use generic for Unmarshal after go 1.18?
func convertToMatch(object map[string]interface{}) (*match.Match, error) {
	j, err := json.Marshal(object)
	if err != nil {
		return nil, errors.Wrap(err, "could not convert unknown object to JSON")
	}
	obj := &match.Match{}
	if err := json.Unmarshal(j, obj); err != nil {
		return nil, errors.Wrap(err, "could not convert JSON to Match")
	}
	return obj, nil
}

// ToMatcher converts .spec.match in mutators to Matcher.
func (h *K8sValidationTarget) ToMatcher(u *unstructured.Unstructured) (constraints.Matcher, error) {
	obj, found, err := unstructured.NestedMap(u.Object, "spec", "match")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCreatingMatcher, err)
	}

	if found && obj != nil {
		m, err := convertToMatch(obj)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCreatingMatcher, err)
		}
		return &Matcher{match: m, cache: &h.cache}, nil
	}

	return &Matcher{}, nil
}

func (h *K8sValidationTarget) GetCache() handler.Cache {
	return &h.cache
}

// Matcher implements constraint.Matcher.
type Matcher struct {
	match *match.Match
	cache *nsCache
}

type nsCache struct {
	lock  sync.RWMutex
	cache map[string]*corev1.Namespace
}

func (c *nsCache) Add(key storage.Path, object interface{}) error {
	obj, ok := object.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%w: cannot cache type %T, want %T", ErrCachingType, object, map[string]interface{}{})
	}

	u := &unstructured.Unstructured{Object: obj}

	nsType := schema.GroupKind{Kind: "Namespace"}
	if u.GroupVersionKind().GroupKind() != nsType {
		return nil
	}

	ns, err := toNamespace(u)
	if err != nil {
		return fmt.Errorf("%w: cannot cache Namespace: %v", ErrCachingType, ns)
	}

	c.AddNamespace(key.String(), ns)

	return nil
}

func toNamespace(u *unstructured.Unstructured) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, ns)
	if err != nil {
		return nil, err
	}

	return ns, nil
}

func (c *nsCache) AddNamespace(key string, ns *corev1.Namespace) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.cache == nil {
		c.cache = make(map[string]*corev1.Namespace)
	}

	c.cache[key] = ns
}

func (c *nsCache) GetNamespace(name string) *corev1.Namespace {
	key := clusterScopedKey(corev1.SchemeGroupVersion.WithKind("Namespace"), name)

	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.cache[key.String()]
}

func (c *nsCache) Remove(key storage.Path) {
	c.RemoveNamespace(key.String())
}

func (c *nsCache) RemoveNamespace(key string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.cache, key)
}

func (m *Matcher) Match(review interface{}) (bool, error) {
	if m.match == nil {
		// No-op if Match unspecified.
		return true, nil
	}

	gkReq, ok := review.(*gkReview)
	if !ok {
		return false, fmt.Errorf("%w: expect %T, got %T", ErrReviewFormat, &gkReview{}, review)
	}

	obj, oldObj, ns, err := gkReviewToObject(gkReq)
	if err != nil {
		return false, err
	}

	if (ns == nil) && (gkReq.Namespace != "") {
		ns = m.cache.GetNamespace(gkReq.Namespace)
	}

	return matchAny(m, ns, obj, oldObj)
}

func matchAny(m *Matcher, ns *corev1.Namespace, objs ...*unstructured.Unstructured) (bool, error) {
	nilObj := 0
	for _, obj := range objs {
		if obj == nil || obj.Object == nil {
			nilObj++
			continue
		}

		matched, err := match.Matches(m.match, obj, ns)
		if err != nil {
			return false, fmt.Errorf("%w: %v", ErrMatching, err)
		}

		if matched {
			return true, nil
		}
	}

	if nilObj == len(objs) {
		return false, fmt.Errorf("%w: neither object nor old object are defined", ErrRequestObject)
	}
	return false, nil
}

func gkReviewToObject(req *gkReview) (*unstructured.Unstructured, *unstructured.Unstructured, *corev1.Namespace, error) {
	var obj *unstructured.Unstructured
	if req.Object.Raw != nil {
		obj = &unstructured.Unstructured{}
		err := obj.UnmarshalJSON(req.Object.Raw)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%w: failed to unmarshal gkReview object %s", ErrRequestObject, string(req.Object.Raw))
		}
	}

	var oldObj *unstructured.Unstructured
	if req.OldObject.Raw != nil {
		oldObj = &unstructured.Unstructured{}
		err := oldObj.UnmarshalJSON(req.OldObject.Raw)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%w: failed to unmarshal gkReview oldObject %s", ErrRequestObject, string(req.OldObject.Raw))
		}
	}

	return obj, oldObj, req.Unstable.Namespace, nil
}

var (
	_ constraints.Matcher = &Matcher{}
	_ handler.Cache       = &nsCache{}
	_ handler.Cacher      = &K8sValidationTarget{}
)
