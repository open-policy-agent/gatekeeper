/*
Copyright 2021 The Dapr Authors
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

package http

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"

	"github.com/dapr/go-sdk/actor"
	"github.com/dapr/go-sdk/actor/config"
	"github.com/dapr/go-sdk/actor/runtime"
	"github.com/dapr/go-sdk/service/common"
	"github.com/dapr/go-sdk/service/internal"
)

// NewService creates new Service.
func NewService(address string) common.Service {
	return newServer(address, nil)
}

// NewServiceWithMux creates new Service with existing http mux.
func NewServiceWithMux(address string, mux *mux.Router) common.Service {
	return newServer(address, mux)
}

func newServer(address string, router *mux.Router) *Server {
	if router == nil {
		router = mux.NewRouter()
	}
	return &Server{
		address: address,
		httpServer: &http.Server{
			Addr:    address,
			Handler: router,
		},
		mux:            router,
		topicRegistrar: make(internal.TopicRegistrar),
		authToken:      os.Getenv(common.AppAPITokenEnvVar),
	}
}

// Server is the HTTP server wrapping mux many Dapr helpers.
type Server struct {
	address        string
	mux            *mux.Router
	httpServer     *http.Server
	topicRegistrar internal.TopicRegistrar
	authToken      string
}

func (s *Server) RegisterActorImplFactory(f actor.Factory, opts ...config.Option) {
	runtime.GetActorRuntimeInstance().RegisterActorFactory(f, opts...)
}

// Start starts the HTTP handler. Blocks while serving.
func (s *Server) Start() error {
	s.registerBaseHandler()
	return s.httpServer.ListenAndServe()
}

// Stop stops previously started HTTP service with a five second timeout.
func (s *Server) Stop() error {
	ctxShutDown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.httpServer.Shutdown(ctxShutDown)
}

func (s *Server) GracefulStop() error {
	return s.Stop()
}

func setOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "authorization, origin, content-type, accept")
	w.Header().Set("Allow", "POST,OPTIONS")
}

func optionsHandler(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			setOptions(w, r)
		} else {
			h.ServeHTTP(w, r)
		}
	}
}
