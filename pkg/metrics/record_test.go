package metrics

import (
	"context"
	"testing"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
)

func TestRecord(t *testing.T) {
	const measureName = "test_total"
	testM := stats.Int64(measureName, measureName, stats.UnitDimensionless)
	var expectedValue int64 = 10

	ctx := context.Background()
	testView := &view.View{
		Measure:     testM,
		Aggregation: view.LastValue(),
	}

	if err := view.Register(testView); err != nil {
		t.Errorf("failed to register views: %v", err)
	}
	defer view.Unregister(testView)

	Record(ctx, testM.M(expectedValue))

	row, err := view.RetrieveData(measureName)
	if err != nil {
		t.Errorf("Error when retrieving data: %v from %v", err, measureName)
	}
	value, ok := row[0].Data.(*view.LastValueData)
	if !ok {
		t.Error("ReportConstraints should have aggregation LastValue()")
	}
	if int64(value.Value) != expectedValue {
		t.Errorf("Metric: %v - Expected %v, got %v. ", measureName, value.Value, expectedValue)
	}
}
