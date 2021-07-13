package mutation

import (
	"testing"
	"time"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	expectedDurationValueMin         = time.Duration(1 * time.Second)
	expectedDurationValueMax         = time.Duration(5 * time.Second)
	expectedDurationMin      float64 = 1
	expectedDurationMax      float64 = 5
	expectedMutators                 = 3
	expectedRowLength                = 1
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

	// Count test
	row := checkData(t, mutatorIngestionCountMetricName, expectedRowLength)
	count, ok := row.Data.(*view.CountData)
	if !ok {
		t.Error("ReportRequest should have aggregation Count()")
	}
	if count.Value != 2 {
		t.Errorf("Metric: %v - Expected %v, got %v. ", mutatorIngestionCountMetricName, 2, count.Value)
	}

	verifyTags(t, expectedTags, row.Tags)

	// Duration test
	row = checkData(t, mutatorIngestionDurationMetricName, expectedRowLength)
	durationValue, ok := row.Data.(*view.DistributionData)
	if !ok {
		t.Error("ReportRequest should have aggregation Distribution()")
	}
	if durationValue.Min != expectedDurationMin {
		t.Errorf("Metric: %v - Expected %v, got %v. ", mutatorIngestionDurationMetricName, expectedDurationMin, durationValue.Min)
	}
	if durationValue.Max != expectedDurationMax {
		t.Errorf("Metric: %v - Expected %v, got %v. ", mutatorIngestionDurationMetricName, expectedDurationMax, durationValue.Max)
	}

	verifyTags(t, expectedTags, row.Tags)
}

func TestReportMutatorsStatus(t *testing.T) {
	r, err := newStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}

	// Set to one set of values and then to another.  Prove that the view reflects the most up
	// to date version of the data.

	// 4 active, 4 error
	err = r.reportMutatorsStatus(MutatorStatusActive, 4)
	if err != nil {
		t.Errorf("reportMutatorsStatus error: %v", err)
	}
	err = r.reportMutatorsStatus(MutatorStatusError, 4)
	if err != nil {
		t.Errorf("reportMutatorsStatus error: %v", err)
	}
	// 5 active, 3 error
	err = r.reportMutatorsStatus(MutatorStatusActive, 5)
	if err != nil {
		t.Errorf("reportMutatorsStatus error: %v", err)
	}
	err = r.reportMutatorsStatus(MutatorStatusError, 3)
	if err != nil {
		t.Errorf("reportMutatorsStatus error: %v", err)
	}

	data, err := view.RetrieveData(mutatorsMetricName)
	if err != nil {
		t.Errorf("Error when retrieving data: %v from %v", err, mutatorsMetricName)
	}

	l := len(data)
	if l != 2 {
		t.Errorf("Expected '%v' view to have length %v, got %v", mutatorsMetricName, 2, l)
	}

	verifyRow(t, data, MutatorStatusActive, 5)
	verifyRow(t, data, MutatorStatusError, 3)
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

func verifyTags(t *testing.T, expected map[string]string, actual []tag.Tag) {
	for _, tag := range actual {
		ex := expected[tag.Key.Name()]
		if tag.Value != ex {
			t.Errorf("Expected tag '%v' to have value '%v' but found '%v'", tag.Key.Name(), ex, tag.Value)
		}
	}
}

func verifyRow(t *testing.T, rows []*view.Row, tag MutatorStatus, expectedValue int) {
	for _, r := range rows {
		if !hasTag(r, mutatorStatusKey.Name(), string(tag)) {
			continue
		}

		lastValueData, ok := r.Data.(*view.LastValueData)
		if !ok {
			t.Errorf("Data is not of type *view.LastValueData")
		}

		if int(lastValueData.Value) != expectedValue {
			t.Errorf("Expected value '%v' for tag '%v' but received '%v'", expectedValue, tag, lastValueData.Value)
		}

		return
	}

	t.Errorf("Expected to find row with tag '%v' but none were found", tag)
}

func hasTag(row *view.Row, key, value string) bool {
	for _, tag := range row.Tags {
		if tag.Key.Name() == key && tag.Value == value {
			return true
		}
	}

	return false
}
