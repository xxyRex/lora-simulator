package gateway

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	guc = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gateway_uplink_count",
		Help: "The number of uplinks sent by the gateways.",
	})

	gdc = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gateway_downlink_count",
		Help: "The number of downlinks received by the gateways.",
	})
)

func gatewayUplinkCounter() prometheus.Counter {
	return guc
}

func gatewayDownlinkCounter() prometheus.Counter {
	return gdc
}
