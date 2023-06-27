// Package mevshare implements mev share node
// Here is a full flow of data through the node:
//
// bundle-relay-api -> API sends:
//   - private transaction
//   - mev share bundle
//
// API -> SimQueue schedules simulation
// SimQueue -> SimulationWorker calls with the next simulation to process
//
//	SimulationWorker -> SimulationBackend is used to do simulation
//	SimulationWorker -> SimulationResultBackend is used to consume simulation result
//
// SimulationResultBackend -> RedisHintBackend is used to send hint to hint-relay-api
// SimulationResultBackend -> BuilderBackend is used to send bundle to the builders
package mevshare

const (
	MaxBlockOffset uint64 = 5
	MaxBlockRange  uint64 = 30
	MaxBodySize           = 50

	MaxNestingLevel = 1

	RefundPercent = 90

	HintGasPriceNumberPrecisionDigits = 3
	HintGasNumberPrecisionDigits      = 2
)
