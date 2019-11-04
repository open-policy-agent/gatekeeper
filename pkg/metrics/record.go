package metrics

import (
	"context"

	"go.opencensus.io/stats"
)

func Record(ctx context.Context, ms stats.Measurement, ros ...stats.Options) {
	ros = append(ros, stats.WithMeasurements(ms))

	stats.RecordWithOptions(ctx, ros...)
}
