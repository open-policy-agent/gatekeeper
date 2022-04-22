// Package registry provides a dynamic registry of available exporters.
// this makes it easy for forks to inject new metrics exporters as-needed.
package registry

import (
	"context"
	"flag"
	"fmt"
	"strings"

	// register exporters with the registry.
	"github.com/open-policy-agent/gatekeeper/pkg/metrics/exporters/opencensus"
	"github.com/open-policy-agent/gatekeeper/pkg/metrics/exporters/prometheus"
	"github.com/open-policy-agent/gatekeeper/pkg/metrics/exporters/stackdriver"
)

func init() {
	flag.Var(exporters, "metrics-backend", "Backend used for metrics. e.g. `prometheus`, `stackdriver`. This flag can be declared more than once. Omitting will default to supporting `prometheus`.")
}

type Exporter interface {
	// Start starts the exporter behavior. If Start()
	// returns an error, that is bubbled up to the
	// controller manager
	Start(context.Context) error
}

var exporters = newExporterSet(
	map[string]StartExporter{
		opencensus.Name:  opencensus.Start,
		prometheus.Name:  prometheus.Start,
		stackdriver.Name: stackdriver.Start,
	},
)

type StartExporter func(context.Context) error

type exporterSet struct {
	validExporters      []string
	registeredExporters map[string]StartExporter
	assignedExporters   map[string]StartExporter
}

var _ flag.Value = &exporterSet{}

func newExporterSet(exporters map[string]StartExporter) *exporterSet {
	registered := make(map[string]StartExporter)
	assigned := make(map[string]StartExporter)
	set := &exporterSet{
		validExporters:      []string{},
		registeredExporters: registered,
		assignedExporters:   assigned,
	}
	for name := range exporters {
		set.MustRegister(name, exporters[name])
	}
	return set
}

func (es *exporterSet) String() string {
	contents := make([]string, 0)
	for k := range es.assignedExporters {
		contents = append(contents, string(k))
	}
	return fmt.Sprintf("%s", contents)
}

func (es *exporterSet) Set(s string) error {
	splt := strings.Split(s, ",")
	for _, v := range splt {
		lower := strings.ToLower(v)
		new, ok := es.registeredExporters[lower]
		if !ok {
			return fmt.Errorf("exporter %s is not a valid exporter: %v", v, es.validExporters)
		}
		es.assignedExporters[lower] = new
	}
	return nil
}

func (es *exporterSet) MustRegister(name string, new StartExporter) {
	if _, ok := es.registeredExporters[name]; ok {
		panic(fmt.Sprintf("exporter %v registered twice", name))
	}
	es.registeredExporters[name] = new
	es.validExporters = append(es.validExporters, name)
}

func Exporters() []StartExporter {
	if len(exporters.assignedExporters) == 0 {
		newProm, ok := exporters.registeredExporters[prometheus.Name]
		if !ok {
			panic("prometheus exporter not registered, cannot use default exporter")
		}
		return []StartExporter{newProm}
	}
	ret := make([]StartExporter, 0, len(exporters.assignedExporters))
	for _, new := range exporters.assignedExporters {
		ret = append(ret, new)
	}
	return ret
}
