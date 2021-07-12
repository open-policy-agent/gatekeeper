package mutation

import (
	"testing"
	"time"

	"go.opencensus.io/stats/view"
)

const (
	expectedDurationValueMin         = time.Duration(1 * time.Second)
	expectedDurationValueMax         = time.Duration(5 * time.Second)
	expectedDurationMin      float64 = 1
	expectedDurationMax      float64 = 5
	expectedMutators                 = 3
	expectedRowLength                = 1
	expectedCount            int64   = 2
)

func TestReportMutatorIngestionRequest(t *testing.T) {
	expectedTags := map[string]string{
		"status": "active",
	}

	r, err := newStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}
	err = r.reportMutatorIngestionRequest(MutatorStatusActive, expectedDurationValueMin)
	if err != nil {
		t.Errorf("ReportRequest error %v", err)
	}
	err = r.reportMutatorIngestionRequest(MutatorStatusActive, expectedDurationValueMax)
	if err != nil {
		t.Errorf("ReportRequest error %v", err)
	}

	// from the "check" function

	// count test
	row := checkData(t, mutatorIngestionCountMetricName, expectedRowLength)

	count, ok := row.Data.(*view.CountData)
	if !ok {
		t.Error("ReportRequest should have aggregation Count()")
	}
	for _, tag := range row.Tags {
		expected := expectedTags[tag.Key.Name()]
		if tag.Value != expected {
			t.Errorf("Expected tag '%v' to have value '%v' but found '%v'", tag.Key.Name(), expected, tag.Value)
		}
	}
	if count.Value != expectedCount {
		t.Errorf("Metric: %v - Expected %v, got %v. ", mutatorIngestionCountMetricName, expectedCount, count.Value)
	}

	// Duration test
	row = checkData(t, mutatorIngestionDurationMetricName, expectedRowLength)
	durationValue, ok := row.Data.(*view.DistributionData)
	if !ok {
		t.Error("ReportRequest should have aggregation Distribution()")
	}
	for _, tag := range row.Tags {
		expected := expectedTags[tag.Key.Name()]
		if tag.Value != expected {
			t.Errorf("Expected tag '%v' to have value '%v' but found '%v'", tag.Key.Name(), expected, tag.Value)
		}
	}
	if durationValue.Min != expectedDurationMin {
		t.Errorf("Metric: %v - Expected %v, got %v. ", mutatorIngestionDurationMetricName, expectedDurationMin, durationValue.Min)
	}
	if durationValue.Max != expectedDurationMax {
		t.Errorf("Metric: %v - Expected %v, got %v. ", mutatorIngestionDurationMetricName, expectedDurationMax, durationValue.Max)
	}
}

func checkData(t *testing.T, name string, expectedRowLength int) *view.Row {
	row, err := view.RetrieveData(name)
	if err != nil {
		t.Errorf("Error when retrieving data: %v from %v", err, name)
	}
	if len(row) != expectedRowLength {
		t.Errorf("Expected '%v' row to have length %v, got %v", name, expectedRowLength, len(row))
	}
	if row[0].Data == nil {
		t.Errorf("Expected row data not to be nil")
	}
	return row[0]
}
