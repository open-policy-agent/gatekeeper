/*
Copyright 2023 The Dapr Authors
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

package client

import "encoding/json"

// isCloudEvent returns true if the event is a CloudEvent.
// An event is a CloudEvent if it `id`, `source`, `specversion` and `type` fields.
// See https://github.com/cloudevents/spec/blob/main/cloudevents/spec.md for more details.
func isCloudEvent(event []byte) bool {
	var ce struct {
		ID          string `json:"id"`
		Source      string `json:"source"`
		SpecVersion string `json:"specversion"`
		Type        string `json:"type"`
	}
	if err := json.Unmarshal(event, &ce); err != nil {
		return false
	}
	return ce.ID != "" && ce.Source != "" && ce.SpecVersion != "" && ce.Type != ""
}
