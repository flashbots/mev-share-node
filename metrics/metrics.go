// Package metrics contains all application-logic metrics
package metrics

import "github.com/VictoriaMetrics/metrics"

var (
	sbundlesReceived          = metrics.NewCounter("sbundles_received_total")
	sbundlesReceivedValid     = metrics.NewCounter("sbundles_received_valid_total")
	sbundlesReceivedStale     = metrics.NewCounter("sbundles_received_stale_total")
	queueFullSbundles         = metrics.NewCounter("sbundles_queue_full_total")
	queuePopstaleItemSbundles = metrics.NewCounter("sbundles_queue_pop_stale_item_total")
)

func IncSbundlesReceived() {
	sbundlesReceived.Inc()
}

func IncSbundlesReceivedValid() {
	sbundlesReceivedValid.Inc()
}

func IncSbundlesReceivedStale() {
	sbundlesReceivedStale.Inc()
}

func IncQueueFullSbundles() {
	queueFullSbundles.Inc()
}

func IncQueuePopStaleItemSbundles() {
	queuePopstaleItemSbundles.Inc()
}
