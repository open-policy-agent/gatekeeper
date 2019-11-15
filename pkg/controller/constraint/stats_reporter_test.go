package constraint

import (
	"testing"

	"go.opencensus.io/stats/view"
)

func TestReportConstraints(t *testing.T) {
	var expectedValue int64 = 10
	expectedTags := map[string]string{
		"enforcement_action": "deny",
	}

	r, err := NewStatsReporter()
	if err != nil {
		t.Errorf("NewStatsReporter() error %v", err)
	}

	err = r.ReportConstraints("deny", expectedValue)
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
		if tag.Value != expectedTags[tag.Key.Name()] {
			t.Errorf("ReportConstraints tags does not match for %v", tag.Key.Name())
		}
	}
	if int64(value.Value) != expectedValue {
		t.Errorf("Metric: %v - Expected %v, got %v", totalConstraintsName, value.Value, expectedValue)
	}
}
