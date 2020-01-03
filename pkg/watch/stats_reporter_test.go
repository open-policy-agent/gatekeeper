package watch

import (
	"reflect"
	"testing"

	"go.opencensus.io/stats/view"
)

func TestLastRestartCheck(t *testing.T) {
	const expectedTime float64 = 11
	const expectedRowLength = 1

	fakeNow := func() float64 {
		return float64(expectedTime)
	}

	r, err := newStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}
	r.now = fakeNow
	err = r.reportRestartCheck()
	if err != nil {
		t.Errorf("reportRestartCheck error %v", err)
	}
	row := checkData(t, lastRestartCheck, expectedRowLength)
	value, ok := row.Data.(*view.LastValueData)
	if !ok {
		t.Error("reportRestartCheck should have aggregation LastValue()")
	}
	if len(row.Tags) != 0 {
		t.Errorf("reportRestartCheck tags is non-empty, got: %v", row.Tags)
	}
	if value.Value != expectedTime {
		t.Errorf("Metric: %v - Expected %v, got %v", lastRestartCheck, expectedTime, value.Value)
	}
}

func TestLastRestart(t *testing.T) {
	const expectedTime float64 = 11
	const expectedRowLength = 1

	fakeNow := func() float64 {
		return float64(expectedTime)
	}

	if err := reset(); err != nil {
		t.Errorf("Could not reset stats: %v", err)
	}

	r, err := newStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}
	r.now = fakeNow
	if err := r.reportRestart(); err != nil {
		t.Errorf("reportRestart error %v", err)
	}
	row := checkData(t, lastRestart, expectedRowLength)
	value, ok := row.Data.(*view.LastValueData)
	if !ok {
		t.Error("reportRestart should have aggregation LastValue()")
	}
	if len(row.Tags) != 0 {
		t.Errorf("reportRestart tags is non-empty, got: %v", row.Tags)
	}
	if value.Value != expectedTime {
		t.Errorf("Metric: %v - Expected %v, got %v", lastRestart, expectedTime, value.Value)
	}

	countRow := checkData(t, totalRestarts, expectedRowLength)
	countValue, ok := countRow.Data.(*view.CountData)
	if !ok {
		t.Fatalf("totalRestarts should have type CountData: %s", reflect.TypeOf(countRow.Data))
	}
	if len(countRow.Tags) != 0 {
		t.Errorf("totalRestarts tags is non-empty, got: %v", row.Tags)
	}
	if countValue.Value != 1 {
		t.Errorf("Metric: %v - Expected %v, got %v", totalRestarts, 1, countValue.Value)
	}

	if err = r.reportRestart(); err != nil {
		t.Errorf("reportRestart error %v", err)
	}

	countRow2 := checkData(t, totalRestarts, expectedRowLength)
	countValue2, ok := countRow2.Data.(*view.CountData)
	if !ok {
		t.Fatalf("totalRestarts should have type CountData: %s", reflect.TypeOf(countRow2.Data))
	}
	if len(countRow2.Tags) != 0 {
		t.Errorf("totalRestarts tags is non-empty, got: %v", row.Tags)
	}
	if countValue2.Value != 2 {
		t.Errorf("Metric: %v - Expected %v, got %v", totalRestarts, 2, countValue2.Value)
	}
}

func TestGauges(t *testing.T) {
	r, err := newStatsReporter()
	if err != nil {
		t.Fatalf("newStatsReporter() error %v", err)
	}
	tc := []struct {
		name string
		fn   func(int64) error
	}{
		{
			name: gvkCount,
			fn:   r.reportGvkCount,
		},
		{
			name: gvkIntentCount,
			fn:   r.reportGvkIntentCount,
		},
		{
			name: isRunning,
			fn:   r.reportIsRunning,
		},
	}
	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			const expectedValue int64 = 10
			const expectedRowLength = 1

			err = tt.fn(expectedValue)
			if err != nil {
				t.Errorf("function error %v", err)
			}
			row := checkData(t, tt.name, expectedRowLength)
			value, ok := row.Data.(*view.LastValueData)
			if !ok {
				t.Errorf("metric %s should have aggregation LastValue()", tt.name)
			}
			if len(row.Tags) != 0 {
				t.Errorf("%s tags is non-empty, got: %v", tt.name, row.Tags)
			}
			if int64(value.Value) != expectedValue {
				t.Errorf("Metric: %v - Expected %v, got %v", tt.name, expectedValue, value.Value)
			}
		})
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
