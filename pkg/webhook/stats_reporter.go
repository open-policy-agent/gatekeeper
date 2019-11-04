package webhook

import (
	"context"
	"strconv"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
)

const (
	requestCountName     = "request_count"
	requestLatenciesName = "request_latencies"
)

var (
	requestCountM = stats.Int64(
		requestCountName,
		"The number of requests that are routed to webhook",
		stats.UnitDimensionless)
	responseTimeInMsecM = stats.Float64(
		requestLatenciesName,
		"The response time in milliseconds",
		stats.UnitMilliseconds)

	requestOperationKey  = tag.MustNewKey("request_operation")
	kindGroupKey         = tag.MustNewKey("kind_group")
	kindVersionKey       = tag.MustNewKey("kind_version")
	kindKindKey          = tag.MustNewKey("kind_kind")
	resourceGroupKey     = tag.MustNewKey("resource_group")
	resourceVersionKey   = tag.MustNewKey("resource_version")
	resourceResourceKey  = tag.MustNewKey("resource_resource")
	resourceNameKey      = tag.MustNewKey("resource_name")
	resourceNamespaceKey = tag.MustNewKey("resource_namespace")
	admissionAllowedKey  = tag.MustNewKey("admission_allowed")
)

func init() {
	register()
}

// StatsReporter reports webhook metrics
type StatsReporter interface {
	ReportRequest(request *admissionv1beta1.AdmissionRequest, response *admissionv1beta1.AdmissionResponse, d time.Duration) error
}

// reporter implements StatsReporter interface
type reporter struct {
	ctx context.Context
}

// NewStatsReporter creaters a reporter for webhook metrics
func NewStatsReporter() (StatsReporter, error) {
	ctx, err := tag.New(
		context.Background(),
	)
	if err != nil {
		return nil, err
	}

	return &reporter{ctx: ctx}, nil
}

// Captures req count metric, recording the count and the duration
func (r *reporter) ReportRequest(req *admissionv1beta1.AdmissionRequest, resp *admissionv1beta1.AdmissionResponse, d time.Duration) error {
	ctx, err := tag.New(
		r.ctx,
		tag.Insert(requestOperationKey, string(req.Operation)),
		tag.Insert(kindGroupKey, req.Kind.Group),
		tag.Insert(kindVersionKey, req.Kind.Version),
		tag.Insert(kindKindKey, req.Kind.Kind),
		tag.Insert(resourceGroupKey, req.Resource.Group),
		tag.Insert(resourceVersionKey, req.Resource.Version),
		tag.Insert(resourceResourceKey, req.Resource.Resource),
		tag.Insert(resourceNameKey, req.Name),
		tag.Insert(resourceNamespaceKey, req.Namespace),
		tag.Insert(admissionAllowedKey, strconv.FormatBool(resp.Allowed)),
	)
	if err != nil {
		return err
	}

	r.report(ctx, requestCountM.M(1))
	// Convert time.Duration in nanoseconds to milliseconds
	r.report(ctx, responseTimeInMsecM.M(float64(d/time.Millisecond)))
	return nil
}

func (r *reporter) report(ctx context.Context, m stats.Measurement) error {
	metrics.Record(ctx, m)
	return nil
}

func register() {
	tagKeys := []tag.Key{
		requestOperationKey,
		kindGroupKey,
		kindVersionKey,
		kindKindKey,
		resourceGroupKey,
		resourceVersionKey,
		resourceResourceKey,
		resourceNamespaceKey,
		resourceNameKey,
		admissionAllowedKey}

	if err := view.Register(
		&view.View{
			Description: requestCountM.Description(),
			Measure:     requestCountM,
			Aggregation: view.Count(),
			TagKeys:     tagKeys,
		},
		&view.View{
			Description: responseTimeInMsecM.Description(),
			Measure:     responseTimeInMsecM,
			Aggregation: view.Distribution(1000, 2000, 3000, 4000, 5000, 6000, 7000, 8000, 9000, 10000, 11000, 12000, 13000, 14000, 15000),
			TagKeys:     tagKeys,
		},
	); err != nil {
		panic(err)
	}
}
