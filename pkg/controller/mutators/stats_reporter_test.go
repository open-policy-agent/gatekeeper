package mutators

import (
	"fmt"
	"testing"
	"time"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

func TestReportMutatorIngestionRequest(t *testing.T) {
	wantTags := map[string]string{
		"status": "active",
	}

	const (
		minIngestionDuration = 1 * time.Second
		maxIngestionDuration = 5 * time.Second

		wantMinIngestionDuration = 1.0
		wantMaxIngestionDuration = 5.0

		wantRowLength      = 1
		wantIngestionCount = 2
	)

	r := NewStatsReporter()
	err := r.ReportMutatorIngestionRequest(MutatorStatusActive, minIngestionDuration)
	if err != nil {
		t.Fatalf("got ReportRequest error %v, want nil", err)
	}

	err = r.ReportMutatorIngestionRequest(MutatorStatusActive, maxIngestionDuration)
	if err != nil {
		t.Fatalf("got ReportRequest error %v, want nil", err)
	}

	// Count test
	rows, err := checkData(mutatorIngestionCountMetricName, wantRowLength)
	if err != nil {
		t.Fatal(err)
	}

	gotCount, ok := rows[0].Data.(*view.CountData)
	if !ok {
		t.Fatalf("got %q type %T, want %T", mutatorIngestionCountMetricName, rows[0].Data, &view.CountData{})
	}

	if gotCount.Value != wantIngestionCount {
		t.Errorf("got %q = %v, want %v", mutatorIngestionCountMetricName, gotCount.Value, wantIngestionCount)
	}

	verifyTags(t, wantTags, rows[0].Tags)

	// Duration test
	rows, err = checkData(mutatorIngestionDurationMetricName, wantRowLength)
	if err != nil {
		t.Error(err)
	}

	durationValue, ok := rows[0].Data.(*view.DistributionData)
	if !ok {
		t.Fatalf("got %q type %T, want %T", mutatorIngestionCountMetricName, rows[0].Data, &view.DistributionData{})
	}

	if durationValue.Min != wantMinIngestionDuration {
		t.Errorf("got tag %q min %v, want %v", mutatorIngestionDurationMetricName, durationValue.Min, wantMinIngestionDuration)
	}

	if durationValue.Max != wantMaxIngestionDuration {
		t.Errorf("got tag %q max %v, want %v", mutatorIngestionDurationMetricName, durationValue.Max, wantMaxIngestionDuration)
	}

	verifyTags(t, wantTags, rows[0].Tags)
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
		t.Fatalf("got row type %T, want type 'view.LastValueData'", activeRow.Data)
	}

	if lastValueData.Value != float64(activeMutators) {
		t.Errorf("got row value %v for status %q, want %v", lastValueData.Value, MutatorStatusActive, activeMutators)
	}

	errorRow, err := getRow(rows, string(MutatorStatusError))
	if err != nil {
		t.Error(err)
	}

	lastValueData, ok = errorRow.Data.(*view.LastValueData)
	if !ok {
		t.Fatalf("got row type %T, want type 'view.LastValueData'", errorRow.Data)
	}

	if lastValueData.Value != float64(errorMutators) {
		t.Errorf("got row value %v for status %q, want %v", lastValueData.Value, MutatorStatusError, errorMutators)
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
		return nil, fmt.Errorf("got RetrieveData error %v from %v, want nil", err, name)
	}
	if len(rows) != rowLength {
		return nil, fmt.Errorf("got %q row length %v, want %v", name, len(rows), rowLength)
	}
	return rows, nil
}

func verifyTags(t *testing.T, wantTags map[string]string, actual []tag.Tag) {
	for _, gotTag := range actual {
		tagName := gotTag.Key.Name()
		want := wantTags[tagName]
		if gotTag.Value != want {
			t.Errorf("got tag %q value %q, want %q", tagName, gotTag.Value, want)
		}
	}
}
