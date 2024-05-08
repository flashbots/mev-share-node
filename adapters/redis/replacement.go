// Package redis provides an adapter to redis client
package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type ReplacementCache struct {
	client         *redis.Client
	expireDuration time.Duration
	keyPrefix      string
}

func NewReplacementCache(client *redis.Client, expireDuration time.Duration, keyPrefix string) *ReplacementCache {
	return &ReplacementCache{
		client:         client,
		expireDuration: expireDuration,
		keyPrefix:      keyPrefix,
	}
}

func (r *ReplacementCache) IncReplacementNonce(ctx context.Context, signingAddress, replacementUUID string) (uint64, error) {
	nonce, err := r.client.Incr(ctx, r.keyPrefix+signingAddress+replacementUUID).Result()
	if err != nil {
		return 0, err
	}
	// ignore expiry error as it is not critical
	// TODO: probably just log here
	_ = r.client.Expire(ctx, r.keyPrefix+signingAddress+replacementUUID, r.expireDuration).Err()
	return uint64(nonce), nil
}

func (r *ReplacementCache) GetReplacementNonce(ctx context.Context, signingAddress, replacementUUID string) (uint64, error) {
	nonce, err := r.client.Get(ctx, r.keyPrefix+signingAddress+replacementUUID).Int64()
	return uint64(nonce), err
}
