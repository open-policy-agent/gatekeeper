package common

import (
	"sync"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/view"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

var (
	opts            []metric.Option
	res             *resource.Resource
	mutex           sync.Mutex
	requiredReaders int
)

// SetRequiredReaders sets the number of required readers for the MeterProvider.
func SetRequiredReaders(num int) {
	requiredReaders = num
}

// AddReader adds a reader to the options and updates the MeterProvider if the required conditions are met.
func AddReader(opt metric.Option) {
	mutex.Lock()
	defer mutex.Unlock()
	if opt == nil {
		requiredReaders--
	} else {
		opts = append(opts, opt)
	}
	setMeterProvider()
}

// SetResource sets the resource to be used by the MeterProvider.
func SetResource(r *resource.Resource) {
	mutex.Lock()
	defer mutex.Unlock()
	res = r
}

// setMeterProvider sets the MeterProvider if the required conditions are met.
func setMeterProvider() {
	// Check if we have the required number of readers and at least one reader.
	if len(opts) != requiredReaders || len(opts) == 0 {
		return
	}

	// Start with the existing options.
	options := opts

	// Add views to the options.
	options = append(options, metric.WithView(view.Views()...))

	// If a resource is available, add it to the options.
	if res != nil {
		options = append(options, metric.WithResource(res))
	}

	meterProvider := metric.NewMeterProvider(options...)
	otel.SetMeterProvider(meterProvider)
}
