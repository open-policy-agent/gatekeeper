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

package readiness_test

import (
	"testing"

	"github.com/onsi/gomega"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

func Test_Tracker(t *testing.T) {
	g := gomega.NewWithT(t)
	configStore := cache.NewStore(cache.MetaNamespaceKeyFunc)
	err := configStore.Add(&configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "this-should-be-ignored",
			Namespace: "gatekeeper-system",
		},
		Spec: configv1alpha1.ConfigSpec{
			Sync: configv1alpha1.Sync{
				SyncOnly: []configv1alpha1.SyncOnlyEntry{
					{Group: "apps", Version: "v1", Kind: "Deployment"},
					{Group: "apps", Version: "v1", Kind: "ReplicaSet"},
				},
			},
		},
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	err = configStore.Add(&configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: "gatekeeper-system",
		},
		Spec: configv1alpha1.ConfigSpec{
			Sync: configv1alpha1.Sync{
				SyncOnly: []configv1alpha1.SyncOnlyEntry{
					{Group: "", Version: "v1", Kind: "Namespace"},
					{Group: "", Version: "v1", Kind: "Pod"},
				},
			},
		},
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	err = configStore.Add(&configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: "this-is-a-fake-namespace",
		},
		Spec: configv1alpha1.ConfigSpec{
			Sync: configv1alpha1.Sync{
				SyncOnly: []configv1alpha1.SyncOnlyEntry{
					{Group: "", Version: "v1", Kind: "Event"},
					{Group: "", Version: "v1", Kind: "Secret"},
				},
			},
		},
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	namespaceStore := cache.NewStore(cache.MetaNamespaceKeyFunc)
	err = namespaceStore.Add(&metav1.ObjectMeta{
		Name:      "a",
		Namespace: "",
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	err = namespaceStore.Add(&metav1.ObjectMeta{
		Name:      "b",
		Namespace: "",
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	err = namespaceStore.Add(&metav1.ObjectMeta{
		Name:      "c",
		Namespace: "",
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	namespaceInformer := &mockInformer{mockStore: &namespaceStore}

	podStore := cache.NewStore(cache.MetaNamespaceKeyFunc)
	err = podStore.Add(&metav1.ObjectMeta{
		Name:      "1",
		Namespace: "x",
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	err = podStore.Add(&metav1.ObjectMeta{
		Name:      "2",
		Namespace: "y",
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	err = podStore.Add(&metav1.ObjectMeta{
		Name:      "3",
		Namespace: "z",
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	err = podStore.Add(&metav1.ObjectMeta{
		Name:      "4",
		Namespace: "q",
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	podInformer := &mockInformer{mockStore: &podStore}

	templateStore := cache.NewStore(cache.MetaNamespaceKeyFunc)
	err = templateStore.Add(&v1beta1.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "require-foo"},
		Spec: v1beta1.ConstraintTemplateSpec{
			CRD: v1beta1.CRD{
				Spec: v1beta1.CRDSpec{
					Names: v1beta1.Names{
						Kind: "RequireFoo",
					},
				},
			},
		},
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	err = templateStore.Add(&v1beta1.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "require-bar"},
		Spec: v1beta1.ConstraintTemplateSpec{
			CRD: v1beta1.CRD{
				Spec: v1beta1.CRDSpec{
					Names: v1beta1.Names{
						Kind: "RequireBar",
					},
				},
			},
		},
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	fooConstraintStore := cache.NewStore(cache.MetaNamespaceKeyFunc)
	err = fooConstraintStore.Add(&metav1.ObjectMeta{
		Name:      "ThisIsAFooConstraint",
		Namespace: "Foo",
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	fooConstraintInformer := &mockInformer{mockStore: &fooConstraintStore}

	barConstraintStore := cache.NewStore(cache.MetaNamespaceKeyFunc)
	err = barConstraintStore.Add(&metav1.ObjectMeta{
		Name:      "ThisIsABarConstraint",
		Namespace: "Bar",
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	barConstraintInformer := &mockInformer{mockStore: &barConstraintStore}

	mockCache := &mockCache{
		informers: map[schema.GroupVersionKind]*mockInformer{
			schema.GroupVersionKind{
				Group:   "config.gatekeeper.sh",
				Kind:    "Config",
				Version: "v1alpha1",
			}: &mockInformer{mockStore: &configStore},
			schema.GroupVersionKind{
				Group:   "",
				Kind:    "Namespace",
				Version: "v1",
			}: namespaceInformer,
			schema.GroupVersionKind{
				Group:   "",
				Kind:    "Pod",
				Version: "v1",
			}: podInformer,
			schema.GroupVersionKind{
				Group:   "templates.gatekeeper.sh",
				Kind:    "ConstraintTemplate",
				Version: "v1beta1",
			}: &mockInformer{mockStore: &templateStore},
			schema.GroupVersionKind{
				Group:   "constraints.gatekeeper.sh",
				Kind:    "RequireFoo",
				Version: "v1beta1",
			}: fooConstraintInformer,
			schema.GroupVersionKind{
				Group:   "constraints.gatekeeper.sh",
				Kind:    "RequireBar",
				Version: "v1beta1",
			}: barConstraintInformer,
		},
	}

	mockManager := &mockManager{mockCache: mockCache}
	tracker := readiness.NewTracker(mockManager)
	// ensure satisfied returns false before `Start` is called
	g.Expect(tracker.Satisfied()).Should(gomega.Equal(false))
	err = tracker.Start(make(chan struct{}))
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	g.Expect(tracker.Satisfied()).Should(gomega.Equal(false))

	expected := [][]string{
		[]string{"config.gatekeeper.sh", "Config", "config", "gatekeeper-system"},
		[]string{"this", "is", "extra", "observation"},
		[]string{"constraints.gatekeeper.sh", "RequireBar", "ThisIsABarConstraint", "Bar"},
		[]string{"constraints.gatekeeper.sh", "RequireFoo", "ThisIsAFooConstraint", "Foo"},
		[]string{"templates.gatekeeper.sh", "ConstraintTemplate", "require-bar", ""},
		[]string{"templates.gatekeeper.sh", "ConstraintTemplate", "require-foo", ""},
		[]string{"", "Namespace", "a", ""},
		[]string{"", "Namespace", "b", ""},
		[]string{"", "Namespace", "c", ""},
		[]string{"", "Pod", "1", "x"},
		[]string{"", "Pod", "2", "y"},
		[]string{"", "Pod", "3", "z"},
		[]string{"", "Pod", "4", "q"},
	}
	for _, e := range expected {
		g.Expect(tracker.Satisfied()).Should(gomega.Equal(false))
		tracker.Observe(e[0], e[1], e[2], e[3])
	}
	g.Expect(tracker.Satisfied()).Should(gomega.Equal(true))
	// assert memory is free after first osberved success
	g.Expect(tracker.Expecting).Should(gomega.Equal(map[string]bool{}))
	g.Expect(tracker.Seen).Should(gomega.Equal(map[string]bool{}))
	g.Expect(tracker.Satisfied()).Should(gomega.Equal(true))

	// assert wait for sync calls were made
	g.Expect(mockCache.waitForCacheSyncCalled).Should(gomega.Equal(true))
	g.Expect(podInformer.hasSyncedCalled).Should(gomega.Equal(true))
	g.Expect(namespaceInformer.hasSyncedCalled).Should(gomega.Equal(true))
	g.Expect(fooConstraintInformer.hasSyncedCalled).Should(gomega.Equal(true))
	g.Expect(barConstraintInformer.hasSyncedCalled).Should(gomega.Equal(true))
}

func Test_Tracker_CheckSatisfied(t *testing.T) {
	g := gomega.NewWithT(t)
	tracker := readiness.NewTracker(nil)

	err := tracker.CheckSatisfied(nil)
	g.Expect(err).Should(gomega.HaveOccurred())

	tracker.Ready = true

	err = tracker.CheckSatisfied(nil)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
}
