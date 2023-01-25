package verify

import (
	"errors"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/gator"
	"k8s.io/utils/pointer"
)

func TestAssertion_Run(t *testing.T) {
	tests := []struct {
		name      string
		assertion *Assertion
		results   []*types.Result
		wantErr   error
	}{{
		name:      "default to expect violation",
		assertion: &Assertion{},
		results:   nil,
		wantErr:   gator.ErrNumViolations,
	}, {
		name: "no violations",
		assertion: &Assertion{
			Violations: gator.IntStrFromInt(0),
		},
		results: nil,
		wantErr: nil,
	}, {
		name: "negative violations",
		assertion: &Assertion{
			Violations: gator.IntStrFromInt(-1),
		},
		results: nil,
		wantErr: gator.ErrInvalidYAML,
	}, {
		name: "violation with message",
		assertion: &Assertion{
			Violations: gator.IntStrFromInt(1),
			Message:    pointer.String("message"),
		},
		results: nil,
		wantErr: gator.ErrNumViolations,
	}, {
		name: "no violations with message",
		assertion: &Assertion{
			Violations: gator.IntStrFromStr("no"),
			Message:    pointer.String("message"),
		},
		results: nil,
		wantErr: nil,
	}, {
		name: "fail no violations with message",
		assertion: &Assertion{
			Violations: gator.IntStrFromStr("no"),
			Message:    pointer.String("message"),
		},
		results: []*types.Result{{
			Msg: "message",
		}},
		wantErr: gator.ErrNumViolations,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.assertion.Run(tt.results)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
