package constraint

import (
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"go.opencensus.io/stats/view"
)

func TestReportConstraints(t *testing.T) {
	var expectedValue int64 = 10
	expectedTags := util.Tags{
		EnforcementAction: util.Deny,
	}

	r, err := NewStatsReporter()
	if err != nil {
		t.Errorf("NewStatsReporter() error %v", err)
	}

	err = r.ReportConstraints(expectedTags, expectedValue)
	if err != nil {
		t.Errorf("ReportConstraints error %v", err)
	}
	row, err := view.RetrieveData(totalConstraintsName)
	if err != nil {
		t.Errorf("Error when retrieving data: %v from %v", err, totalConstraintsName)
	}
	value, ok := row[0].Data.(*view.LastValueData)
	if !ok {
		t.Error("ReportConstraints should have aggregation LastValue()")
	}
	for _, tag := range row[0].Tags {
		if tag.Value != string(expectedTags.EnforcementAction) {
			t.Errorf("ReportConstraints tags does not match for %v", tag.Key.Name())
		}
	}
	if int64(value.Value) != expectedValue {
		t.Errorf("Metric: %v - Expected %v, got %v", totalConstraintsName, value.Value, expectedValue)
	}
}
