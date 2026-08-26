package main

import (
	"testing"

	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
)

func setExportFlags(t *testing.T, exportEnabled, admissionExportEnabled bool) {
	t.Helper()
	oldExport, oldAdmission := *exportutil.ExportEnabled, *exportutil.AdmissionExportEnabled
	t.Cleanup(func() {
		*exportutil.ExportEnabled = oldExport
		*exportutil.AdmissionExportEnabled = oldAdmission
	})
	*exportutil.ExportEnabled = exportEnabled
	*exportutil.AdmissionExportEnabled = admissionExportEnabled
}

func TestNewExportSystem(t *testing.T) {
	tests := []struct {
		name                   string
		exportEnabled          bool
		admissionExportEnabled bool
		wantSystem             bool
	}{
		{name: "export disabled", exportEnabled: false, admissionExportEnabled: false, wantSystem: false},
		{name: "audit export enabled", exportEnabled: true, admissionExportEnabled: false, wantSystem: true},
		{name: "admission export enabled", exportEnabled: false, admissionExportEnabled: true, wantSystem: true},
		{name: "both exports enabled", exportEnabled: true, admissionExportEnabled: true, wantSystem: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setExportFlags(t, tt.exportEnabled, tt.admissionExportEnabled)
			system := newExportSystem()
			if (system != nil) != tt.wantSystem {
				t.Errorf("newExportSystem() = %v, want system present: %v", system, tt.wantSystem)
			}
		})
	}
}
