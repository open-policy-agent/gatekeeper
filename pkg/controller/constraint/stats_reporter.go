package constraint

import (
	"context"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	constraintsMetricName = "constraints"
	enforcementActionKey  = "enforcement_action"
	statusKey             = "status"
)

var (
	constraintsM metric.Int64ObservableGauge
	meter        metric.Meter
)

func init() {
	var err error
	meter = otel.GetMeterProvider().Meter("gatekeeper")
	constraintsM, err = meter.Int64ObservableGauge(
		constraintsMetricName,
		metric.WithDescription("Current number of known constraints"))
	if err != nil {
		panic(err)
	}
}

func (c *ConstraintsCache) observeConstraints(ctx context.Context, observer metric.Observer) error {
	c.mux.RLock()
	defer c.mux.RUnlock()
	if c.reportMetrics {
		totals := make(map[tags]int)
		// report total number of constraints
		for _, v := range c.cache {
			totals[v]++
		}

		for _, enforcementAction := range util.KnownEnforcementActions {
			for _, status := range metrics.AllStatuses {
				t := tags{
					enforcementAction: enforcementAction,
					status:            status,
				}
				observer.ObserveInt64(constraintsM, int64(totals[t]), metric.WithAttributes(attribute.String(enforcementActionKey, string(enforcementAction)), attribute.String(statusKey, string(status))))
			}
		}
	}
	return nil
}

// newStatsReporter creates a reporter for audit metrics.
func newStatsReporter() (*reporter, error) {
	return &reporter{}, nil
}

func (rep *reporter) registerCallback(r *ReconcileConstraint) error {
	_, err := meter.RegisterCallback(r.constraintsCache.observeConstraints, constraintsM)
	return err
}

type reporter struct{}
