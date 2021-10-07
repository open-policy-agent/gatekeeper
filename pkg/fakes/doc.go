// Package fakes defines methods for instantiating objects which act like
// resources on a Kubernetes cluster, but are not intended to actually be
// instantiated on a real, production cluster.
//
// The primary purpose of these objects is to aid writing tests, but that is not
// this package's only purpose. These objects are also used in debugging logic
// which is compiled in to production code.
package fakes
