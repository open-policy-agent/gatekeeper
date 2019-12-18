package webhook

import (
	"testing"
	"time"

	"go.opencensus.io/stats/view"
)

func TestReportRequest(t *testing.T) {
	expectedTags := map[string]string{
		"admission_status": "allow",
	}
	const expectedDurationValueMin = time.Duration(1 * time.Second)
	const expectedDurationValueMax = time.Duration(5 * time.Second)
	const expectedDurationMin float64 = 1
	const expectedDurationMax float64 = 5
	const expectedCount int64 = 2
	const expectedRowLength = 1

	r, err := newStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}
	err = r.ReportRequest(allowResponse, expectedDurationValueMin)
	if err != nil {
		t.Errorf("ReportRequest error %v", err)
	}
	err = r.ReportRequest(allowResponse, expectedDurationValueMax)
	if err != nil {
		t.Errorf("ReportRequest error %v", err)
	}

	// count test
	row := checkData(t, requestCountName, expectedRowLength)
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
		t.Errorf("Metric: %v - Expected %v, got %v. ", requestCountName, count.Value, expectedCount)
	}

	// Duration test
	row = checkData(t, requestDurationName, expectedRowLength)
	DurationValue, ok := row.Data.(*view.DistributionData)
	if !ok {
		t.Error("ReportRequest should have aggregation Distribution()")
	}
	for _, tag := range row.Tags {
		if tag.Value != expectedTags[tag.Key.Name()] {
			t.Errorf("ReportRequest tags does not match for %v", tag.Key.Name())
		}
	}
	if DurationValue.Min != expectedDurationMin {
		t.Errorf("Metric: %v - Expected %v, got %v. ", requestDurationName, DurationValue.Min, expectedDurationMin)
	}
	if DurationValue.Max != expectedDurationMax {
		t.Errorf("Metric: %v - Expected %v, got %v. ", requestDurationName, DurationValue.Max, expectedDurationMax)
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
