package metrics

import (
	"context"
	"testing"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
)

func TestRecord(t *testing.T) {
	const measureName = "test_total"
	const expectedValue int64 = 10
	const expectedRowLength = 1
	testM := stats.Int64(measureName, measureName, stats.UnitDimensionless)

	ctx := context.Background()
	testView := &view.View{
		Measure:     testM,
		Aggregation: view.LastValue(),
	}

	if err := view.Register(testView); err != nil {
		t.Errorf("failed to register views: %v", err)
	}
	defer view.Unregister(testView)

	if err := Record(ctx, testM.M(expectedValue)); err != nil {
		t.Errorf("failed while recording: %v", err)
	}

	row := checkData(t, measureName, expectedRowLength)
	value, ok := row.Data.(*view.LastValueData)
	if !ok {
		t.Error("ReportConstraints should have aggregation LastValue()")
	}
	if int64(value.Value) != expectedValue {
		t.Errorf("Metric: %v - Expected %v, got %v. ", measureName, value.Value, expectedValue)
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
