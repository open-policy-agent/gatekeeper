package sync

import (
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

func TestReportSync(t *testing.T) {
	const wantValue = 10
	const wantRowLength = 1
	wantTags := Tags{
		Kind:   "Pod",
		Status: metrics.ActiveStatus,
	}

	r, err := NewStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}

	err = r.reportSync(wantTags, wantValue)
	if err != nil {
		t.Fatalf("got reportSync() error %v", err)
	}

	row := checkData(t, syncMetricName, wantRowLength)
	gotLast, ok := row.Data.(*view.LastValueData)
	if !ok {
		t.Fatalf("got %q type %T, want %T", syncMetricName, row.Data, &view.LastValueData{})
	}

	found := contains(row.Tags, wantTags.Kind)
	if !found {
		t.Errorf("reportSync tags %+v does not contain %q", row.Tags, wantTags.Kind)
	}

	found = contains(row.Tags, string(wantTags.Status))
	if !found {
		t.Errorf("reportSync tags %+v does not contain %v", row.Tags, wantTags.Status)
	}

	if gotLast.Value != wantValue {
		t.Errorf("got %v = %v, want %v", syncMetricName, gotLast.Value, wantValue)
	}
}

func TestReportSyncLatency(t *testing.T) {
	const minLatency = 100 * time.Second
	const maxLatency = 500 * time.Second

	const wantLatencyCount int64 = 2
	const wantLatencyMin float64 = 100
	const wantLatencyMax float64 = 500
	const wantRowLength int = 1

	r, err := NewStatsReporter()
	if err != nil {
		t.Fatalf("got newStatsReporter() error %v, want nil", err)
	}

	err = r.reportSyncDuration(minLatency)
	if err != nil {
		t.Fatalf("got reportSyncDuration() error %v, want nil", err)
	}

	err = r.reportSyncDuration(maxLatency)
	if err != nil {
		t.Fatalf("got reportSyncDuration error %v, want nil", err)
	}

	row := checkData(t, syncDurationMetricName, wantRowLength)
	gotLatency, ok := row.Data.(*view.DistributionData)
	if !ok {
		t.Fatalf("got %q type %T, want %T", syncDurationMetricName, row.Data, &view.DistributionData{})
	}

	if gotLatency.Count != wantLatencyCount {
		t.Errorf("got %q = %v, want %v", syncDurationMetricName, gotLatency.Count, wantLatencyCount)
	}

	if gotLatency.Min != wantLatencyMin {
		t.Errorf("got %q = %v, want %v", syncDurationMetricName, gotLatency.Min, wantLatencyMin)
	}

	if gotLatency.Max != wantLatencyMax {
		t.Errorf("got %q = %v, want %v", syncDurationMetricName, gotLatency.Max, wantLatencyMax)
	}
}

func TestLastRunSync(t *testing.T) {
	const wantTime float64 = 11
	const wantRowLength = 1

	fakeNow := func() float64 {
		return wantTime
	}

	r, err := NewStatsReporter()
	if err != nil {
		t.Fatalf("got NewStatsReporter() error %v, want nil", err)
	}

	r.now = fakeNow
	err = r.reportLastSync()
	if err != nil {
		t.Fatalf("got reportLastSync() error %v, want nil", err)
	}

	row := checkData(t, lastRunTimeMetricName, wantRowLength)
	gotLast, ok := row.Data.(*view.LastValueData)
	if !ok {
		t.Fatalf("got %q type %T, want %T", lastRunTimeMetricName, row.Data, &view.LastValueData{})
	}

	if len(row.Tags) != 0 {
		t.Errorf("reportRestartCheck tags is non-empty, got: %v", row.Tags)
	}

	if gotLast.Value != wantTime {
		t.Errorf("got %q = %v, want %v", lastRunTimeMetricName, gotLast.Value, wantTime)
	}
}

func contains(s []tag.Tag, e string) bool {
	for _, a := range s {
		if a.Value == e {
			return true
		}
	}
	return false
}

func checkData(t *testing.T, name string, wantRowLength int) *view.Row {
	row, err := view.RetrieveData(name)
	if err != nil {
		t.Fatalf("got %v RetrieveData() error = %v", name, err)
	}

	if len(row) != wantRowLength {
		t.Fatalf("got row length %v, want %v", len(row), wantRowLength)
	}

	if row[0].Data == nil {
		t.Fatalf("got row[0].Data = nil, want non-nil")
	}
	return row[0]
}
