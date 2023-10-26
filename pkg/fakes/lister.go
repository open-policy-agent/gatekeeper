package fakes

import (
	"context"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	syncsetv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/syncset/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var scheme *runtime.Scheme

func init() {
	scheme = runtime.NewScheme()
	if err := v1beta1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := syncsetv1alpha1.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

// Fake lister to use for readiness testing.
type TestLister struct {
	templatesToList []*templates.ConstraintTemplate
	syncSetsToList  []*syncsetv1alpha1.SyncSet
}

type LOpt func(tl *TestLister)

func WithConstraintTemplates(t []*templates.ConstraintTemplate) LOpt {
	return func(tl *TestLister) {
		tl.templatesToList = t
	}
}

func WithSyncSets(s []*syncsetv1alpha1.SyncSet) LOpt {
	return func(tl *TestLister) {
		tl.syncSetsToList = s
	}
}

func NewTestLister(opts ...LOpt) *TestLister {
	tl := &TestLister{}
	for _, o := range opts {
		o(tl)
	}

	return tl
}

// List implements readiness.Lister.
func (tl *TestLister) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	// failures will be swallowed by readiness.retryAll
	switch list := list.(type) {
	case *v1beta1.ConstraintTemplateList:
		if len(tl.templatesToList) == 0 {
			return nil
		}

		items := []v1beta1.ConstraintTemplate{}
		for _, t := range tl.templatesToList {
			i := v1beta1.ConstraintTemplate{}
			if err := scheme.Convert(t, &i, nil); err != nil {
				return err
			}
			items = append(items, i)
		}
		list.Items = items

	case *syncsetv1alpha1.SyncSetList:
		if len(tl.syncSetsToList) == 0 {
			return nil
		}

		items := []syncsetv1alpha1.SyncSet{}
		for _, t := range tl.syncSetsToList {
			i := syncsetv1alpha1.SyncSet{}
			if err := scheme.Convert(t, &i, nil); err != nil {
				return err
			}
			items = append(items, i)
		}
		list.Items = items

	case *unstructured.UnstructuredList:
		if len(tl.syncSetsToList) == 0 {
			return nil
		}

		list.Items = []unstructured.Unstructured{{}} // return one element per list for unstructured.
	default:
		return nil
	}

	return nil
}
