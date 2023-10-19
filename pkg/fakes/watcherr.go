package fakes

import (
	"fmt"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ watch.WatchesError = &FakeErr{}

type FakeErr struct {
	gvks       []schema.GroupVersionKind
	err        error
	generalErr bool
}

func (e *FakeErr) Error() string {
	return fmt.Sprintf("failing gvks: %v", e.gvks)
}

func (e *FakeErr) FailingGVKs() []schema.GroupVersionKind {
	return e.gvks
}

func (e *FakeErr) HasGeneralErr() bool {
	return e.generalErr
}

type FOpt func(f *FakeErr)

func WithErr(e error) FOpt {
	return func(f *FakeErr) {
		f.err = e
	}
}

func WithGVKs(gvks []schema.GroupVersionKind) FOpt {
	return func(f *FakeErr) {
		f.gvks = gvks
	}
}

func GeneralErr() FOpt {
	return func(f *FakeErr) {
		f.generalErr = true
	}
}

func WatchesErr(opts ...FOpt) error {
	result := &FakeErr{}

	for _, opt := range opts {
		opt(result)
	}

	return result
}
