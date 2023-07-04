// Package simqueue is a queue implementation that uses redis as a backend.
//
// Queue uses one sorted set in redis to store items. It implements a priority queue with the rules described below.
//
// Usage:
// 1. Create a new queue instance with `NewRedisQueue`.
// 2. Start processing loop with `StartProcessLoop`.
// 3. Push items to the queue with `Push`.
// 4. Queue needs to be updated with the current block number regularly. It does not update the block number automatically.
//
// NOTE: Queue is not 100% reliable.
//
//	 There is a small chance that an item is lost when worker who claimed the item crashes or loses connection to the
//		network.
//
//		The impact of this is reduced by the fact that workers don't hold more items than they are processing.
//		So the max number of items that can be lost in a catastrophic event is equal to the number of workers.
//		See shutdown section below on how to avoid loss on normal shutdown.
//
// Queue submission:
// 1. Client pushes an item to the queue specifying:
//   - the target block number range when the item should be processed.
//   - whether the item is high priority or not.
//     2. The queue stores the item in a sorted set with the score being the minimal target block number.
//     3. If the queue is full, the item is discarded and `ErrQueueFull` is returned.
//     There is a limit on the number of elements in the queue items.
//
// Queue processing:
//
//  1. The queue is processed in a loop by number of workers in parallel.
//     Amount of workers is determined by the number of `ProcessFunc` functions passed to `StartProcessLoop`.
//     Each worker is working on one item at a time. So to fully saturate worker node that can process multiple items in parallel
//     you need to start multiple workers for the same node.
//
//  2. The queue is processed in the following way:
//     * The worker pops next item. Order of items is determined by the following rules:
//     * Items with lower target block number are processed first.
//     If target block number is not reached yet, the item is rescheduled.
//     If target block number is the same, priority is determined lexicographically in the following order:
//     + high priority
//     + number of retries while processing this item
//     + time of submission
//     + max target block number
//     + payload data itself
//     + The worker calls the `ProcessFunc` function with the payload data.
//
//     The `ProcessFunc` function is responsible for processing the item.
//     * It should return `nil` if the item was processed successfully.
//     If item should be retried on the next block, the `ErrProcessScheduleNextBlock` error should be returned.
//     If item should be retried on the same block (worker is faulty), the `ErrProcessWorkerError` error should be returned.
//     * If the `ProcessFunc` function returns `ErrProcess*` error, the item is rescheduled but up to `maxRetries` times.
//     Rescheduling is needed so in case of a worker error (one of the nodes in the cluster is down)
//     the item is added back to the queue and processed by (hopefully) another worker.
//     maxRetries is needed to prevent infinite loop in case of a bug.
//     Rescheduling for the next block is needed if bundle fails, but it is still possible that it will work on the next block.
//     * There is an exponential backoff between retries for the worker so if the worker
//     is constantly failing to process item it will get less and less work.
//
// Queue shutdown:
// 1. Workers can be shutdown by cancelling the context passed to `StartProcessLoop`.
// 2. SyncGroup returned form `StartProcessLoop` can be used to wait for all workers to finish processing.
package simqueue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var (
	ErrBlockNumberIncorrect = errors.New("block number is invalid")
	ErrStaleItem            = errors.New("item is stale")
	ErrQueueFull            = errors.New("queue is full")
	ErrMaxRetriesReached    = errors.New("max retries reached")
	ErrNoNextBlock          = errors.New("failed to requeue item, no next block available")
	ErrRequeueFailed        = errors.New("item requeue failed")
)

const (
	DefaultMaxRetries                     = uint16(30)
	DefaultMaxUnprocessedItemsForLowPrio  = uint64(1024)
	DefaultMaxUnprocessedItemsForHighPrio = uint64(2048)
	DefaultWorkerTimout                   = 4 * time.Second
)

// Errors returned by ProcessFunc.
var (
	// ErrProcessScheduleNextBlock is returned by ProcessFunc if item should be retried on the next block.
	ErrProcessScheduleNextBlock = errors.New("try to schedule item for the next block")
	// ErrProcessWorkerError is returned by ProcessFunc if item should be retried on the same block by a different worker.
	ErrProcessWorkerError = errors.New("worker error, retry processing on another worker")
)

type ProcessFunc func(ctx context.Context, data []byte) error

type Queue interface {
	UpdateBlock(block uint64) error
	Push(ctx context.Context, data []byte, highPriority bool, minTargetBlock, maxTargetBlock uint64) error
	StartProcessLoop(ctx context.Context, workers []ProcessFunc) *sync.WaitGroup
}

type RedisQueue struct {
	log          *zap.Logger
	red          *redis.Client
	currentBlock *uint64
	queueName    string

	MaxRetries                  uint16
	MaxUnprocessedItemsLowPrio  uint64
	MaxUnprocessedItemsHighPrio uint64
	WorkerTimeout               time.Duration
}

func NewRedisQueue(log *zap.Logger, red *redis.Client, queueName string) *RedisQueue {
	currentBlock := uint64(0)
	log = log.With(zap.String("queue", queueName))
	return &RedisQueue{
		log:                         log,
		red:                         red,
		currentBlock:                &currentBlock,
		queueName:                   queueName,
		MaxRetries:                  DefaultMaxRetries,
		MaxUnprocessedItemsLowPrio:  DefaultMaxUnprocessedItemsForLowPrio,
		MaxUnprocessedItemsHighPrio: DefaultMaxUnprocessedItemsForHighPrio,
		WorkerTimeout:               DefaultWorkerTimout,
	}
}

func (s *RedisQueue) UpdateBlock(block uint64) error {
	current := atomic.LoadUint64(s.currentBlock)
	if current == block {
		return nil
	}
	if current > block {
		return ErrBlockNumberIncorrect
	}
	atomic.StoreUint64(s.currentBlock, block)
	return nil
}

func (s *RedisQueue) Push(ctx context.Context, data []byte, highPriority bool, minTargetBlock, maxTargetBlock uint64) error {
	currentBlock := atomic.LoadUint64(s.currentBlock)

	if maxTargetBlock <= currentBlock {
		s.log.Debug("max target block is less than current block, skipping", zap.Uint64("max_target_block", maxTargetBlock), zap.Uint64("current_block", currentBlock))
		return ErrStaleItem
	}

	// we schedule items for the next block
	if nextBlock := currentBlock + 1; minTargetBlock < nextBlock {
		minTargetBlock = nextBlock
	}

	args := packArgs{
		data:           data,
		minTargetBlock: minTargetBlock,
		maxTargetBlock: maxTargetBlock,
		highPriority:   highPriority,
		timestamp:      time.Now(),
		iteration:      0,
	}
	err := s.pushToQueue(ctx, args)
	if err != nil {
		return err
	}
	s.log.Debug("pushed to queue", zap.Uint64("min_target_block", minTargetBlock), zap.Uint64("max_target_block", maxTargetBlock), zap.Bool("high_priority", highPriority))
	return nil
}

// returns number of items in the queue that should be eventually processed
func (s *RedisQueue) queuedItems(ctx context.Context) (uint64, error) {
	return s.red.ZCard(ctx, s.queueName).Uint64()
}

func (s *RedisQueue) pushToQueue(ctx context.Context, args packArgs) error {
	queued, err := s.queuedItems(ctx)
	if err != nil {
		s.log.Warn("failed to get queued items", zap.Error(err))
		return err
	}
	threshold := s.MaxUnprocessedItemsLowPrio
	if args.highPriority {
		threshold = s.MaxUnprocessedItemsHighPrio
	}
	if queued >= threshold {
		s.log.Error("too many unprocessed items in the queue", zap.Uint64("queued", queued), zap.Uint64("max_unprocessed_items", threshold))
		return ErrQueueFull
	}

	score, redisData := packData(args)
	err = s.red.ZAdd(ctx, s.queueName, redis.Z{Score: score, Member: redisData}).Err()
	if err != nil {
		s.log.Debug("failed to push to queue", zap.Error(err))
	}
	return err
}

// popFromQueue pops an item from the queue
// it will block for up to 1 second waiting for an item if a queue is empty
func (s *RedisQueue) popFromQueue(ctx context.Context) (packArgs, error) {
	// 1 second is minimal value for a timeout
	// we will block for up to 1 second waiting for an item
	value, err := s.red.BZPopMin(ctx, time.Second, s.queueName).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return packArgs{}, err
		}
		s.log.Error("failed to pop from queue", zap.Error(err))
		return packArgs{}, err
	}

	redisData, ok := value.Member.(string)
	if !ok {
		s.log.Error("failed to pop from queue, invalid data type")
		return packArgs{}, err
	}

	args, err := unpackData(value.Score, []byte(redisData))
	if err != nil {
		s.log.Error("failed to unpack data", zap.Error(err))
		return packArgs{}, err
	}
	return args, nil
}

func (s *RedisQueue) processNextItem(ctx context.Context, process ProcessFunc) error {
	// we use this backoff for requeuing items because It's important to not lose items
	exp := backoff.NewExponentialBackOff()
	exp.MaxElapsedTime = 4 * time.Second
	back := backoff.WithContext(exp, ctx)

	args, err := s.popFromQueue(ctx)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil
		}
		return err
	}

	nextBlock := atomic.LoadUint64(s.currentBlock) + 1

	// too early to process, requeue
	if nextBlock < args.minTargetBlock {
		err := s.retryItem(ctx, args, false, false, back)
		if err != nil {
			return err
		}
		return nil
	}

	// stale item, skip or requeue for the next block
	if nextBlock > args.minTargetBlock {
		if nextBlock > args.maxTargetBlock {
			s.log.Debug("skipping stale item",
				zap.Uint64("next_block", nextBlock),
				zap.Uint64("min_target_block", args.minTargetBlock),
				zap.Uint64("max_target_block", args.maxTargetBlock))
			return nil
		}

		// requeue for the next block
		args.minTargetBlock = nextBlock

		err := s.retryItem(ctx, args, false, false, back)
		if err != nil {
			return err
		}
		return nil
	}

	// process item
	workerCtx, workerCancel := context.WithTimeout(ctx, s.WorkerTimeout)
	defer workerCancel()
	err = process(workerCtx, args.data)

	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrProcessWorkerError):
		s.log.Warn("worker failed to process item, retrying", zap.Error(err), zap.Uint16("iteration", args.iteration))
		err := s.retryItem(ctx, args, true, false, back)
		if err != nil {
			return err
		}
	case errors.Is(err, ErrProcessScheduleNextBlock):
		s.log.Debug("worker iteration failed, scheduled for the next block",
			zap.Error(err),
			zap.Uint64("next_block", nextBlock),
			zap.Uint64("min_target_block", args.minTargetBlock),
			zap.Uint64("max_target_block", args.maxTargetBlock),
		)
		err := s.retryItem(ctx, args, true, true, back)
		if err != nil {
			return err
		}
	case err != nil:
		return err
	}
	timeInQueue := time.Since(args.timestamp)
	s.log.Debug("processed queue item", zap.Uint16("iteration", args.iteration), zap.Duration("time_in_queue", timeInQueue))
	return nil
}

// StartProcessLoop starts a loop that will process items from the queue
// it will spawn a goroutine for each worker.
// ctx can be used to signal shutdown
// Wait group is returned to allow for graceful shutdown
func (s *RedisQueue) StartProcessLoop(ctx context.Context, workers []ProcessFunc) *sync.WaitGroup {
	var wg sync.WaitGroup
	for _, process := range workers {
		wg.Add(1)
		go func(process func(ctx context.Context, data []byte) error) {
			defer wg.Done()

			exp := backoff.NewExponentialBackOff()
			exp.MaxInterval = 30 * time.Second
			exp.MaxElapsedTime = 2 * time.Minute
			back := backoff.WithContext(exp, ctx)
			for {
				select {
				case <-ctx.Done():
					return
				default:
					err := backoff.Retry(func() error {
						err := s.processNextItem(ctx, process)
						return err
					}, back)
					if err != nil && !errors.Is(err, context.Canceled) {
						s.log.Error("Processing next element failed", zap.Error(err))
					}
				}
			}
		}(process)
	}
	return &wg
}

func (s *RedisQueue) retryItem(ctx context.Context, args packArgs, incrIteration, incrBlock bool, back backoff.BackOff) error {
	if args.iteration >= s.MaxRetries {
		return ErrMaxRetriesReached
	}

	if incrIteration {
		args.iteration++
	}
	if incrBlock {
		if args.minTargetBlock >= args.maxTargetBlock {
			return ErrNoNextBlock
		}
		args.minTargetBlock++
	}
	err := backoff.Retry(func() error {
		return s.pushToQueue(ctx, args)
	}, back)
	if err != nil {
		s.log.Error("failed to requeue item", zap.Error(err))
		return errors.Join(err, ErrRequeueFailed)
	}
	return nil
}

// CleanQueues cleans all data in redis associated with the given queue
// NOTE: slow and dangerous operation, should only be used for testing
func (s *RedisQueue) CleanQueues(ctx context.Context) error {
	return s.red.Del(ctx, s.queueName).Err()
}
