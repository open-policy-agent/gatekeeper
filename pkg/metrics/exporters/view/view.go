package view

import (
	"go.opentelemetry.io/otel/sdk/metric"
)

var views []metric.View

func init() {
	views = []metric.View{}
}

func Register(v ...metric.View) {
	views = append(views, v...)
}

func Views() []metric.View {
	return views
}
