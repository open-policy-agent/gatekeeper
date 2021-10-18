package mutators

import (
	"fmt"
	"testing"
	"time"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

func TestReportMutatorIngestionRequest(t *testing.T) {
	expectedTags := map[string]string{
		"status": "active",
	}

	const (
		expectedDurationValueMin = time.Duration(1 * time.Second)
		expectedDurationValueMax = time.Duration(5 * time.Second)
		expectedDurationMin      = 1.0
		expectedDurationMax      = 5.0
		expectedRowLength        = 1
	)

	r := NewStatsReporter()
	err := r.ReportMutatorIngestionRequest(MutatorStatusActive, expectedDurationValueMin)
	if err != nil {
		t.Errorf("ReportRequest error %v", err)
	}
	err = r.ReportMutatorIngestionRequest(MutatorStatusActive, expectedDurationValueMax)
	if err != nil {
		t.Errorf("ReportRequest error %v", err)
	}

	// Count test
	rows, err := checkData(mutatorIngestionCountMetricName, expectedRowLength)
	if err != nil {
		t.Error(err)
	}
	count, ok := rows[0].Data.(*view.CountData)
	if !ok {
		t.Error("ReportRequest should have aggregation Count()")
	}
	if count.Value != 2 {
		t.Errorf("Metric: %v - Expected %v, got %v. ", mutatorIngestionCountMetricName, 2, count.Value)
	}

	verifyTags(t, expectedTags, rows[0].Tags)

	// Duration test
	rows, err = checkData(mutatorIngestionDurationMetricName, expectedRowLength)
	if err != nil {
		t.Error(err)
	}
	durationValue, ok := rows[0].Data.(*view.DistributionData)
	if !ok {
		t.Fatalf("ReportRequest should have aggregation Distribution()")
	}
	if durationValue.Min != expectedDurationMin {
		t.Errorf("got tag '%v' min %v, want %v", mutatorIngestionDurationMetricName, durationValue.Min, expectedDurationMin)
	}
	if durationValue.Max != expectedDurationMax {
		t.Errorf("got tag '%v' max %v, want %v", mutatorIngestionDurationMetricName, durationValue.Max, expectedDurationMax)
	}

	verifyTags(t, expectedTags, rows[0].Tags)
}

func TestReportMutatorsStatus(t *testing.T) {
	r := NewStatsReporter()

	activeMutators := 5
	errorMutators := 1
	if err := r.ReportMutatorsStatus(MutatorStatusActive, activeMutators); err != nil {
		t.Errorf("ReportMutatorsStatus error %v", err)
	}
	if err := r.ReportMutatorsStatus(MutatorStatusError, errorMutators); err != nil {
		t.Errorf("ReportMutatorsStatus error %v", err)
	}

	rows, err := checkData(mutatorsMetricName, 2)
	if err != nil {
		t.Error(err)
	}

	activeRow, err := getRow(rows, string(MutatorStatusActive))
	if err != nil {
		t.Error(err)
	}

	lastValueData, ok := activeRow.Data.(*view.LastValueData)
	if !ok {
		t.Fatalf("wanted active status row of type LastValueData. got: %v", activeRow.Data)
	}

	if lastValueData.Value != float64(activeMutators) {
		t.Errorf("wanted status %q to have value %v, got: %v", MutatorStatusActive, activeMutators, lastValueData.Value)
	}

	errorRow, err := getRow(rows, string(MutatorStatusError))
	if err != nil {
		t.Error(err)
	}

	lastValueData, ok = errorRow.Data.(*view.LastValueData)
	if !ok {
		t.Fatalf("wanted error status row of type LastValueData. got: %v", activeRow.Data)
	}

	if lastValueData.Value != float64(errorMutators) {
		t.Errorf("wanted status %q to have value %v, got: %v", MutatorStatusError, activeMutators, lastValueData.Value)
	}
}

func TestReportMutatorsInConflict(t *testing.T) {
	r := NewStatsReporter()

	conflicts := 3

	// Report conflicts for the first time
	err := r.ReportMutatorsInConflict(conflicts)
	if err != nil {
		t.Errorf("ReportMutatorsInConflict error %v", err)
	}

	rows, err := checkData(mutatorsConflictingCountMetricsName, 1)
	if err != nil {
		t.Error(err)
	}

	lastValueData, ok := rows[0].Data.(*view.LastValueData)
	if !ok {
		t.Fatalf("wanted row of type LastValueData. got: %v", rows[0].Data)
	}

	if lastValueData.Value != float64(conflicts) {
		t.Errorf("wanted metric value %v, got %v", conflicts, lastValueData.Value)
	}

	// Report conflicts again, confirming the updated value
	conflicts = 2
	err = r.ReportMutatorsInConflict(conflicts)
	if err != nil {
		t.Errorf("ReportMutatorsInConflict error %v", err)
	}

	rows, err = checkData(mutatorsConflictingCountMetricsName, 1)
	if err != nil {
		t.Error(err)
	}

	lastValueData, ok = rows[0].Data.(*view.LastValueData)
	if !ok {
		t.Fatalf("wanted row of type LastValueData. got: %v", rows[0].Data)
	}

	if lastValueData.Value != float64(conflicts) {
		t.Errorf("wanted metric value %v, got %v", conflicts, lastValueData.Value)
	}
}

func getRow(rows []*view.Row, tagValue string) (*view.Row, error) {
	for _, row := range rows {
		if row.Tags[0].Value == tagValue {
			return row, nil
		}
	}

	return nil, fmt.Errorf("no rows found with tag key name %q", tagValue)
}

func checkData(name string, rowLength int) ([]*view.Row, error) {
	rows, err := view.RetrieveData(name)
	if err != nil {
		return nil, fmt.Errorf("Error when retrieving data: %v from %v", err, name)
	}
	if len(rows) != rowLength {
		return nil, fmt.Errorf("Got '%v' row length %v, want %v", name, len(rows), rowLength)
	}
	return rows, nil
}

func verifyTags(t *testing.T, expected map[string]string, actual []tag.Tag) {
	for _, tag := range actual {
		ex := expected[tag.Key.Name()]
		if tag.Value != ex {
			t.Errorf("Got tag '%v' value '%v', want '%v'", tag.Key.Name(), tag.Value, ex)
		}
	}
}
