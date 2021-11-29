package webhook

import (
	"context"
	"testing"
	"time"

	"go.opencensus.io/stats/view"
)

const (
	minValidationDuration = 1 * time.Second
	maxValidationDuration = 5 * time.Second

	wantMinValidationSeconds float64 = 1
	wantMaxValidationSeconds float64 = 5

	wantCount     int64 = 2
	wantRowLength int   = 1
)

func TestValidationReportRequest(t *testing.T) {
	wantTags := map[string]string{
		"admission_status": "allow",
	}

	ctx := context.Background()
	r, err := newStatsReporter()
	if err != nil {
		t.Fatalf("got newStatsReporter() error %v, want nil", err)
	}

	err = r.ReportValidationRequest(ctx, allowResponse, minValidationDuration)
	if err != nil {
		t.Fatalf("got ReportValidationRequest() error = %v, want nil", err)
	}

	err = r.ReportValidationRequest(ctx, allowResponse, maxValidationDuration)
	if err != nil {
		t.Fatalf("got ReportValidationRequest() error = %v, want nil", err)
	}

	check(t, wantTags, validationRequestCountMetricName, validationRequestDurationMetricName)
}

func TestMutationReportRequest(t *testing.T) {
	wantTags := map[string]string{
		"mutation_status": "success",
	}

	ctx := context.Background()
	r, err := newStatsReporter()
	if err != nil {
		t.Fatalf("got newStatsReporter() error %v, want nil", err)
	}

	err = r.ReportMutationRequest(ctx, successResponse, minValidationDuration)
	if err != nil {
		t.Fatalf("got ReportMutationRequest error %v, want nil", err)
	}

	err = r.ReportMutationRequest(ctx, successResponse, maxValidationDuration)
	if err != nil {
		t.Fatalf("got ReportRequest error %v, want nil", err)
	}

	check(t, wantTags, mutationRequestCountMetricName, mutationRequestDurationMetricName)
}

func check(t *testing.T, wantTags map[string]string, requestCountMetricName string, requestDurationMetricName string) {
	// count test
	row := checkData(t, requestCountMetricName, wantRowLength)

	gotCount, ok := row.Data.(*view.CountData)
	if !ok {
		t.Fatalf("got %q type %T, want %T", requestCountMetricName, row.Data, &view.CountData{})
	}
	for _, gotTag := range row.Tags {
		tagName := gotTag.Key.Name()
		want := wantTags[tagName]
		if gotTag.Value != want {
			t.Errorf("got tag %q value %q, want %q", tagName, gotTag.Value, want)
		}
	}

	if gotCount.Value != wantCount {
		t.Errorf("got %q = %v, want %v", requestCountMetricName, gotCount.Value, wantCount)
	}

	// Duration test
	row = checkData(t, requestDurationMetricName, wantRowLength)
	gotDuration, ok := row.Data.(*view.DistributionData)
	if !ok {
		t.Fatalf("got %q type %T, want %T", requestDurationMetricName, row.Data, &view.DistributionData{})
	}

	for _, gotTag := range row.Tags {
		tagName := gotTag.Key.Name()
		want := wantTags[tagName]
		if gotTag.Value != want {
			t.Errorf("got tag %q value %q, want %q", tagName, gotTag.Value, want)
		}
	}

	if gotDuration.Min != wantMinValidationSeconds {
		t.Errorf("got %q min = %v, want %v", requestDurationMetricName, gotDuration.Min, wantMinValidationSeconds)
	}

	if gotDuration.Max != wantMaxValidationSeconds {
		t.Errorf("got %q max = %v, want %v", requestDurationMetricName, gotDuration.Max, wantMaxValidationSeconds)
	}
}

func checkData(t *testing.T, name string, wantRowLength int) *view.Row {
	row, err := view.RetrieveData(name)
	if err != nil {
		t.Fatalf("got RetrieveData(%q) error %v, want nil", name, err)
	}

	if len(row) != wantRowLength {
		t.Fatalf("got row length %v, want %v", len(row), wantRowLength)
	}

	if row[0].Data == nil {
		t.Fatalf("got row[0].Data = nil, want non-nil")
	}
	return row[0]
}
