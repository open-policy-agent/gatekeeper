/*

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

package operations

import "testing"

func TestNeedsValidationDataSync(t *testing.T) {
	tests := []struct {
		name     string
		assigned []Operation
		want     bool
	}{
		{
			name: "no operations",
		},
		{
			name:     "audit",
			assigned: []Operation{Audit},
			want:     true,
		},
		{
			name:     "validating webhook",
			assigned: []Operation{Webhook},
			want:     true,
		},
		{
			name:     "status only",
			assigned: []Operation{Status},
		},
		{
			name:     "mutation webhook only",
			assigned: []Operation{MutationWebhook},
		},
		{
			name:     "mutation status only",
			assigned: []Operation{MutationStatus},
		},
		{
			name:     "mutation controller only",
			assigned: []Operation{MutationController},
		},
		{
			name:     "generate only",
			assigned: []Operation{Generate},
		},
		{
			name:     "generate and mutation webhook",
			assigned: []Operation{Generate, MutationWebhook},
		},
		{
			name:     "audit and status",
			assigned: []Operation{Audit, Status},
			want:     true,
		},
		{
			name:     "all operations",
			assigned: allOperations,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore, err := SetForTest(tt.assigned...)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restore)

			if got := NeedsValidationDataSync(); got != tt.want {
				t.Errorf("NeedsValidationDataSync() = %t, want %t", got, tt.want)
			}
		})
	}
}
