package instrumentation

import (
	"testing"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	cfinstr "github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ToStatsEntriesWithDesc(t *testing.T) {
	driver, err := rego.New()
	assert.NoError(t, err)

	actualClient, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver), constraintclient.EnforcementPoints(util.AuditEnforcementPoint))
	assert.NoError(t, err)

	testCases := []struct {
		name      string
		client    *constraintclient.Client
		cfentries []*cfinstr.StatsEntry
		expected  []*StatsEntryWithDesc
	}{
		{
			name:      "Empty input",
			client:    nil,
			cfentries: []*cfinstr.StatsEntry{},
			expected:  nil,
		},
		{
			name:   "Single entry with one stat, unknown description",
			client: nil,
			cfentries: []*cfinstr.StatsEntry{
				{
					Scope:    "scope1",
					StatsFor: "statsFor1",
					Stats: []*cfinstr.Stat{
						{
							Name:  "stat1",
							Value: "value1",
						},
					},
					Labels: []*cfinstr.Label{
						{
							Name:  "label1",
							Value: "lvalue1",
						},
					},
				},
			},
			expected: []*StatsEntryWithDesc{
				{
					Scope:    "scope1",
					StatsFor: "statsFor1",
					Stats: []*StatWithDesc{
						{
							Stat: cfinstr.Stat{
								Name:  "stat1",
								Value: "value1",
							},
							Description: cfinstr.UnknownDescription,
						},
					},
					Labels: []*cfinstr.Label{
						{
							Name:  "label1",
							Value: "lvalue1",
						},
					},
				},
			},
		},
		{
			name:   "actual client, stat",
			client: actualClient,
			cfentries: []*cfinstr.StatsEntry{
				{
					Scope:    "scope1",
					StatsFor: "statsFor1",
					Stats: []*cfinstr.Stat{
						{
							Name:  "templateRunTimeNS",
							Value: "value1",
							Source: cfinstr.Source{
								Type:  cfinstr.EngineSourceType,
								Value: "Rego",
							},
						},
						{
							Name:  "constraintCount",
							Value: "value1",
							Source: cfinstr.Source{
								Type:  cfinstr.EngineSourceType,
								Value: "Rego",
							},
						},
					},
					Labels: []*cfinstr.Label{
						{
							Name:  "label1",
							Value: "lvalue1",
						},
					},
				},
			},
			expected: []*StatsEntryWithDesc{
				{
					Scope:    "scope1",
					StatsFor: "statsFor1",
					Stats: []*StatWithDesc{
						{
							Stat: cfinstr.Stat{
								Name:  "templateRunTimeNS",
								Value: "value1",
								Source: cfinstr.Source{
									Type:  cfinstr.EngineSourceType,
									Value: "Rego",
								},
							},
							Description: "the number of nanoseconds it took to evaluate all constraints for a template",
						},
						{
							Stat: cfinstr.Stat{
								Name:  "constraintCount",
								Value: "value1",
								Source: cfinstr.Source{
									Type:  cfinstr.EngineSourceType,
									Value: "Rego",
								},
							},
							Description: "the number of constraints that were evaluated for the given constraint kind",
						},
					},
					Labels: []*cfinstr.Label{
						{
							Name:  "label1",
							Value: "lvalue1",
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ToStatsEntriesWithDesc(tc.client, tc.cfentries)
			require.Equal(t, tc.expected, result)
		})
	}
}
