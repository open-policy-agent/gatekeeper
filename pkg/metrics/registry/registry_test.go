package registry

import (
	"flag"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/pkg/metrics/exporters/prometheus"
)

// Validates flags parsing for metrics reporters.
func Test_Flags(t *testing.T) {
	tests := map[string]struct {
		input    []string
		expected map[string]StartExporter
	}{
		"empty": {
			input:    []string{},
			expected: map[string]StartExporter{},
		},
		"multiple": {
			input: []string{"--metrics-backend", "prometheus", "--metrics-backend", "stackdriver"},
			expected: map[string]StartExporter{
				prometheus.Name: exporters.registeredExporters[prometheus.Name],
				"stackdriver":   exporters.registeredExporters["stackdriver"],
			},
		},
		"one": {
			input:    []string{"--metrics-backend", "opencensus"},
			expected: map[string]StartExporter{"opencensus": exporters.registeredExporters["opencensus"]},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			exportersTst := newExporterSet(exporters.registeredExporters)
			flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
			flagSet.Var(exportersTst, "metrics-backend", "Backend used for metrics. e.g. `prometheus`, `stackdriver`. This flag can be declared more than once. Omitting will default to supporting `prometheus`.")

			err := flagSet.Parse(tc.input)
			if err != nil {
				t.Errorf("parsing: %v", err)
				return
			}
			if diff := cmp.Diff(tc.expected, exportersTst.assignedExporters,
				// this compares the memory addresses of the referenced functions
				cmp.Transformer("interface", func(se StartExporter) string {
					return fmt.Sprint(se)
				})); diff != "" {
				t.Errorf("unexpected result: %s", diff)
			}
		})
	}
}
