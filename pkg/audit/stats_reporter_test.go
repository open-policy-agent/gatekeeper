package audit

import (
	"testing"
	"time"

	"go.opencensus.io/stats/view"
)

func TestReportTotalViolations(t *testing.T) {
	var expectedValue int64 = 10
	expectedTags := map[string]string{
		"enforcement_action": "deny",
	}

	r, err := NewStatsReporter()
	if err != nil {
		t.Errorf("NewStatsReporter() error %v", err)
	}

	err = r.ReportTotalViolations("deny", expectedValue)
	if err != nil {
		t.Errorf("ReportTotalViolations error %v", err)
	}
	row, err := view.RetrieveData(totalViolationsName)
	if err != nil {
		t.Errorf("Error when retrieving data: %v from %v", err, totalViolationsName)
	}
	value, ok := row[0].Data.(*view.LastValueData)
	if !ok {
		t.Error("ReportTotalViolations should have aggregation LastValue()")
	}
	for _, tag := range row[0].Tags {
		if tag.Value != expectedTags[tag.Key.Name()] {
			t.Errorf("ReportTotalViolations tags does not match for %v", tag.Key.Name())
		}
	}
	if int64(value.Value) != expectedValue {
		t.Errorf("Metric: %v - Expected %v, got %v. ", totalViolationsName, value.Value, expectedValue)
	}
}

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
		t.Errorf("Metric: %v - Expected %v, got %v. ", totalConstraintsName, value.Value, expectedValue)
	}
}

func TestReportLatency(t *testing.T) {
	expectedLatencyValueMin := time.Duration(100 * time.Second)
	expectedLatencyValueMax := time.Duration(500 * time.Second)
	var expectedLatencyCount int64 = 2
	var expectedLatencyMin float64 = 100
	var expectedLatencyMax float64 = 500

	r, err := NewStatsReporter()
	if err != nil {
		t.Errorf("NewStatsReporter() error %v", err)
	}

	err = r.ReportLatency(expectedLatencyValueMin)
	if err != nil {
		t.Errorf("ReportLatency error %v", err)
	}
	err = r.ReportLatency(expectedLatencyValueMax)
	if err != nil {
		t.Errorf("ReportLatency error %v", err)
	}
	row, err := view.RetrieveData(auditDuration)
	if err != nil {
		t.Errorf("Error when retrieving data: %v from %v", err, auditDuration)
	}
	latencyValue, ok := row[0].Data.(*view.DistributionData)
	if !ok {
		t.Error("ReportLatency should have aggregation type Distribution")
	}
	if latencyValue.Count != expectedLatencyCount {
		t.Errorf("Metric: %v - Expected %v, got %v. ", auditDuration, latencyValue.Count, expectedLatencyCount)
	}
	if latencyValue.Min != expectedLatencyMin {
		t.Errorf("Metric: %v - Expected %v, got %v. ", auditDuration, latencyValue.Min, expectedLatencyMin)
	}
	if latencyValue.Max != expectedLatencyMax {
		t.Errorf("Metric: %v - Expected %v, got %v. ", auditDuration, latencyValue.Max, expectedLatencyMax)
	}
}
