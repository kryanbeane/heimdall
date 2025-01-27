package controllers

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var WatchingResourcesCount = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "heimdall_watching_resource_count",
	Help: "Number of resources being watched by Heimdall",
})

func NewMetrics() {
	metrics.Registry.MustRegister(WatchingResourcesCount)
}
