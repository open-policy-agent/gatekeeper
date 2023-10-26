package fakes

import (
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FOpt func(f *watch.ErrorList)

func WithErr(e error) FOpt {
	return func(f *watch.ErrorList) {
		f.Add(e)
	}
}

func WithGVKsErr(gvks []schema.GroupVersionKind, e error) FOpt {
	return func(f *watch.ErrorList) {
		for _, gvk := range gvks {
			f.AddGVKErr(gvk, e)
		}
	}
}

func WatchesErr(opts ...FOpt) error {
	result := watch.NewErrorList()

	for _, opt := range opts {
		opt(result)
	}

	return result
}
