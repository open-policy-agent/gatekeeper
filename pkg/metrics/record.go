package metrics

import (
	"context"

	"go.opencensus.io/stats"
)

func Record(ctx context.Context, ms stats.Measurement, ros ...stats.Options) {
	ros = append(ros, stats.WithMeasurements(ms))

	if err := stats.RecordWithOptions(ctx, ros...); err != nil {
		log.Error(err, "failed to record metric")
	}
}
