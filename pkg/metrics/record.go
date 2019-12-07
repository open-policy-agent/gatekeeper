package metrics

import (
	"context"

	"go.opencensus.io/stats"
)

func Record(ctx context.Context, ms stats.Measurement, ros ...stats.Options) error {
	ros = append(ros, stats.WithMeasurements(ms))
	return stats.RecordWithOptions(ctx, ros...)
}
