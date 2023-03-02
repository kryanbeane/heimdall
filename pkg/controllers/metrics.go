package controllers

import "github.com/prometheus/client_golang/prometheus"

var watchingResourcesCount = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "heimdall_watching_resource_count",
	Help: "Number of resources being watched by Heimdall",
})

func init() {
	prometheus.MustRegister(watchingResourcesCount)
}
