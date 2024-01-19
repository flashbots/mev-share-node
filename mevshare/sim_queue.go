package mevshare

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/ethereum/go-ethereum/common"
	"github.com/flashbots/mev-share-node/metrics"
	"github.com/flashbots/mev-share-node/simqueue"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

var (
	consumeSimulationTimeout = 5 * time.Second
	simCacheTimeout          = 1 * time.Second
)

type SimQueue struct {
	log            *zap.Logger
	queue          simqueue.Queue
	eth            EthClient
	workers        []SimulationWorker
	workersPerNode int
}

func NewQueue(
	log *zap.Logger, queue simqueue.Queue, eth EthClient, sim []SimulationBackend, simRes SimulationResult,
	workersPerNode int, backgroundWg *sync.WaitGroup, cancelCache *RedisCancellationCache,
) *SimQueue {
	log = log.Named("queue")
	q := &SimQueue{
		log:            log,
		queue:          queue,
		eth:            eth,
		workers:        make([]SimulationWorker, 0, len(sim)),
		workersPerNode: workersPerNode,
	}

	for i, s := range sim {
		worker := SimulationWorker{
			log:               log.Named("worker").With(zap.Int("worker-id", i)),
			simulationBackend: s,
			simRes:            simRes,
			cancelCache:       cancelCache,
			backgroundWg:      backgroundWg,
		}
		q.workers = append(q.workers, worker)
	}
	return q
}

func (q *SimQueue) Start(ctx context.Context) *sync.WaitGroup {
	process := make([]simqueue.ProcessFunc, 0, len(q.workers)*q.workersPerNode)
	for _, w := range q.workers {
		if q.workersPerNode > 1 {
			workers := simqueue.MultipleWorkers(w.Process, q.workersPerNode, rate.Inf, 1)[0]
			process = append(process, workers)
		} else {
			process = append(process, w.Process)
		}
	}
	blockNumber, err := q.eth.BlockNumber(ctx)
	if err != nil {
		q.log.Warn("Failed to get block number", zap.Error(err))
	} else {
		_ = q.queue.UpdateBlock(blockNumber)
	}

	wg := q.queue.StartProcessLoop(ctx, process)

	wg.Add(1)
	go func() {
		defer wg.Done()

		back := backoff.NewExponentialBackOff()
		back.MaxInterval = 3 * time.Second
		back.MaxElapsedTime = 12 * time.Second

		ticker := time.NewTicker(100 * time.Millisecond)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := backoff.Retry(func() error {
					blockNumber, err := q.eth.BlockNumber(ctx)
					if err != nil {
						return err
					}
					return q.queue.UpdateBlock(blockNumber)
				}, back)
				if err != nil {
					q.log.Error("Failed to update block number", zap.Error(err))
				}
			}
		}
	}()
	return wg
}

func (q *SimQueue) ScheduleBundleSimulation(ctx context.Context, bundle *SendMevBundleArgs, highPriority bool) error {
	startAt := time.Now()
	defer func() {
		metrics.RecordBundleAddQueueDuration(time.Since(startAt).Milliseconds())
	}()
	data, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	return q.queue.Push(ctx, data, highPriority, uint64(bundle.Inclusion.BlockNumber), uint64(bundle.Inclusion.MaxBlock))
}

type SimulationWorker struct {
	log               *zap.Logger
	simulationBackend SimulationBackend
	simRes            SimulationResult
	cancelCache       *RedisCancellationCache
	backgroundWg      *sync.WaitGroup
}

func (w *SimulationWorker) Process(ctx context.Context, data []byte, info simqueue.QueueItemInfo) (err error) {
	startAt := time.Now()
	defer func() {
		metrics.RecordBundleProcessDuration(time.Since(startAt).Milliseconds())
	}()
	var bundle SendMevBundleArgs
	err = json.Unmarshal(data, &bundle)
	if err != nil {
		w.log.Error("Failed to unmarshal bundle simulation data", zap.Error(err))
		return err
	}

	var hash common.Hash
	if bundle.Metadata != nil {
		hash = bundle.Metadata.BundleHash
	}
	logger := w.log.With(zap.String("bundle", hash.Hex()))

	// Check if bundle was cancelled
	cancelled, err := w.isBundleCancelled(ctx, &bundle)
	if err != nil {
		// We don't return error here,  because we would consider this error as non-critical as our cancellations are "best effort".
		logger.Error("Failed to check if bundle was cancelled", zap.Error(err))
	}
	if cancelled {
		logger.Info("Bundle is not simulated because it was cancelled")
		return simqueue.ErrProcessUnrecoverable
	}

	result, err := w.simulationBackend.SimulateBundle(ctx, &bundle, nil)
	if err != nil {
		logger.Error("Failed to simulate matched bundle", zap.Error(err))
		// we want to retry after such error
		return errors.Join(err, simqueue.ErrProcessWorkerError)
	}

	logger.Info("Simulated bundle",
		zap.Bool("success", result.Success), zap.String("err_reason", result.Error),
		zap.String("gwei_eff_gas_price", formatUnits(result.MevGasPrice.ToInt(), "gwei")),
		zap.String("eth_profit", formatUnits(result.Profit.ToInt(), "eth")),
		zap.String("eth_refundable_value", formatUnits(result.RefundableValue.ToInt(), "eth")),
		zap.Uint64("gas_used", uint64(result.GasUsed)),
		zap.Uint64("state_block", uint64(result.StateBlock)),
		zap.Int("retries", info.Retries),
	)

	// Try to re-simulate bundle if it failed
	if !result.Success && isErrorRecoverable(result.Error) {
		max := bundle.Inclusion.MaxBlock
		state := result.StateBlock
		// If state block is N, that means simulation for target block N+1 was tried
		if max != 0 && state != 0 && max > state+1 {
			return simqueue.ErrProcessScheduleNextBlock
		}
	}

	w.backgroundWg.Add(1)
	go func() {
		defer w.backgroundWg.Done()
		resCtx, cancel := context.WithTimeout(context.Background(), consumeSimulationTimeout)
		defer cancel()
		err = w.simRes.SimulatedBundle(resCtx, &bundle, result, info)
		if err != nil {
			w.log.Error("Failed to consume matched share bundle", zap.Error(err))
		}
	}()

	if !result.Success && !isErrorRecoverable(result.Error) {
		return simqueue.ErrProcessUnrecoverable
	}
	return nil
}

func (w *SimulationWorker) isBundleCancelled(ctx context.Context, bundle *SendMevBundleArgs) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, simCacheTimeout)
	defer cancel()
	if bundle.Metadata == nil {
		w.log.Error("Bundle has no metadata, skipping cancel check")
		return false, nil
	}
	res, err := w.cancelCache.IsCancelled(ctx, append([]common.Hash{bundle.Metadata.BundleHash}, bundle.Metadata.BodyHashes...))
	if err != nil {
		return false, err
	}
	return res, nil
}

func isErrorRecoverable(message string) bool {
	return !strings.Contains(message, "nonce too low")
}
