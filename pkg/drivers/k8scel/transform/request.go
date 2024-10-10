package transform

import (
	"encoding/json"
	"errors"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	auditinternal "k8s.io/apiserver/pkg/apis/audit"
	"k8s.io/apiserver/pkg/authentication/user"
)

func RequestToVersionedAttributes(request *admissionv1.AdmissionRequest) (*admission.VersionedAttributes, error) {
	wrapped, err := NewWrapper(request)
	if err != nil {
		return nil, err
	}
	return &admission.VersionedAttributes{
		Attributes:         wrapped,
		VersionedKind:      wrapped.GetKind(),
		VersionedOldObject: wrapped.GetOldObject(),
		VersionedObject:    wrapped.GetObject(),
	}, nil
}

// FRICTION this wrapper class is excessive. Validator code should define an interface that only requires the methods it needs.
type RequestWrapper struct {
	ar               *admissionv1.AdmissionRequest
	object           runtime.Object
	oldObject        runtime.Object
	operationOptions runtime.Object
}

func NewWrapper(req *admissionv1.AdmissionRequest) (*RequestWrapper, error) {
	var object runtime.Object
	if len(req.Object.Raw) != 0 {
		object = &unstructured.Unstructured{}
		if err := json.Unmarshal(req.Object.Raw, object); err != nil {
			return nil, fmt.Errorf("%w: could not unmarshal object", err)
		}
	}

	var oldObject runtime.Object
	if len(req.OldObject.Raw) != 0 {
		oldObject = &unstructured.Unstructured{}
		if err := json.Unmarshal(req.OldObject.Raw, oldObject); err != nil {
			return nil, fmt.Errorf("%w: could not unmarshal old object", err)
		}
	}

	// this may be unnecessary, since GetOptions() may not be used by downstream
	// code, but is better than doing this lazily and needing to panic if GetOptions()
	// fails.
	var options runtime.Object
	if len(req.Options.Raw) != 0 {
		options = &unstructured.Unstructured{}
		if err := json.Unmarshal(req.Options.Raw, options); err != nil {
			return nil, fmt.Errorf("%w: could not unmarshal options", err)
		}
	}
	return &RequestWrapper{
		ar:               req,
		object:           object,
		oldObject:        oldObject,
		operationOptions: options,
	}, nil
}

func (w *RequestWrapper) GetName() string {
	return w.ar.Name
}

func (w *RequestWrapper) GetNamespace() string {
	return w.ar.Namespace
}

func (w *RequestWrapper) GetResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    w.ar.Resource.Group,
		Version:  w.ar.Resource.Version,
		Resource: w.ar.Resource.Resource,
	}
}

func (w *RequestWrapper) GetSubresource() string {
	return w.ar.SubResource
}

var opMap = map[admissionv1.Operation]admission.Operation{
	admissionv1.Create:  admission.Create,
	admissionv1.Update:  admission.Update,
	admissionv1.Delete:  admission.Delete,
	admissionv1.Connect: admission.Connect,
}

func (w *RequestWrapper) GetOperation() admission.Operation {
	return opMap[w.ar.Operation]
}

func (w *RequestWrapper) GetOperationOptions() runtime.Object {
	return w.operationOptions
}

func (w *RequestWrapper) IsDryRun() bool {
	if w.ar.DryRun == nil {
		return false
	}
	return *w.ar.DryRun
}

func (w *RequestWrapper) GetObject() runtime.Object {
	return w.object
}

func (w *RequestWrapper) GetOldObject() runtime.Object {
	return w.oldObject
}

func (w *RequestWrapper) GetKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   w.ar.Kind.Group,
		Version: w.ar.Kind.Version,
		Kind:    w.ar.Kind.Kind,
	}
}

func (w *RequestWrapper) GetUserInfo() user.Info {
	extra := map[string][]string{}
	for k := range w.ar.UserInfo.Extra {
		vals := make([]string, len(w.ar.UserInfo.Extra[k]))
		copy(vals, w.ar.UserInfo.Extra[k])
		extra[k] = vals
	}

	return &user.DefaultInfo{
		Name:   w.ar.UserInfo.Username,
		UID:    w.ar.UserInfo.UID,
		Groups: w.ar.UserInfo.Groups,
		Extra:  extra,
	}
}

func (w *RequestWrapper) AddAnnotation(_, _ string) error {
	return errors.New("AddAnnotation not implemented")
}

func (w *RequestWrapper) AddAnnotationWithLevel(_, _ string, _ auditinternal.Level) error {
	return errors.New("AddAnnotationWithLevel not implemented")
}

func (w *RequestWrapper) GetReinvocationContext() admission.ReinvocationContext {
	return nil
}
