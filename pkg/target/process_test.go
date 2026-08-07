package target

import (
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
)

func TestProcessFromEnforcementPoint(t *testing.T) {
	tests := []struct {
		name             string
		enforcementPoint string
		want             process.Process
	}{
		{
			name:             "audit enforcement point maps to audit process",
			enforcementPoint: util.AuditEnforcementPoint,
			want:             process.Audit,
		},
		{
			name:             "webhook enforcement point maps to webhook process",
			enforcementPoint: util.WebhookEnforcementPoint,
			want:             process.Webhook,
		},
		{
			name:             "vap enforcement point maps to webhook process",
			enforcementPoint: util.VAPEnforcementPoint,
			want:             process.Webhook,
		},
		{
			name:             "gator enforcement point maps to webhook process",
			enforcementPoint: util.GatorEnforcementPoint,
			want:             process.Webhook,
		},
		{
			name:             "empty enforcement point maps to webhook process",
			enforcementPoint: "",
			want:             process.Webhook,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := processFromEnforcementPoint(tt.enforcementPoint); got != tt.want {
				t.Errorf("processFromEnforcementPoint(%q) = %q, want %q", tt.enforcementPoint, got, tt.want)
			}
		})
	}
}
