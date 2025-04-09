package device

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	duc = promauto.NewCounter(prometheus.CounterOpts{
		Name: "device_uplink_count",
		Help: "The number of uplinks sent by the devices.",
	})

	djrc = promauto.NewCounter(prometheus.CounterOpts{
		Name: "device_join_request_count",
		Help: "The number of join-requests sent by the devices.",
	})

	djac = promauto.NewCounter(prometheus.CounterOpts{
		Name: "device_join_accept_count",
		Help: "The number of join-accepts received by the devices.",
	})
)

func deviceUplinkCounter() prometheus.Counter {
	return duc
}

func deviceJoinRequestCounter() prometheus.Counter {
	return djrc
}

func deviceJoinAcceptCounter() prometheus.Counter {
	return djac
}
