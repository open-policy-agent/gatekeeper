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

package main

import (
	"testing"

	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
)

func TestNewExportSystem(t *testing.T) {
	origExport := *exportutil.ExportEnabled
	origAdmission := *exportutil.AdmissionExportEnabled
	defer func() {
		*exportutil.ExportEnabled = origExport
		*exportutil.AdmissionExportEnabled = origAdmission
	}()

	tests := []struct {
		name             string
		exportEnabled    bool
		admissionEnabled bool
		wantNil          bool
	}{
		{"both disabled", false, false, true},
		{"audit export enabled", true, false, false},
		{"admission export enabled", false, true, false},
		{"both enabled", true, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			*exportutil.ExportEnabled = tc.exportEnabled
			*exportutil.AdmissionExportEnabled = tc.admissionEnabled

			got := newExportSystem()
			if gotNil := got == nil; gotNil != tc.wantNil {
				t.Errorf("newExportSystem() = %v, want nil: %v", got, tc.wantNil)
			}
		})
	}
}
