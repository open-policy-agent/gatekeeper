package main

import (
	"net/http"

	"github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider/pkg/dummy"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("dummy-provider")

func main() {
	// Create a new cache
	cache := dummy.NewCache()

	log.Info("starting server...")
	http.HandleFunc("/validate", cache.Validate)

	if err := http.ListenAndServe(":8090", nil); err != nil {
		panic(err)
	}
}
