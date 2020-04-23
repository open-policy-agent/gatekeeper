package util

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// UnpackRequest unpacks the GVK from a reconcile.Request and returns the separated components.
// GVK is encoded as "Kind.Version.Group".
// Requests are expected to be in the format: {Name: "gvk:EncodedGVK:Name", Namespace: Namespace}
func UnpackRequest(r reconcile.Request) (schema.GroupVersionKind, reconcile.Request, error) {
	fields := strings.SplitN(r.Name, ":", 3)
	if len(fields) != 3 || fields[0] != "gvk" {
		return schema.GroupVersionKind{}, reconcile.Request{}, fmt.Errorf("invalid packed name: %s", r.Name)
	}
	gvk, _ := schema.ParseKindArg(fields[1])
	if gvk == nil {
		return schema.GroupVersionKind{}, reconcile.Request{}, fmt.Errorf("unable to parse gvk: %s", fields[1])
	}

	return *gvk, reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      fields[2],
		Namespace: r.Namespace,
	}}, nil
}

// EventPacker implements handler.Mapper to pack incoming events into requests
// with embedded GVK info. These requests can be unpacked in Reconcile() using UnpackRequest().
type EventPacker struct{}

// Map maps an event into a reconcile.Request with embedded GVK information. Must
// be unpacked with UnpackRequest() before use.
func (EventPacker) Map(obj handler.MapObject) []reconcile.Request {
	if obj.Object == nil || obj.Meta == nil {
		return nil
	}
	gvk := obj.Object.GetObjectKind().GroupVersionKind()
	if gvk.Version == "" {
		gvk.Version = "v1"
	}
	encodedGVK := fmt.Sprintf("%s.%s.%s", gvk.Kind, gvk.Version, gvk.Group)

	packed := fmt.Sprintf("gvk:%s:%s", encodedGVK, obj.Meta.GetName())
	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Namespace: obj.Meta.GetNamespace(),
				Name:      packed,
			}},
	}
}
