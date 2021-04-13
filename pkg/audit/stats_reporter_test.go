package audit

import (
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"go.opencensus.io/stats/view"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestReportTotalViolationsByEnforcementAction(t *testing.T) {
	const expectedValue int64 = 10
	const expectedRowLength = 1
	expectedTags := map[string]string{
		"enforcement_action": "deny",
	}

	r, err := newStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}
	err = r.reportTotalViolationsByEnforcementAction("deny", expectedValue)
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

func TestReportTotalViolationsByConstraint(t *testing.T) {
	var expectedValues = []int64{10, 20}

	kind1 := "kind1"
	kind2 := "kind2"
	name1 := "name1"
	name2 := "name2"
	expectedTags := []map[string]string{
		{
			"constraint_type": kind1,
			"constraint_name": name1,
		},
		{
			"constraint_type": kind2,
			"constraint_name": name2,
		},
	}

	r, err := newStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}

	constraint1 := createConstraint(kind1, name1)
	constraint2 := createConstraint(kind2, name2)
	constraints := []util.KindVersionResource{constraint1, constraint2}

	for i := range []int{1, 2} {
		err = r.reportTotalViolationsByConstraint(constraints[i], expectedValues[i])
		if err != nil {
			t.Errorf("ReportTotalViolations error %v", err)
		}
	}

	rows, err := view.RetrieveData(violationsMetricName)
	if err != nil {
		t.Errorf("Error when retrieving data: %v from %v", err, violationsMetricName)
	}

	// because the view package has a global state, we have a side effect from the previous test case
	var adjustedRows []*view.Row
	for _, row := range rows {
		if row.Tags[0].Key.Name() == "enforcement_action" {
			continue
		}
		adjustedRows = append(adjustedRows, row)
	}
	if len(adjustedRows) != 2 {
		t.Errorf("Expected length %v, got %v", 2, len(adjustedRows))
	}

	for i := 0; i < len(adjustedRows); i++ {
		row := adjustedRows[i]
		if row.Data == nil {
			t.Errorf("Expected rows data not to be nil")
		}

		value, ok := row.Data.(*view.LastValueData)
		if !ok {
			t.Error("ReportTotalViolations should have aggregation LastValue()")
		}
		for _, tag := range row.Tags {
			if tag.Key.Name() == "enforcement_action" {
				continue
			}

			if tag.Value != expectedTags[i][tag.Key.Name()] {
				t.Errorf("ReportTotalViolations tags does not match for %v", tag.Key.Name())
			}
		}
		if int64(value.Value) != expectedValues[i] {
			t.Errorf("Metric: %v - Expected %v, got %v", violationsMetricName, value.Value, expectedValues[i])
		}
	}
}

func createConstraint(kind string, name string) util.KindVersionResource {
	return util.GetUniqueKey(unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       kind,
			"metadata": map[string]interface{}{
				"name": name,
			},
		},
	})
}

func TestReportLatency(t *testing.T) {
	const expectedLatencyValueMin = 100 * time.Second
	const expectedLatencyValueMax = 500 * time.Second
	const expectedLatencyCount int64 = 2
	const expectedLatencyMin float64 = 100
	const expectedLatencyMax float64 = 500
	const expectedRowLength = 1

	r, err := newStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}
	err = r.reportLatency(expectedLatencyValueMin)
	if err != nil {
		t.Errorf("ReportLatency error %v", err)
	}
	err = r.reportLatency(expectedLatencyValueMax)
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

func TestLastRestartCheck(t *testing.T) {
	expectedTime := time.Now()
	expectedTs := float64(expectedTime.UnixNano()) / 1e9
	const expectedRowLength = 1

	r, err := newStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}
	err = r.reportRunStart(expectedTime)
	if err != nil {
		t.Errorf("reportRunStart error %v", err)
	}
	row := checkData(t, lastRunTimeMetricName, expectedRowLength)
	value, ok := row.Data.(*view.LastValueData)
	if !ok {
		t.Error("lastRunTimeMetricName should have aggregation LastValue()")
	}
	if len(row.Tags) != 0 {
		t.Errorf("lastRunTimeMetricName tags is non-empty, got: %v", row.Tags)
	}
	if value.Value != expectedTs {
		t.Errorf("Metric: %v - Expected %v, got %v", lastRunTimeMetricName, expectedTs, value.Value)
	}
}
