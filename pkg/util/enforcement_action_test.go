package util

import (
	"errors"
	"testing"
)

func TestValidateEnforcementAction(t *testing.T) {
	testCases := []struct {
		name    string
		action  EnforcementAction
		wantErr error
	}{
		{
			name:    "empty string",
			action:  "",
			wantErr: ErrEnforcementAction,
		},
		{
			action:  "notsupported",
			wantErr: ErrEnforcementAction,
		},
		{
			action: Dryrun,
		},
	}

	for _, tc := range testCases {
		if tc.name == "" {
			tc.name = string(tc.action)
		}
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEnforcementAction(tc.action)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got ValidateEnforcementAction(%q) == %v, want %v",
					tc.action, err, tc.wantErr)
			}
		})
	}
}

func TestGetEnforcementAction(t *testing.T) {
	testCases := []struct {
		name    string
		item    map[string]interface{}
		want    EnforcementAction
		wantErr error
	}{
		{
			name: "empty item",
			item: map[string]interface{}{},
			want: Deny,
		},
		{
			name: "invalid spec.enforcementAction",
			item: map[string]interface{}{
				"spec": []string{},
			},
			wantErr: ErrInvalidSpecEnforcementAction,
		},
		{
			name: "unsupported spec.enforcementAction",
			item: map[string]interface{}{
				"spec": map[string]interface{}{
					"enforcementAction": "notsupported",
				},
			},
			want: Unrecognized,
		},
		{
			name: "valid spec.enforcementAction",
			item: map[string]interface{}{
				"spec": map[string]interface{}{
					"enforcementAction": string(Dryrun),
				},
			},
			want: Dryrun,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GetEnforcementAction(tc.item)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got GetEnforcementAction() error = %v, want %v",
					err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("got GetEnforcementAction() = %v, want %v",
					got, tc.want)
			}
		})
	}
}
