/*
Copyright 2021 The Kubernetes Authors.

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

package authentication

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	authenticationv1 "k8s.io/api/authentication/v1"
	authenticationv1beta1 "k8s.io/api/authentication/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

var authenticationScheme = runtime.NewScheme()
var authenticationCodecs = serializer.NewCodecFactory(authenticationScheme)

func init() {
	utilruntime.Must(authenticationv1.AddToScheme(authenticationScheme))
	utilruntime.Must(authenticationv1beta1.AddToScheme(authenticationScheme))
}

var _ http.Handler = &Webhook{}

func (wh *Webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body []byte
	var err error
	ctx := r.Context()
	if wh.WithContextFunc != nil {
		ctx = wh.WithContextFunc(ctx, r)
	}

	var reviewResponse Response
	if r.Body == nil {
		err = errors.New("request body is empty")
		wh.getLogger(nil).Error(err, "bad request")
		reviewResponse = Errored(err)
		wh.writeResponse(w, reviewResponse)
		return
	}

	defer r.Body.Close()
	if body, err = io.ReadAll(r.Body); err != nil {
		wh.getLogger(nil).Error(err, "unable to read the body from the incoming request")
		reviewResponse = Errored(err)
		wh.writeResponse(w, reviewResponse)
		return
	}

	// verify the content type is accurate
	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		err = fmt.Errorf("contentType=%s, expected application/json", contentType)
		wh.getLogger(nil).Error(err, "unable to process a request with unknown content type")
		reviewResponse = Errored(err)
		wh.writeResponse(w, reviewResponse)
		return
	}

	// Both v1 and v1beta1 TokenReview types are exactly the same, so the v1beta1 type can
	// be decoded into the v1 type. The v1beta1 api is deprecated as of 1.19 and will be
	// removed in authenticationv1.22. However the runtime codec's decoder guesses which type to
	// decode into by type name if an Object's TypeMeta isn't set. By setting TypeMeta of an
	// unregistered type to the v1 GVK, the decoder will coerce a v1beta1 TokenReview to authenticationv1.
	// The actual TokenReview GVK will be used to write a typed response in case the
	// webhook config permits multiple versions, otherwise this response will fail.
	req := Request{}
	ar := unversionedTokenReview{}
	// avoid an extra copy
	ar.TokenReview = &req.TokenReview
	ar.SetGroupVersionKind(authenticationv1.SchemeGroupVersion.WithKind("TokenReview"))
	_, actualTokRevGVK, err := authenticationCodecs.UniversalDeserializer().Decode(body, nil, &ar)
	if err != nil {
		wh.getLogger(nil).Error(err, "unable to decode the request")
		reviewResponse = Errored(err)
		wh.writeResponse(w, reviewResponse)
		return
	}
	wh.getLogger(&req).V(4).Info("received request")

	if req.Spec.Token == "" {
		err = errors.New("token is empty")
		wh.getLogger(&req).Error(err, "bad request")
		reviewResponse = Errored(err)
		wh.writeResponse(w, reviewResponse)
		return
	}

	reviewResponse = wh.Handle(ctx, req)
	wh.writeResponseTyped(w, reviewResponse, actualTokRevGVK)
}

// writeResponse writes response to w generically, i.e. without encoding GVK information.
func (wh *Webhook) writeResponse(w io.Writer, response Response) {
	wh.writeTokenResponse(w, response.TokenReview)
}

// writeResponseTyped writes response to w with GVK set to tokRevGVK, which is necessary
// if multiple TokenReview versions are permitted by the webhook.
func (wh *Webhook) writeResponseTyped(w io.Writer, response Response, tokRevGVK *schema.GroupVersionKind) {
	ar := response.TokenReview

	// Default to a v1 TokenReview, otherwise the API server may not recognize the request
	// if multiple TokenReview versions are permitted by the webhook config.
	if tokRevGVK == nil || *tokRevGVK == (schema.GroupVersionKind{}) {
		ar.SetGroupVersionKind(authenticationv1.SchemeGroupVersion.WithKind("TokenReview"))
	} else {
		ar.SetGroupVersionKind(*tokRevGVK)
	}
	wh.writeTokenResponse(w, ar)
}

// writeTokenResponse writes ar to w.
func (wh *Webhook) writeTokenResponse(w io.Writer, ar authenticationv1.TokenReview) {
	if err := json.NewEncoder(w).Encode(ar); err != nil {
		wh.getLogger(nil).Error(err, "unable to encode the response")
		wh.writeResponse(w, Errored(err))
	}
	res := ar
	wh.getLogger(nil).V(4).Info("wrote response", "requestID", res.UID, "authenticated", res.Status.Authenticated)
}

// unversionedTokenReview is used to decode both v1 and v1beta1 TokenReview types.
type unversionedTokenReview struct {
	*authenticationv1.TokenReview
}

var _ runtime.Object = &unversionedTokenReview{}
