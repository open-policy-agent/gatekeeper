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
	row, err := checkData(mutatorIngestionCountMetricName, wantRowLength)
	if err != nil {
		t.Fatal(err)
	}

	gotCount, ok := row.Data.(*view.CountData)
	if !ok {
		t.Fatalf("got %q type %T, want %T", mutatorIngestionCountMetricName, row.Data, &view.CountData{})
	}

	if gotCount.Value != wantIngestionCount {
		t.Errorf("got %q = %v, want %v", mutatorIngestionCountMetricName, gotCount.Value, wantIngestionCount)
	}

	verifyTags(t, wantTags, row.Tags)

	// Duration test
	row, err = checkData(mutatorIngestionDurationMetricName, wantRowLength)
	if err != nil {
		t.Error(err)
	}

	durationValue, ok := row.Data.(*view.DistributionData)
	if !ok {
		t.Fatalf("got %q type %T, want %T", mutatorIngestionCountMetricName, row.Data, &view.DistributionData{})
	}

	if durationValue.Min != wantMinIngestionDuration {
		t.Errorf("got tag %q min %v, want %v", mutatorIngestionDurationMetricName, durationValue.Min, wantMinIngestionDuration)
	}

	if durationValue.Max != wantMaxIngestionDuration {
		t.Errorf("got tag %q max %v, want %v", mutatorIngestionDurationMetricName, durationValue.Max, wantMaxIngestionDuration)
	}

	verifyTags(t, wantTags, row.Tags)
}

func checkData(name string, rowLength int) (*view.Row, error) {
	row, err := view.RetrieveData(name)
	if err != nil {
		return nil, fmt.Errorf("got RetrieveData error %v from %v, want nil", err, name)
	}

	if len(row) != rowLength {
		return nil, fmt.Errorf("got %q row length %v, want %v", name, len(row), rowLength)
	}

	if row[0].Data == nil {
		return nil, fmt.Errorf("got row[0].Data = nil, want non-nil")
	}
	return row[0], nil
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
