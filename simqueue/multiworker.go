package simqueue

import (
	"context"

	"golang.org/x/time/rate"
)

// MultipleWorkers creates n workers that are rate limited by limit.
// Use case is to have multiple workers per simulation node.
// ProcessFunc must be thread safe.
func MultipleWorkers(processFunc ProcessFunc, n int, limit rate.Limit, burst int) []ProcessFunc {
	rateLimiter := rate.NewLimiter(limit, burst)

	process := make([]ProcessFunc, n)
	for i := 0; i < n; i++ {
		process[i] = func(ctx context.Context, data []byte, info QueueItemInfo) error {
			err := rateLimiter.Wait(ctx)
			if err != nil {
				return err
			}
			return processFunc(ctx, data, info)
		}
	}
	return process
}
