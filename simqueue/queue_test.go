package simqueue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

func TestRedisQueue(t *testing.T) {
	ctx := context.Background()
	log, err := zap.NewDevelopment()
	require.NoError(t, err)
	red := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	processed := make(chan []byte, 10)
	nextProcessed := func() []byte {
		select {
		case data := <-processed:
			return data
		case <-time.After(1 * time.Second):
			t.Fatal("timeout")
		}
		return nil
	}
	processOk := func(ctx context.Context, data []byte) error {
		processed <- data
		return nil
	}
	queue := NewRedisQueue(log, red, "queue_test")
	err = queue.CleanQueues(ctx)
	require.NoError(t, err)

	// test that queue can be cancelled
	t.Run("empty queue cancel", func(t *testing.T) {
		procCtx, procCancel := context.WithCancel(ctx)
		wg := queue.StartProcessLoop(procCtx, []ProcessFunc{processOk})

		// wait so code gets to the blocking pop opearation
		time.Sleep(10 * time.Millisecond)

		procCancel()
		wg.Wait()
		err = queue.CleanQueues(context.Background())
		require.NoError(t, err)
	})

	// test that normal processing works
	t.Run("normal processing", func(t *testing.T) {
		procCtx, procCancel := context.WithCancel(ctx)
		wg := queue.StartProcessLoop(procCtx, []ProcessFunc{processOk})

		err = queue.UpdateBlock(1)
		require.NoError(t, err)

		err = queue.Push(ctx, []byte("test"), false, 2, 2)
		require.NoError(t, err)

		require.Equal(t, "test", string(nextProcessed()))
		procCancel()
		wg.Wait()
		err = queue.CleanQueues(context.Background())
		require.NoError(t, err)
	})

	// test multiple workers
	t.Run("multiple workers", func(t *testing.T) {
		procCtx, procCancel := context.WithCancel(ctx)
		workers := MultipleWorkers(processOk, 10, rate.Inf, 1)
		wg := queue.StartProcessLoop(procCtx, workers)

		err = queue.UpdateBlock(1)
		require.NoError(t, err)

		for i := 0; i < 10; i++ {
			err = queue.Push(ctx, []byte("test-multiple"), false, 2, 2)
			require.NoError(t, err)
		}

		for i := 0; i < 10; i++ {
			require.Equal(t, "test-multiple", string(nextProcessed()))
		}
		procCancel()
		wg.Wait()
		err = queue.CleanQueues(context.Background())
		require.NoError(t, err)
	})

	// test that stale items are not processed
	t.Run("test queue cleanup", func(t *testing.T) {
		err = queue.Push(ctx, []byte("test-stale"), false, 2, 2)
		require.NoError(t, err)

		err = queue.UpdateBlock(2)
		require.NoError(t, err)

		procCtx, procCancel := context.WithCancel(ctx)
		wg := queue.StartProcessLoop(procCtx, []ProcessFunc{processOk})
		require.NoError(t, err)

		err = queue.Push(ctx, []byte("test-new"), false, 3, 3)
		require.NoError(t, err)

		require.Equal(t, "test-new", string(nextProcessed()))

		procCancel()
		wg.Wait()
		err = queue.CleanQueues(context.Background())
		require.NoError(t, err)
	})

	// test queue push semantics
	t.Run("queue push", func(t *testing.T) {
		queue.MaxUnprocessedItemsLowPrio = 3
		queue.MaxUnprocessedItemsHighPrio = 4
		defer func() {
			queue.MaxUnprocessedItemsLowPrio = DefaultMaxUnprocessedItemsForLowPrio
			queue.MaxUnprocessedItemsHighPrio = DefaultMaxUnprocessedItemsForHighPrio
		}()

		err = queue.UpdateBlock(3)
		require.NoError(t, err)

		queued, err := queue.queuedItems(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(0), queued)

		// adding stale element fails
		err = queue.Push(ctx, []byte("test-stale"), false, 2, 3)
		require.ErrorIs(t, err, ErrStaleItem)

		queued, err = queue.queuedItems(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(0), queued)

		// add 3 items
		err = queue.Push(ctx, []byte("test-full"), false, 3, 4)
		require.NoError(t, err)
		err = queue.Push(ctx, []byte("test-full"), false, 3, 5)
		require.NoError(t, err)
		err = queue.Push(ctx, []byte("test-full"), false, 5, 6)
		require.NoError(t, err)

		queued, err = queue.queuedItems(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(3), queued)

		// adding 4th item fails
		err = queue.Push(ctx, []byte("test-full"), false, 3, 7)
		require.ErrorIs(t, err, ErrQueueFull)

		// adding 4th high prio item ok
		err = queue.Push(ctx, []byte("test-full"), true, 3, 7)
		require.NoError(t, err)

		// adding 5th high prio item fails
		err = queue.Push(ctx, []byte("test-full"), true, 3, 7)
		require.ErrorIs(t, err, ErrQueueFull)

		queued, err = queue.queuedItems(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(4), queued)

		err = queue.CleanQueues(context.Background())
		require.NoError(t, err)
	})

	// test rollover of the unprocessed items in the queue
	t.Run("test rollover", func(t *testing.T) {
		err = queue.Push(ctx, []byte("test-rollover"), false, 4, 5)
		require.NoError(t, err)

		err = queue.UpdateBlock(3)
		require.NoError(t, err)

		err = queue.UpdateBlock(4)
		require.NoError(t, err)

		procCtx, procCancel := context.WithCancel(ctx)
		wg := queue.StartProcessLoop(procCtx, []ProcessFunc{processOk})
		require.NoError(t, err)

		require.Equal(t, "test-rollover", string(nextProcessed()))

		procCancel()
		wg.Wait()
		err = queue.CleanQueues(context.Background())
		require.NoError(t, err)
	})

	// test processing when processor returns error
	t.Run("test processing with error", func(t *testing.T) {
		errEncountered := false
		processErr := func(ctx context.Context, data []byte) error {
			errEncountered = true
			return errors.New("processing error") //nolint:goerr113
		}

		err = queue.UpdateBlock(5)
		require.NoError(t, err)

		procCtx, procCancel := context.WithCancel(ctx)
		wg := queue.StartProcessLoop(procCtx, []ProcessFunc{processOk, processErr})
		require.NoError(t, err)

		// push 4 items
		for i := 0; i < 4; i++ {
			err = queue.Push(ctx, []byte("test-error"), false, 6, 6)
			require.NoError(t, err)
		}

		// receive 4 items from the queue (processed by the first processor that does not fail)
		for i := 0; i < 4; i++ {
			require.Equal(t, "test-error", string(nextProcessed()))
		}
		// make sure the error was encountered (processed by the second processor that fails)
		require.True(t, errEncountered)

		procCancel()
		wg.Wait()
		err = queue.CleanQueues(context.Background())
		require.NoError(t, err)
	})
}

func BenchmarkQueue(b *testing.B) {
	ctx := context.Background()
	log := zap.NewNop()
	red := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	processOk := func(ctx context.Context, data []byte) error {
		return nil
	}
	queue := NewRedisQueue(log, red, "queue_test")
	queue.MaxUnprocessedItemsHighPrio = 1000000
	queue.MaxUnprocessedItemsLowPrio = 1000000
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
