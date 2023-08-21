package mevshare

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisCancellationCache_Add(t *testing.T) {
	red := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx := context.Background()

	cache := NewRedisCancellationCache(red, 3*time.Second, "test")
	require.NoError(t, cache.DeleteAll(ctx))

	hash1 := common.HexToHash("0x123")
	hash2 := common.HexToHash("0x123")

	res, err := cache.IsCancelled(ctx, []common.Hash{hash1, hash2})
	require.NoError(t, err)
	require.False(t, res)

	require.NoError(t, cache.Add(ctx, hash1))

	res, err = cache.IsCancelled(ctx, []common.Hash{hash1, hash2})
	require.NoError(t, err)
	require.True(t, res)

	require.NoError(t, cache.Add(ctx, hash2))

	res, err = cache.IsCancelled(ctx, []common.Hash{hash1, hash2})
	require.NoError(t, err)
	require.True(t, res)

	time.Sleep(3*time.Second + 100*time.Millisecond)

	res, err = cache.IsCancelled(ctx, []common.Hash{hash1, hash2})
	require.NoError(t, err)
	require.False(t, res)
}
