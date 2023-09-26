package simqueue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

var processTimeout = 50 * time.Millisecond

type queueRunner struct {
	queue  *RedisQueue
	wg     *sync.WaitGroup
	cancel context.CancelFunc
}

func newQueueRunner(queueName string) queueRunner {
	log, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	red := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	queue := NewRedisQueue(log, red, queueName)
	err = queue.CleanQueues(context.Background())
	if err != nil {
		panic(err)
	}
	return queueRunner{
		queue:  queue,
		cancel: nil,
		wg:     nil,
	}
}

func (q *queueRunner) startProcessLoop(ctx context.Context, processFuncs []ProcessFunc) {
	procCtx, procCancel := context.WithCancel(ctx)
	q.cancel = procCancel
	q.wg = q.queue.StartProcessLoop(procCtx, processFuncs)
}

func (q *queueRunner) stopProcessLoop() {
	if q.cancel != nil {
		q.cancel()
	}
	if q.wg != nil {
		q.wg.Wait()
	}
	err := q.queue.CleanQueues(context.Background())
	if err != nil {
		panic(err)
	}
}

type testWorker struct {
	processed chan []byte
}

func newTestWorker() *testWorker {
	return &testWorker{
		processed: make(chan []byte, 10),
	}
}

func (w *testWorker) processOk(ctx context.Context, data []byte, info QueueItemInfo) error {
	w.processed <- data
	return nil
}

func (w *testWorker) nextProcessed(timeout time.Duration) []byte {
	select {
	case data := <-w.processed:
		return data
	case <-time.After(timeout):
		return nil
	}
}

// TestRedisQueue tests that the redis queue works as expected
// it's implemented in one method to test possible interactions between different tests
func TestRedisQueue(t *testing.T) {
	ctx := context.Background()
	r := newQueueRunner("test_queue")
	worker := newTestWorker()

	//

	// test that queue can be cancelled
	t.Run("empty queue cancel", func(t *testing.T) {
		r.startProcessLoop(ctx, []ProcessFunc{worker.processOk})

		// wait so code gets to the blocking pop opearation
		time.Sleep(10 * time.Millisecond)

		r.stopProcessLoop()
	})

	// test that normal processing works
	t.Run("normal processing", func(t *testing.T) {
		r.startProcessLoop(ctx, []ProcessFunc{worker.processOk})

		err := r.queue.UpdateBlock(1)
		require.NoError(t, err)

		err = r.queue.Push(context.Background(), []byte("test"), false, 2, 2)
		require.NoError(t, err)

		require.Equal(t, "test", string(worker.nextProcessed(processTimeout)))

		require.Nil(t, worker.nextProcessed(processTimeout))

		r.stopProcessLoop()
	})

	// test multiple workers
	t.Run("multiple workers", func(t *testing.T) {
		workers := MultipleWorkers(worker.processOk, 10, rate.Inf, 1)
		r.startProcessLoop(ctx, workers)

		err := r.queue.UpdateBlock(1)
		require.NoError(t, err)

		for i := 0; i < 10; i++ {
			err = r.queue.Push(ctx, []byte("test-multiple"), false, 2, 2)
			require.NoError(t, err)
		}

		for i := 0; i < 10; i++ {
			require.Equal(t, "test-multiple", string(worker.nextProcessed(processTimeout)))
		}

		require.Nil(t, worker.nextProcessed(processTimeout))
		r.stopProcessLoop()
	})

	// test that stale items are not processed
	t.Run("test queue cleanup", func(t *testing.T) {
		err := r.queue.Push(context.Background(), []byte("test-stale"), false, 2, 2)
		require.NoError(t, err)

		err = r.queue.UpdateBlock(2)
		require.NoError(t, err)

		r.startProcessLoop(ctx, []ProcessFunc{worker.processOk})

		err = r.queue.Push(ctx, []byte("test-new"), false, 3, 3)
		require.NoError(t, err)

		require.Equal(t, "test-new", string(worker.nextProcessed(processTimeout)))

		require.Nil(t, worker.nextProcessed(processTimeout))

		r.stopProcessLoop()
	})

	// test queue push semantics
	t.Run("queue push", func(t *testing.T) {
		testQueuePush(t, r, worker)
	})

	// test rollover of the unprocessed items in the queue
	t.Run("test rollover", func(t *testing.T) {
		err := r.queue.Push(ctx, []byte("test-rollover"), false, 4, 5)
		require.NoError(t, err)

		err = r.queue.UpdateBlock(3)
		require.NoError(t, err)

		err = r.queue.UpdateBlock(4)
		require.NoError(t, err)

		r.startProcessLoop(ctx, []ProcessFunc{worker.processOk})

		require.Equal(t, "test-rollover", string(worker.nextProcessed(processTimeout)))

		require.Nil(t, worker.nextProcessed(processTimeout))
		r.stopProcessLoop()
	})

	// test processing when processor returns error
	t.Run("test processing with error", func(t *testing.T) {
		errEncountered := false
		processErr := func(ctx context.Context, data []byte, info QueueItemInfo) error {
			errEncountered = true
			return errors.Join(errors.New("processing error"), ErrProcessWorkerError) //nolint:goerr113
		}

		err := r.queue.UpdateBlock(5)
		require.NoError(t, err)

		r.startProcessLoop(ctx, []ProcessFunc{worker.processOk, processErr})

		// push 4 items
		for i := 0; i < 4; i++ {
			err = r.queue.Push(ctx, []byte("test-error"), false, 6, 6)
			require.NoError(t, err)
		}

		// receive 4 items from the queue (processed by the first processor that does not fail)
		for i := 0; i < 4; i++ {
			require.Equal(t, "test-error", string(worker.nextProcessed(processTimeout)))
		}
		// make sure the error was encountered (processed by the second processor that fails)
		require.True(t, errEncountered)

		require.Nil(t, worker.nextProcessed(processTimeout))

		r.stopProcessLoop()
	})

	// test processing when processor returns error too many times
	t.Run("test processing with error, worker error", func(t *testing.T) {
		r.queue.Config.MaxRetries = 2
		defer func() {
			r.queue.Config.MaxRetries = DefaultQueueConfig.MaxRetries
		}()

		processErr := func(ctx context.Context, data []byte, info QueueItemInfo) error {
			return errors.Join(errors.New("processing error"), ErrProcessWorkerError) //nolint:goerr113
		}

		err := r.queue.UpdateBlock(6)
		require.NoError(t, err)

		r.startProcessLoop(ctx, []ProcessFunc{processErr})

		err = r.queue.Push(ctx, []byte("test-error-forever"), false, 7, 7)
		require.NoError(t, err)

		require.Nil(t, worker.nextProcessed(processTimeout*5))

		r.stopProcessLoop()
	})

	// test rescheduling of the unprocessed items in the queue for the next block
	t.Run("test processing with rescheduling", func(t *testing.T) {
		testQueueResched(t, r, worker)
	})

	// test rescheduling of the unprocessed items in the queue for the next block, but with too many retries
	t.Run("test processing with rescheduling, error processing, too many retries", func(t *testing.T) {
		testQueueReschedTooManyRetries(t, r, worker)
	})

	// test rescheduling of the processed items in the queue for the next block when block range is specified
	t.Run("test reschedule when processing successful", func(t *testing.T) {
		testQueueReschedOnSuccess(t, r, worker)
	})

	// test rescheduling of the processed items in the queue for the next block when item is not recoverable
	t.Run("test reschedule when processing, unrecoverable item", func(t *testing.T) {
		var (
			callCount int
			mu        sync.Mutex
		)
		processErr := func(ctx context.Context, data []byte, info QueueItemInfo) error {
			mu.Lock()
			defer mu.Unlock()
			callCount++
			return errors.Join(errors.New("item unrecoverable"), ErrProcessUnrecoverable) //nolint:goerr113
		}

		err := r.queue.UpdateBlock(21)
		require.NoError(t, err)

		r.startProcessLoop(ctx, []ProcessFunc{processErr})

		err = r.queue.Push(ctx, []byte("test-item-unrecoverable"), false, 22, 25)
		require.NoError(t, err)

		// after 1 attempt, it should give up
		for b := 22; b <= 25; b++ {
			require.NoError(t, r.queue.UpdateBlock(uint64(b)))
			time.Sleep(processTimeout)
		}

		require.Nil(t, worker.nextProcessed(processTimeout*5))
		mu.Lock()
		require.Equal(t, 1, callCount)
		mu.Unlock()

		r.stopProcessLoop()
	})
}

func testQueueReschedOnSuccess(t *testing.T, r queueRunner, worker *testWorker) {
	t.Helper()
	ctx := context.Background()
	r.queue.Config.MaxRetries = 3
	defer func() {
		r.queue.Config.MaxRetries = DefaultQueueConfig.MaxRetries
	}()

	var (
		callCount = 0
		mu        sync.Mutex
	)
	processOk := func(ctx context.Context, data []byte, info QueueItemInfo) error {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		return nil
	}

	err := r.queue.UpdateBlock(15)
	require.NoError(t, err)

	err = r.queue.Push(ctx, []byte("test-error-reschedule-success"), false, 15, 20)
	require.NoError(t, err)

	r.startProcessLoop(ctx, []ProcessFunc{processOk})

	// after 3 successful attempts, it should give up
	for b := 16; b <= 20; b++ {
		require.NoError(t, r.queue.UpdateBlock(uint64(b)))
		time.Sleep(processTimeout)
	}

	require.Nil(t, worker.nextProcessed(processTimeout))
	// 3 successful attempts
	mu.Lock()
	require.Equal(t, 3, callCount)
	mu.Unlock()

	r.stopProcessLoop()
}

func testQueueReschedTooManyRetries(t *testing.T, r queueRunner, worker *testWorker) {
	t.Helper()
	ctx := context.Background()
	r.queue.Config.MaxRetries = 3
	defer func() {
		r.queue.Config.MaxRetries = DefaultQueueConfig.MaxRetries
	}()

	var (
		callCount = 0
		mu        sync.Mutex
	)
	processErr := func(ctx context.Context, data []byte, info QueueItemInfo) error {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		return ErrProcessScheduleNextBlock
	}

	err := r.queue.Push(ctx, []byte("test-error-reschedule-unrecoverable"), false, 11, 20)
	require.NoError(t, err)

	r.startProcessLoop(ctx, []ProcessFunc{processErr})

	err = r.queue.UpdateBlock(10)
	require.NoError(t, err)
	time.Sleep(processTimeout)
	err = r.queue.UpdateBlock(11)
	require.NoError(t, err)
	time.Sleep(processTimeout)
	err = r.queue.UpdateBlock(12)
	require.NoError(t, err)
	time.Sleep(processTimeout)
	// after 3 failures it should give up
	err = r.queue.UpdateBlock(13)
	require.NoError(t, err)
	time.Sleep(processTimeout)
	err = r.queue.UpdateBlock(14)
	require.NoError(t, err)
	time.Sleep(processTimeout)

	require.Nil(t, worker.nextProcessed(processTimeout))
	// 3 failures
	mu.Lock()
	require.Equal(t, 3, callCount)
	mu.Unlock()

	r.stopProcessLoop()
}

func testQueueResched(t *testing.T, r queueRunner, worker *testWorker) {
	t.Helper()
	ctx := context.Background()
	r.queue.Config.MaxRetries = 3
	defer func() {
		r.queue.Config.MaxRetries = DefaultQueueConfig.MaxRetries
	}()

	var (
		callCount        = 0
		shouldReschedule = true
		mu               sync.Mutex
	)
	processErr := func(ctx context.Context, data []byte, info QueueItemInfo) error {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		if shouldReschedule {
			return ErrProcessScheduleNextBlock
		} else {
			return worker.processOk(ctx, data, info)
		}
	}

	err := r.queue.UpdateBlock(7)
	require.NoError(t, err)

	r.startProcessLoop(ctx, []ProcessFunc{processErr})

	err = r.queue.Push(ctx, []byte("test-error-reschedule"), false, 8, 10)
	require.NoError(t, err)

	// it should fail for current block 7, but reschedule for block 8
	require.Nil(t, worker.nextProcessed(processTimeout))
	err = r.queue.UpdateBlock(8)
	require.NoError(t, err)

	// it should fail for current block 8, but reschedule for block 9, where it should succeed
	time.Sleep(processTimeout)

	mu.Lock()
	shouldReschedule = false
	mu.Unlock()

	require.Nil(t, worker.nextProcessed(processTimeout))
	err = r.queue.UpdateBlock(9)
	require.NoError(t, err)

	require.Equal(t, "test-error-reschedule", string(worker.nextProcessed(processTimeout)))

	require.Nil(t, worker.nextProcessed(processTimeout))

	// 2 failures + 1 success
	mu.Lock()
	require.Equal(t, 3, callCount)
	mu.Unlock()

	r.stopProcessLoop()
}

func testQueuePush(t *testing.T, r queueRunner, worker *testWorker) {
	t.Helper()
	ctx := context.Background()

	r.queue.Config.MaxQueuedProcessableItemsLowPrio = 3
	r.queue.Config.MaxQueuedProcessableItemsHighPrio = 4
	r.queue.Config.MaxQueuedUnprocessableItemsLowPrio = 1
	r.queue.Config.MaxQueuedUnprocessableItemsHighPrio = 2
	defer func() {
		r.queue.Config = DefaultQueueConfig
	}()

	err := r.queue.UpdateBlock(3)
	require.NoError(t, err)

	proc, unproc, err := r.queue.queuedItems(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(0), proc)
	require.Equal(t, uint64(0), unproc)

	// adding stale element fails
	err = r.queue.Push(ctx, []byte("test-stale"), false, 2, 3)
	require.ErrorIs(t, err, ErrStaleItem)

	proc, unproc, err = r.queue.queuedItems(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(0), proc)
	require.Equal(t, uint64(0), unproc)

	// add 3 processable items
	err = r.queue.Push(ctx, []byte("test-full"), false, 2, 4)
	require.NoError(t, err)
	err = r.queue.Push(ctx, []byte("test-full"), false, 3, 5)
	require.NoError(t, err)
	err = r.queue.Push(ctx, []byte("test-full"), false, 4, 6)
	require.NoError(t, err)

	// add 1 unprocessable items
	err = r.queue.Push(ctx, []byte("test-full"), false, 5, 5)
	require.NoError(t, err)

	proc, unproc, err = r.queue.queuedItems(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(3), proc)
	require.Equal(t, uint64(1), unproc)

	// add 1 low-prio processable item, should fail
	err = r.queue.Push(ctx, []byte("test-full"), false, 4, 4)
	require.ErrorIs(t, err, ErrQueueFull)

	// add 1 high-prio processable item, should work
	err = r.queue.Push(ctx, []byte("test-full"), true, 4, 4)
	require.NoError(t, err)

	proc, unproc, err = r.queue.queuedItems(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), proc)
	require.Equal(t, uint64(1), unproc)

	// add 1 low-prio unprocessable item, should fail
	err = r.queue.Push(ctx, []byte("test-full"), false, 5, 6)
	require.ErrorIs(t, err, ErrQueueFull)

	// add 1 high-prio unprocessable item, should work
	err = r.queue.Push(ctx, []byte("test-full"), true, 5, 6)
	require.NoError(t, err)

	proc, unproc, err = r.queue.queuedItems(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), proc)
	require.Equal(t, uint64(2), unproc)

	// add 1 high-prio unprocessable item, should fail
	err = r.queue.Push(ctx, []byte("test-full"), true, 5, 6)
	require.ErrorIs(t, err, ErrQueueFull)

	proc, unproc, err = r.queue.queuedItems(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), proc)
	require.Equal(t, uint64(2), unproc)

	require.Nil(t, worker.nextProcessed(processTimeout))

	err = r.queue.CleanQueues(ctx)
	require.NoError(t, err)
}

func BenchmarkQueue(b *testing.B) {
	ctx := context.Background()
	log := zap.NewNop()
	red := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	processOk := func(ctx context.Context, data []byte, info QueueItemInfo) error {
		return nil
	}
	queue := NewRedisQueue(log, red, "queue_test")
	queue.Config.MaxQueuedProcessableItemsLowPrio = 1000000
	queue.Config.MaxQueuedProcessableItemsHighPrio = 1000000
	queue.Config.MaxQueuedUnprocessableItemsLowPrio = 1000000
	queue.Config.MaxQueuedUnprocessableItemsHighPrio = 1000000
	err := queue.CleanQueues(ctx)
	require.NoError(b, err)

	procCtx, procCancel := context.WithCancel(ctx)
	wg := queue.StartProcessLoop(procCtx, []ProcessFunc{processOk})
	require.NoError(b, err)

	// 10kb of data
	data := make([]byte, 1024*10)
	b.SetBytes(int64(len(data)))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = queue.Push(ctx, []byte("test"), false, 1, 1)
		require.NoError(b, err)
	}
	b.StopTimer()
	procCancel()
	wg.Wait()
	err = queue.CleanQueues(ctx)
	require.NoError(b, err)
}
