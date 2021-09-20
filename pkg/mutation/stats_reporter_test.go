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
		t.Fatalf("ReportIterationConvergence error: %v", err)
	}

	err = r.ReportIterationConvergence(SystemConvergenceFalse, failureMax)
	if err != nil {
		t.Fatalf("ReportIterationConvergence error: %v", err)
	}

	err = r.ReportIterationConvergence(SystemConvergenceTrue, successMin)
	if err != nil {
		t.Fatalf("ReportIterationConvergence error: %v", err)
	}

	rows, err := view.RetrieveData(mutationSystemIterationsMetricName)
	if err != nil {
		t.Fatalf("Error when retrieving data: %v from %v", err, mutationSystemIterationsMetricName)
	}

	wantIterations := 2

	if gotIterations := len(rows); gotIterations != wantIterations {
		t.Errorf("got %q iterations %v, want %v",
			mutationSystemIterationsMetricName, gotIterations, wantIterations)
	}

	err = verifyDistributionRow(rows, SystemConvergenceTrue, 2, successMin, successMax)
	if err != nil {
		t.Error(err)
	}

	err = verifyDistributionRow(rows, SystemConvergenceFalse, 1, failureMin, failureMax)
	if err != nil {
		t.Error(err)
	}
}

func getRow(rows []*view.Row, tag SystemConvergenceStatus) *view.Row {
	for _, row := range rows {
		if !hasTag(row, systemConvergenceKey.Name(), string(tag)) {
			continue
		}

		return row
	}

	return nil
}

func verifyDistributionRow(rows []*view.Row, tag SystemConvergenceStatus, count int64, min, max float64) error {
	row := getRow(rows, tag)
	if row == nil {
		return fmt.Errorf("got no rows with tag %q", tag)
	}

	distData, ok := row.Data.(*view.DistributionData)
	if !ok {
		return fmt.Errorf("got row Data type %T, want type %T", row.Data, &view.DistributionData{})
	}

	if distData.Count != count {
		return fmt.Errorf("got tag %q count %v, want %v", tag, distData.Count, count)
	}

	if distData.Min != min {
		return fmt.Errorf("got tag %q min %v, want %v", tag, distData.Min, min)
	}

	if distData.Max != max {
		return fmt.Errorf("got tag %q max %v, want %v", tag, distData.Max, max)
	}

	return nil
}

func hasTag(row *view.Row, key, value string) bool {
	for _, tag := range row.Tags {
		if tag.Key.Name() == key && tag.Value == value {
			return true
		}
	}

	return false
}
