package mutation

import (
	"testing"

	"go.opencensus.io/stats/view"
)

func TestReportIterationConvergence(t *testing.T) {
	r, err := NewStatsReporter()
	if err != nil {
		t.Errorf("newStatsReporter() error %v", err)
	}

	const (
		successMax = 5
		successMin = 3
		failureMax = 8
		failureMin = failureMax
	)

	err = r.ReportIterationConvergence(SystemConvergenceTrue, successMax)
	if err != nil {
		t.Errorf("ReportIterationConvergence error: %v", err)
	}
	err = r.ReportIterationConvergence(SystemConvergenceFalse, failureMax)
	if err != nil {
		t.Errorf("ReportIterationConvergence error: %v", err)
	}
	err = r.ReportIterationConvergence(SystemConvergenceTrue, successMin)
	if err != nil {
		t.Errorf("ReportIterationConvergence error: %v", err)
	}

	rows, err := view.RetrieveData(mutationSystemIterationsMetricName)
	if err != nil {
		t.Errorf("Error when retrieving data: %v from %v", err, mutationSystemIterationsMetricName)
	}

	validConvergenceStatuses := 2
	l := len(rows)
	if l != validConvergenceStatuses {
		t.Errorf("got '%v' view length %v, want %v", mutationSystemIterationsMetricName, l, validConvergenceStatuses)
	}

	verifyDistributionRow(t, rows, SystemConvergenceTrue, 2, successMin, successMax)
	verifyDistributionRow(t, rows, SystemConvergenceFalse, 1, failureMin, failureMax)
}

func verifyDistributionRow(t *testing.T, rows []*view.Row, tag SystemConvergenceStatus, count, min, max int) {
	for _, r := range rows {
		if !hasTag(r, systemConvergenceKey.Name(), string(tag)) {
			continue
		}

		distData, ok := r.Data.(*view.DistributionData)
		if !ok {
			t.Errorf("Data is not of type *view.DistributionData")
		}

		if int(distData.Count) != count {
			t.Errorf("got tag '%v' count %v, want %v", tag, distData.Count, count)
		}
		if int(distData.Min) != min {
			t.Errorf("got tag '%v' min %v, want %v", tag, distData.Min, min)
		}
		if int(distData.Max) != max {
			t.Errorf("got tag '%v' max %v, want %v", tag, distData.Max, max)
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
