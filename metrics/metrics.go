// Package metrics contains all application-logic metrics
package metrics

import (
	"fmt"

	"github.com/VictoriaMetrics/metrics"
)

var (
	sbundlesReceived              = metrics.NewCounter("sbundles_received_total")
	sbundlesReceivedValid         = metrics.NewCounter("sbundles_received_valid_total")
	sbundlesReceivedStale         = metrics.NewCounter("sbundles_received_stale_total")
	queueFullSbundles             = metrics.NewCounter("sbundles_queue_full_total")
	queuePopstaleItemSbundles     = metrics.NewCounter("sbundles_queue_pop_stale_item_total")
	sbundleProcessDurationSummary = metrics.NewSummary("sbundle_process_duration_milliseconds")
)

const (
	sbundleRPCCallDurationLabel     = `sbundle_rpc_call_duration_milliseconds{method="%s"}`
	sbundleRPCCallErrorCounterLabel = `sbundle_rpc_call_error_total{method="%s"}`

	sbundleSentToBuilderLabel                = `bundle_sent_to_builder_total{builder="%s"}`
	sbundleSentToBuilderFailureLabel         = `bundle_sent_to_builder_failure_total{builder="%s"}`
	sbundleSentToBuilderDurationSummaryLabel = `bundle_sent_to_builder_duration_milliseconds{builder="%s"}`
)

func RecordRPCCallDuration(method string, duration int64) {
	l := fmt.Sprintf(sbundleRPCCallDurationLabel, method)
	metrics.GetOrCreateSummary(l).Update(float64(duration))
}

func IncRPCCallFailure(method string) {
	l := fmt.Sprintf(sbundleRPCCallErrorCounterLabel, method)
	metrics.GetOrCreateCounter(l).Inc()
}

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

func IncBundleSentToBuilder(builder string) {
	l := fmt.Sprintf(sbundleSentToBuilderLabel, builder)
	metrics.GetOrCreateCounter(l).Inc()
}

func IncBundleSentToBuilderFailure(builder string) {
	l := fmt.Sprintf(sbundleSentToBuilderFailureLabel, builder)
	metrics.GetOrCreateCounter(l).Inc()
}

func RecordBundleSentToBuilderTime(endpoint string, duration int64) {
	l := fmt.Sprintf(sbundleSentToBuilderDurationSummaryLabel, endpoint)
	metrics.GetOrCreateSummary(l).Update(float64(duration))
}

func RecordBundleProcessDuration(duration int64) {
	sbundleProcessDurationSummary.Update(float64(duration))
}
