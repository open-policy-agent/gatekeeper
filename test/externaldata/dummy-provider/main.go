package main

import (
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider/pkg/dummy"
	"go.uber.org/zap"
)

var log logr.Logger

func main() {
	// Create a new cache
	cache := dummy.NewCache()

	zapLog, err := zap.NewDevelopment()
	if err != nil {
		panic(fmt.Sprintf("unable to initialize logger: %v", err))
	}
	log = zapr.NewLogger(zapLog)
	log.WithName("dummy-provider")

	log.Info("starting server...")
	http.HandleFunc("/validate", cache.Validate)

	if err = http.ListenAndServe(":8090", nil); err != nil {
		panic(err)
	}
}
