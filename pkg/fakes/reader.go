package fakes

import (
	"context"
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SpyReader struct {
	client.Reader
	ListFunc func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

func (r SpyReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if r.ListFunc != nil {
		return r.ListFunc(ctx, list, opts...)
	}
	return r.Reader.List(ctx, list, opts...)
}

// FailureInjector can be used in combination with the SpyReader to simulate transient
// failures for network calls.
type FailureInjector struct {
	mu       sync.Mutex
	failures map[string]int // registers GVK.Kind and how many times to fail
}

func (f *FailureInjector) SetFailures(kind string, failures int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failures[kind] = failures
}

// CheckFailures looks at the count of failures and returns true
// if there are still failures for the kind to consume, false otherwise.
func (f *FailureInjector) CheckFailures(kind string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, ok := f.failures[kind]
	if !ok {
		return false
	}

	if v == 0 {
		return false
	}

	f.failures[kind] = v - 1

	return true
}

func NewFailureInjector() *FailureInjector {
	return &FailureInjector{
		failures: make(map[string]int),
	}
}
