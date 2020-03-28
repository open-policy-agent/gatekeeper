/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package readiness

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type objKey struct {
	gvk            schema.GroupVersionKind
	namespacedName types.NamespacedName
}

func (o objKey) GetNamespace() string {
	return o.namespacedName.Namespace
}

func (o objKey) SetNamespace(namespace string) {
	panic("implement me")
}

func (o objKey) GetName() string {
	return o.namespacedName.Name
}

func (o objKey) SetName(name string) {
	panic("implement me")
}

func (o objKey) GetGenerateName() string {
	panic("implement me")
}

func (o objKey) SetGenerateName(name string) {
	panic("implement me")
}

func (o objKey) GetUID() types.UID {
	panic("implement me")
}

func (o objKey) SetUID(uid types.UID) {
	panic("implement me")
}

func (o objKey) GetResourceVersion() string {
	panic("implement me")
}

func (o objKey) SetResourceVersion(version string) {
	panic("implement me")
}

func (o objKey) GetGeneration() int64 {
	panic("implement me")
}

func (o objKey) SetGeneration(generation int64) {
	panic("implement me")
}

func (o objKey) GetSelfLink() string {
	panic("implement me")
}

func (o objKey) SetSelfLink(selfLink string) {
	panic("implement me")
}

func (o objKey) GetCreationTimestamp() metav1.Time {
	panic("implement me")
}

func (o objKey) SetCreationTimestamp(timestamp metav1.Time) {
	panic("implement me")
}

func (o objKey) GetDeletionTimestamp() *metav1.Time {
	panic("implement me")
}

func (o objKey) SetDeletionTimestamp(timestamp *metav1.Time) {
	panic("implement me")
}

func (o objKey) GetDeletionGracePeriodSeconds() *int64 {
	panic("implement me")
}

func (o objKey) SetDeletionGracePeriodSeconds(i *int64) {
	panic("implement me")
}

func (o objKey) GetLabels() map[string]string {
	panic("implement me")
}

func (o objKey) SetLabels(labels map[string]string) {
	panic("implement me")
}

func (o objKey) GetAnnotations() map[string]string {
	panic("implement me")
}

func (o objKey) SetAnnotations(annotations map[string]string) {
	panic("implement me")
}

func (o objKey) GetFinalizers() []string {
	panic("implement me")
}

func (o objKey) SetFinalizers(finalizers []string) {
	panic("implement me")
}

func (o objKey) GetOwnerReferences() []metav1.OwnerReference {
	panic("implement me")
}

func (o objKey) SetOwnerReferences(references []metav1.OwnerReference) {
	panic("implement me")
}

func (o objKey) GetClusterName() string {
	panic("implement me")
}

func (o objKey) SetClusterName(clusterName string) {
	panic("implement me")
}

func (o objKey) GetManagedFields() []metav1.ManagedFieldsEntry {
	panic("implement me")
}

func (o objKey) SetManagedFields(managedFields []metav1.ManagedFieldsEntry) {
	panic("implement me")
}

func (o objKey) SetGroupVersionKind(kind schema.GroupVersionKind) {
	panic("not implemented")
}

func (o objKey) GroupVersionKind() schema.GroupVersionKind {
	return o.gvk
}

func (o objKey) GetObjectKind() schema.ObjectKind {
	return o
}

func (o objKey) DeepCopyObject() runtime.Object {
	return o
}

type objSet map[objKey]struct{}
