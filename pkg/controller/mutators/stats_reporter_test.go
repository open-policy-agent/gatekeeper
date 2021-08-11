package mutators

import (
	"context"
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
	ctx := context.Background()
	err := r.ReportMutatorIngestionRequest(ctx, MutatorStatusActive, expectedDurationValueMin)
	if err != nil {
		t.Errorf("ReportRequest error %v", err)
	}
	err = r.ReportMutatorIngestionRequest(ctx, MutatorStatusActive, expectedDurationValueMax)
	if err != nil {
		t.Errorf("ReportRequest error %v", err)
	}

	// Count test
	row, err := checkData(mutatorIngestionCountMetricName, expectedRowLength)
	if err != nil {
		t.Error(err)
	}
	count, ok := row.Data.(*view.CountData)
	if !ok {
		t.Error("ReportRequest should have aggregation Count()")
	}
	if count.Value != 2 {
		t.Errorf("Metric: %v - Expected %v, got %v. ", mutatorIngestionCountMetricName, 2, count.Value)
	}

	verifyTags(t, expectedTags, row.Tags)

	// Duration test
	row, err = checkData(mutatorIngestionDurationMetricName, expectedRowLength)
	if err != nil {
		t.Error(err)
	}
	durationValue, ok := row.Data.(*view.DistributionData)
	if !ok {
		t.Error("ReportRequest should have aggregation Distribution()")
	}
	if durationValue.Min != expectedDurationMin {
		t.Errorf("got tag '%v' min %v, want %v", mutatorIngestionDurationMetricName, durationValue.Min, expectedDurationMin)
	}
	if durationValue.Max != expectedDurationMax {
		t.Errorf("got tag '%v' max %v, want %v", mutatorIngestionDurationMetricName, durationValue.Max, expectedDurationMax)
	}

	verifyTags(t, expectedTags, row.Tags)
}

func checkData(name string, rowLength int) (*view.Row, error) {
	row, err := view.RetrieveData(name)
	if err != nil {
		return nil, fmt.Errorf("Error when retrieving data: %v from %v", err, name)
	}
	if len(row) != rowLength {
		return nil, fmt.Errorf("Got '%v' row length %v, want %v", name, len(row), rowLength)
	}
	if row[0].Data == nil {
		return nil, fmt.Errorf("Expected row data not to be nil")
	}
	return row[0], nil
}

func verifyTags(t *testing.T, expected map[string]string, actual []tag.Tag) {
	for _, tag := range actual {
		ex := expected[tag.Key.Name()]
		if tag.Value != ex {
			t.Errorf("Got tag '%v' value '%v', want '%v'", tag.Key.Name(), tag.Value, ex)
		}
	}
}
