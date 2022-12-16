package logging

import (
	"bytes"
	"testing"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	"github.com/stretchr/testify/require"
	"k8s.io/klog/v2"
)

func Test_LogStatsEntries(t *testing.T) {
	testLogger := klog.NewKlogr()
	testBuf := bytes.NewBuffer(nil)

	klog.SetOutput(testBuf)
	klog.LogToStderr(false)

	LogStatsEntries(
		&constraintclient.Client{},
		testLogger,
		[]*instrumentation.StatsEntry{
			{
				Scope:    "someScope",
				StatsFor: "someConstranint",
				Stats: []*instrumentation.Stat{
					{
						Name:  "someStat",
						Value: "someValue",
						Source: instrumentation.Source{
							Type:  "someType",
							Value: "someValue",
						},
					},
				},
				Labels: []*instrumentation.Label{
					{
						Name:  "someLabel",
						Value: "someLabelValue",
					},
				},
			},
		},
		"test message",
	)

	require.Contains(t, testBuf.String(), "\"test message\" someLabel=\"someLabelValue\" "+
		"scope=\"someScope\" statsFor=\"someConstranint\" source_type=\"someType\" "+
		"source_value=\"someValue\" name=\"someStat\" value=\"someValue\" description=\"unknown description\"",
	)
}
