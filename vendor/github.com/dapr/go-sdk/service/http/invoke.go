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
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/dapr/go-sdk/service/common"
)

// AddServiceInvocationHandler appends provided service invocation handler with its route to the service.
func (s *Server) AddServiceInvocationHandler(route string, fn common.ServiceInvocationHandler) error {
	if route == "" || route == "/" {
		return fmt.Errorf("service route required")
	}

	if fn == nil {
		return fmt.Errorf("invocation handler required")
	}

	if !strings.HasPrefix(route, "/") {
		route = fmt.Sprintf("/%s", route)
	}

	s.mux.Handle(route, optionsHandler(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if s.authToken != "" {
				token := r.Header.Get(common.APITokenKey)
				if token == "" || token != s.authToken {
					http.Error(w, "authentication failed.", http.StatusNonAuthoritativeInfo)
					return
				}
			}
			// capture http args
			e := &common.InvocationEvent{
				Verb:        r.Method,
				QueryString: r.URL.RawQuery,
				ContentType: r.Header.Get("Content-type"),
			}

			// check for post with no data
			if r.ContentLength > 0 {
				content, err := ioutil.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				e.Data = content
			}

			// execute handler
			o, err := fn(r.Context(), e)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// write to response if handler returned data
			if o != nil && o.Data != nil {
				if o.ContentType != "" {
					w.Header().Set("Content-type", o.ContentType)
				}
				if _, err := w.Write(o.Data); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		})))

	return nil
}
