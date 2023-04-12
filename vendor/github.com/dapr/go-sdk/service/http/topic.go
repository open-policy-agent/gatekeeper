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
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"github.com/gorilla/mux"

	actorErr "github.com/dapr/go-sdk/actor/error"
	"github.com/dapr/go-sdk/actor/runtime"
	"github.com/dapr/go-sdk/service/common"
	"github.com/dapr/go-sdk/service/internal"
)

const (
	// PubSubHandlerSuccessStatusCode is the successful ack code for pubsub event appcallback response.
	PubSubHandlerSuccessStatusCode int = http.StatusOK

	// PubSubHandlerRetryStatusCode is the error response code (nack) pubsub event appcallback response.
	PubSubHandlerRetryStatusCode int = http.StatusInternalServerError

	// PubSubHandlerDropStatusCode is the pubsub event appcallback response code indicating that Dapr should drop that message.
	PubSubHandlerDropStatusCode int = http.StatusSeeOther
)

// topicEventJSON is identical to `common.TopicEvent`
// except for it treats `data` as a json.RawMessage so it can
// be used as bytes or interface{}.
type topicEventJSON struct {
	// ID identifies the event.
	ID string `json:"id"`
	// The version of the CloudEvents specification.
	SpecVersion string `json:"specversion"`
	// The type of event related to the originating occurrence.
	Type string `json:"type"`
	// Source identifies the context in which an event happened.
	Source string `json:"source"`
	// The content type of data value.
	DataContentType string `json:"datacontenttype"`
	// The content of the event.
	// Note, this is why the gRPC and HTTP implementations need separate structs for cloud events.
	Data json.RawMessage `json:"data"`
	// The base64 encoding content of the event.
	// Note, this is processing rawPayload and binary content types.
	DataBase64 string `json:"data_base64,omitempty"`
	// Cloud event subject
	Subject string `json:"subject"`
	// The pubsub topic which publisher sent to.
	Topic string `json:"topic"`
	// PubsubName is name of the pub/sub this message came from
	PubsubName string `json:"pubsubname"`
}

func (s *Server) registerBaseHandler() {
	// register subscribe handler
	f := func(w http.ResponseWriter, r *http.Request) {
		subs := make([]*internal.TopicSubscription, 0, len(s.topicRegistrar))
		for _, s := range s.topicRegistrar {
			subs = append(subs, s.Subscription)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(subs); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.mux.HandleFunc("/dapr/subscribe", f)

	// register health check handler
	fHealth := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
	s.mux.HandleFunc("/healthz", fHealth).Methods(http.MethodGet)

	// register actor config handler
	fRegister := func(w http.ResponseWriter, r *http.Request) {
		data, err := runtime.GetActorRuntimeInstance().GetJSONSerializedConfig()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err = w.Write(data); err != nil {
			return
		}
	}
	s.mux.HandleFunc("/dapr/config", fRegister).Methods(http.MethodGet)

	// register actor method invoke handler
	fInvoke := func(w http.ResponseWriter, r *http.Request) {
		varsMap := mux.Vars(r)
		actorType := varsMap["actorType"]
		actorID := varsMap["actorId"]
		methodName := varsMap["methodName"]
		reqData, _ := ioutil.ReadAll(r.Body)
		rspData, err := runtime.GetActorRuntimeInstance().InvokeActorMethod(actorType, actorID, methodName, reqData)
		if err == actorErr.ErrActorTypeNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != actorErr.Success {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rspData)
	}
	s.mux.HandleFunc("/actors/{actorType}/{actorId}/method/{methodName}", fInvoke).Methods(http.MethodPut)

	// register deactivate actor handler
	fDelete := func(w http.ResponseWriter, r *http.Request) {
		varsMap := mux.Vars(r)
		actorType := varsMap["actorType"]
		actorID := varsMap["actorId"]
		err := runtime.GetActorRuntimeInstance().Deactivate(actorType, actorID)
		if err == actorErr.ErrActorTypeNotFound || err == actorErr.ErrActorIDNotFound {
			w.WriteHeader(http.StatusNotFound)
		}
		if err != actorErr.Success {
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.WriteHeader(http.StatusOK)
	}
	s.mux.HandleFunc("/actors/{actorType}/{actorId}", fDelete).Methods(http.MethodDelete)

	// register actor reminder invoke handler
	fReminder := func(w http.ResponseWriter, r *http.Request) {
		varsMap := mux.Vars(r)
		actorType := varsMap["actorType"]
		actorID := varsMap["actorId"]
		reminderName := varsMap["reminderName"]
		reqData, _ := ioutil.ReadAll(r.Body)
		err := runtime.GetActorRuntimeInstance().InvokeReminder(actorType, actorID, reminderName, reqData)
		if err == actorErr.ErrActorTypeNotFound {
			w.WriteHeader(http.StatusNotFound)
		}
		if err != actorErr.Success {
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.WriteHeader(http.StatusOK)
	}
	s.mux.HandleFunc("/actors/{actorType}/{actorId}/method/remind/{reminderName}", fReminder).Methods(http.MethodPut)

	// register actor timer invoke handler
	fTimer := func(w http.ResponseWriter, r *http.Request) {
		varsMap := mux.Vars(r)
		actorType := varsMap["actorType"]
		actorID := varsMap["actorId"]
		timerName := varsMap["timerName"]
		reqData, _ := ioutil.ReadAll(r.Body)
		err := runtime.GetActorRuntimeInstance().InvokeTimer(actorType, actorID, timerName, reqData)
		if err == actorErr.ErrActorTypeNotFound {
			w.WriteHeader(http.StatusNotFound)
		}
		if err != actorErr.Success {
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.WriteHeader(http.StatusOK)
	}
	s.mux.HandleFunc("/actors/{actorType}/{actorId}/method/timer/{timerName}", fTimer).Methods(http.MethodPut)
}

// AddTopicEventHandler appends provided event handler with it's name to the service.
func (s *Server) AddTopicEventHandler(sub *common.Subscription, fn common.TopicEventHandler) error {
	if sub == nil {
		return errors.New("subscription required")
	}
	// Route is only required for HTTP but should be specified for the
	// app protocol to be interchangeable.
	if sub.Route == "" {
		return errors.New("handler route name")
	}
	if err := s.topicRegistrar.AddSubscription(sub, fn); err != nil {
		return err
	}

	s.mux.Handle(sub.Route, optionsHandler(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// check for post with no data
			if r.ContentLength == 0 {
				http.Error(w, "nil content", PubSubHandlerDropStatusCode)
				return
			}

			// deserialize the event
			var in topicEventJSON
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, err.Error(), PubSubHandlerDropStatusCode)
				return
			}

			if in.PubsubName == "" {
				in.Topic = sub.PubsubName
			}
			if in.Topic == "" {
				in.Topic = sub.Topic
			}

			var data interface{}
			var rawData []byte
			if len(in.Data) > 0 {
				rawData = []byte(in.Data)
				data = rawData
				var v interface{}
				// We can assume that rawData is valid JSON
				// without checking in.DataContentType == "application/json".
				if err := json.Unmarshal(rawData, &v); err == nil {
					data = v
					// Handling of JSON base64 encoded or escaped in a string.
					if str, ok := v.(string); ok {
						// This is the path that will most likely succeed.
						var vString interface{}
						if err := json.Unmarshal([]byte(str), &vString); err == nil {
							data = vString
						} else if decoded, err := base64.StdEncoding.DecodeString(str); err == nil {
							// Decoded Base64 encoded JSON does not seem to be in the spec
							// but it is in existing unit tests so this handles that case.
							var vBase64 interface{}
							if err := json.Unmarshal(decoded, &vBase64); err == nil {
								data = vBase64
							}
						}
					}
				}
			} else if in.DataBase64 != "" {
				var err error
				rawData, err = base64.StdEncoding.DecodeString(in.DataBase64)
				if err == nil {
					data = rawData
					if in.DataContentType == "application/json" {
						var v interface{}
						if err := json.Unmarshal(rawData, &v); err == nil {
							data = v
						}
					}
				}
			}

			te := common.TopicEvent{
				ID:              in.ID,
				SpecVersion:     in.SpecVersion,
				Type:            in.Type,
				Source:          in.Source,
				DataContentType: in.DataContentType,
				Data:            data,
				RawData:         rawData,
				DataBase64:      in.DataBase64,
				Subject:         in.Subject,
				PubsubName:      in.PubsubName,
				Topic:           in.Topic,
			}

			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			// execute user handler
			retry, err := fn(r.Context(), &te)
			if err == nil {
				writeStatus(w, common.SubscriptionResponseStatusSuccess)
				return
			}

			if retry {
				writeStatus(w, common.SubscriptionResponseStatusRetry)
				return
			}

			writeStatus(w, common.SubscriptionResponseStatusDrop)
		})))

	return nil
}

func writeStatus(w http.ResponseWriter, s string) {
	status := &common.SubscriptionResponse{Status: s}
	if err := json.NewEncoder(w).Encode(status); err != nil {
		http.Error(w, err.Error(), PubSubHandlerRetryStatusCode)
	}
}
