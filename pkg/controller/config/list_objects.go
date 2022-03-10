package config

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (r *ReconcileConfig) ListObjects() []*unstructured.Unstructured {
	var objs []*unstructured.Unstructured
	r.watched.DoForEach(func(gvk schema.GroupVersionKind) {
		objs = append(objs, r.listObjects(gvk)...)
	})
	return objs
}

func (r *ReconcileConfig) listObjects(gvk schema.GroupVersionKind) []*unstructured.Unstructured {
	list := &unstructured.UnstructuredList{
		Object: map[string]interface{}{},
		Items:  []unstructured.Unstructured{},
	}

	gvk.Kind = gvk.Kind + "List"
	list.SetGroupVersionKind(gvk)

	_ = r.reader.List(context.TODO(), list)
	return nil
}
