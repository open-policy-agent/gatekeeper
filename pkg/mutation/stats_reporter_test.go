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

func TestReportMutatorIngestion(t *testing.T) {
	expectedTags := map[string]string{
		"status": "active",
	}

	r, err := newStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}
	err = r.reportMutatorIngestion(MutatorStatusActive, expectedDurationValueMin, expectedMutators)
	if err != nil {
		t.Errorf("ReportRequest error %v", err)
	}
	err = r.reportMutatorIngestion(MutatorStatusActive, expectedDurationValueMax, expectedMutators)
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
		if tag.Value != expectedTags[tag.Key.Name()] {
			t.Errorf("ReportRequest tags does not match for %v", tag.Key.Name())
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
		if tag.Value != expectedTags[tag.Key.Name()] {
			t.Errorf("ReportRequest tags does not match for %v", tag.Key.Name())
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
		t.Errorf("Expected length %v, got %v", expectedRowLength, len(row))
	}
	if row[0].Data == nil {
		t.Errorf("Expected row data not to be nil")
	}
	return row[0]
}
