package mevshare

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/redis/go-redis/v9"
)

type RedisCancellationCache struct {
	client         *redis.Client
	expireDuration time.Duration
	keyPrefix      string
}

func NewRedisCancellationCache(client *redis.Client, expireDuration time.Duration, keyPrefix string) *RedisCancellationCache {
	return &RedisCancellationCache{
		client:         client,
		expireDuration: expireDuration,
		keyPrefix:      keyPrefix,
	}
}

func (c *RedisCancellationCache) Add(ctx context.Context, hash common.Hash) error {
	return c.client.Set(ctx, c.keyPrefix+hash.Hex(), 1, c.expireDuration).Err()
}

func (c *RedisCancellationCache) IsCancelled(ctx context.Context, hash []common.Hash) (bool, error) {
	keys := make([]string, len(hash))
	for i, h := range hash {
		keys[i] = c.keyPrefix + h.Hex()
	}
	res, err := c.client.MGet(ctx, keys...).Result()
	if err != nil {
		return false, err
	}
	for _, r := range res {
		if r != nil {
			return true, nil
		}
	}
	return false, nil
}

// DeleteAll deletes all the keys in the cache. It can be very slow and should only be used for testing.
func (c *RedisCancellationCache) DeleteAll(ctx context.Context) error {
	keys, err := c.client.Keys(ctx, c.keyPrefix+"*").Result()
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	return c.client.Del(ctx, keys...).Err()
}
