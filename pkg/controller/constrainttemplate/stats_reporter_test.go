package constrainttemplate

import (
	"context"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"go.opencensus.io/stats/view"
)

func TestReportIngestion(t *testing.T) {
	if err := reset(); err != nil {
		t.Fatalf("Could not reset stats: %v", err)
	}
	wantTags := map[string]string{
		"status": "active",
	}

	const (
		minIngestDuration = 1 * time.Second
		maxIngestDuration = 5 * time.Second

		wantMinIngestDurationSeconds = 1.0
		wantMaxIngestDurationSeconds = 5.0

		wantCount     = 2
		wantRowLength = 1
	)

	r := newStatsReporter()
	ctx := context.Background()
	err := r.reportIngestDuration(ctx, metrics.ActiveStatus, minIngestDuration)
	if err != nil {
		t.Fatalf("got reportIngestDuration() error %v, want nil", err)
	}

	err = r.reportIngestDuration(ctx, metrics.ActiveStatus, maxIngestDuration)
	if err != nil {
		t.Fatalf("got reportIngestDuration() error %v, want nil", err)
	}

	// count test
	row := checkData(t, ingestCount, wantRowLength)
	gotCount, ok := row.Data.(*view.CountData)
	if !ok {
		t.Fatalf("got %q type %T, want %T", ingestCount, row.Data, &view.CountData{})
	}

	for _, tag := range row.Tags {
		name := tag.Key.Name()
		wantValue := wantTags[name]
		if tag.Value != wantValue {
			t.Errorf("got ingestCount tag %q =  %q, want %q", name, tag.Value, wantValue)
		}
	}

	if gotCount.Value != wantCount {
		t.Errorf("got %q = %v, want %v", ingestCount, gotCount.Value, wantCount)
	}

	// Duration test
	row = checkData(t, ingestDuration, wantRowLength)
	gotDuration, ok := row.Data.(*view.DistributionData)
	if !ok {
		t.Fatalf("got %q type %T, want %T", ingestDuration, row.Data, &view.DistributionData{})
	}

	for _, tag := range row.Tags {
		name := tag.Key.Name()
		wantValue := wantTags[name]
		if tag.Value != wantValue {
			t.Errorf("got tag %q = %q, want %q", name, tag.Value, wantValue)
		}
	}

	if gotDuration.Min != wantMinIngestDurationSeconds {
		t.Errorf("got %q = %v, want %v", ingestDuration, gotDuration.Min, wantMinIngestDurationSeconds)
	}

	if gotDuration.Max != wantMaxIngestDurationSeconds {
		t.Errorf("got %q = %v, want %v", ingestDuration, gotDuration.Max, wantMaxIngestDurationSeconds)
	}
}

func TestGauges(t *testing.T) {
	r := newStatsReporter()

	tcs := []struct {
		name string
		fn   func(context.Context, metrics.Status, int64) error
	}{
		{
			name: ctMetricName,
			fn:   r.reportCtMetric,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			const wantValue = 10
			const wantRowLength = 1
			wantTags := map[string]string{
				"status": "active",
			}

			ctx := context.Background()
			err := tc.fn(ctx, metrics.ActiveStatus, wantValue)
			if err != nil {
				t.Fatalf("function error %v", err)
			}

			row := checkData(t, tc.name, wantRowLength)
			got, ok := row.Data.(*view.LastValueData)
			if !ok {
				t.Fatalf("got metric %q type %T, want %T", wantRowLength, row.Data, &view.LastValueData{})
			}

			if len(row.Tags) != 1 {
				t.Errorf("got %v tags, want: %v", len(row.Tags), len(wantTags))
			}

			for _, tag := range row.Tags {
				name := tag.Key.Name()
				wantTagValue := wantTags[name]
				if tag.Value != wantTagValue {
					t.Errorf("got tag %q = %q, want %q", name, tag.Value, wantValue)
				}
			}

			if int(got.Value) != wantValue {
				t.Errorf("got %v = %v, want %v", tc.name, got.Value, wantValue)
			}
		})
	}
}

func checkData(t *testing.T, name string, wantRowLength int) *view.Row {
	row, err := view.RetrieveData(name)
	if err != nil {
		t.Fatalf("Error when retrieving data: %v from %v", err, name)
	}

	if len(row) != wantRowLength {
		t.Fatalf("got length %v, want %v", len(row), wantRowLength)
	}

	if row[0].Data == nil {
		t.Fatalf("got row[0].Data = nil, want non-nil")
	}

	return row[0]
}
