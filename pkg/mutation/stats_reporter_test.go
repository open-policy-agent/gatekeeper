package mutation

import (
	"fmt"
	"testing"

	"go.opencensus.io/stats/view"
)

func TestReportIterationConvergence(t *testing.T) {
	const (
		successMax = 5
		successMin = 3
		failureMax = 8
		failureMin = failureMax
	)

	r := NewStatsReporter()

	err := r.ReportIterationConvergence(SystemConvergenceTrue, successMax)
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

	err = verifyDistributionRow(t, rows, SystemConvergenceTrue, 2, successMin, successMax)
	if err != nil {
		t.Error(err)
	}
	err = verifyDistributionRow(t, rows, SystemConvergenceFalse, 1, failureMin, failureMax)
	if err != nil {
		t.Error(err)
	}
}

func verifyDistributionRow(t *testing.T, rows []*view.Row, tag SystemConvergenceStatus, count, min, max int) error {
	for _, r := range rows {
		if !hasTag(r, systemConvergenceKey.Name(), string(tag)) {
			continue
		}

		distData, ok := r.Data.(*view.DistributionData)
		if !ok {
			return fmt.Errorf("Data is not of type *view.DistributionData")
		}

		if int(distData.Count) != count {
			return fmt.Errorf("got tag '%v' count %v, want %v", tag, distData.Count, count)
		}
		if int(distData.Min) != min {
			return fmt.Errorf("got tag '%v' min %v, want %v", tag, distData.Min, min)
		}
		if int(distData.Max) != max {
			return fmt.Errorf("got tag '%v' max %v, want %v", tag, distData.Max, max)
		}

		return nil
	}

	return fmt.Errorf("Expected to find row with tag '%v' but none were found", tag)
}

func hasTag(row *view.Row, key, value string) bool {
	for _, tag := range row.Tags {
		if tag.Key.Name() == key && tag.Value == value {
			return true
		}
	}

	return false
}
