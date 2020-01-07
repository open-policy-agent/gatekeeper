package audit

import (
	"testing"
	"time"

	"go.opencensus.io/stats/view"
)

func TestReportTotalViolations(t *testing.T) {
	const expectedValue int64 = 10
	const expectedRowLength = 1
	expectedTags := map[string]string{
		"enforcement_action": "deny",
	}

	r, err := newStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}
	err = r.ReportTotalViolations("deny", expectedValue)
	if err != nil {
		t.Errorf("ReportTotalViolations error %v", err)
	}
	row := checkData(t, violationsMetricName, expectedRowLength)
	value, ok := row.Data.(*view.LastValueData)
	if !ok {
		t.Error("ReportTotalViolations should have aggregation LastValue()")
	}
	for _, tag := range row.Tags {
		if tag.Value != expectedTags[tag.Key.Name()] {
			t.Errorf("ReportTotalViolations tags does not match for %v", tag.Key.Name())
		}
	}
	if int64(value.Value) != expectedValue {
		t.Errorf("Metric: %v - Expected %v, got %v", violationsMetricName, value.Value, expectedValue)
	}
}

func TestReportLatency(t *testing.T) {
	const expectedLatencyValueMin = time.Duration(100 * time.Second)
	const expectedLatencyValueMax = time.Duration(500 * time.Second)
	const expectedLatencyCount int64 = 2
	const expectedLatencyMin float64 = 100
	const expectedLatencyMax float64 = 500
	const expectedRowLength = 1

	r, err := newStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}
	err = r.ReportLatency(expectedLatencyValueMin)
	if err != nil {
		t.Errorf("ReportLatency error %v", err)
	}
	err = r.ReportLatency(expectedLatencyValueMax)
	if err != nil {
		t.Errorf("ReportLatency error %v", err)
	}
	row := checkData(t, auditDurationMetricName, expectedRowLength)
	latencyValue, ok := row.Data.(*view.DistributionData)
	if !ok {
		t.Error("ReportLatency should have aggregation type Distribution")
	}
	if latencyValue.Count != expectedLatencyCount {
		t.Errorf("Metric: %v - Expected %v, got %v", auditDurationMetricName, latencyValue.Count, expectedLatencyCount)
	}
	if latencyValue.Min != expectedLatencyMin {
		t.Errorf("Metric: %v - Expected %v, got %v", auditDurationMetricName, latencyValue.Min, expectedLatencyMin)
	}
	if latencyValue.Max != expectedLatencyMax {
		t.Errorf("Metric: %v - Expected %v, got %v", auditDurationMetricName, latencyValue.Max, expectedLatencyMax)
	}
}

func checkData(t *testing.T, name string, expectedRowLength int) *view.Row {
	row, err := view.RetrieveData(name)
	if err != nil {
		t.Errorf("Error when retrieving data: %v from %v", err, name)
	}
	if len(row) != expectedRowLength {
		t.Errorf("Expected length %v, got %v", expectedRowLength, len(row))
	}
	if row[0].Data == nil {
		t.Errorf("Expected row data not to be nil")
	}
	return row[0]
}
