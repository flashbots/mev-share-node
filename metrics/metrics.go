package metrics

import "github.com/VictoriaMetrics/metrics"

var sbundlesReceived = metrics.NewCounter("sbundles_received_total")
var sbundlesReceivedValid = metrics.NewCounter("sbundles_received_valid_total")
var sbundlesReceivedStale = metrics.NewCounter("sbundles_received_stale_total")

var queueFullSbundles = metrics.NewCounter("sbundles_queue_full_total")
var queuePopstaleItemSbundles = metrics.NewCounter("sbundles_queue_pop_stale_item_total")

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
