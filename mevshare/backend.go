package mevshare

import (
	"context"
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
	"github.com/redis/go-redis/v9"
	"github.com/ybbus/jsonrpc/v3"
)

type HintBackend interface {
	NotifyHint(ctx context.Context, hint *Hint) error
}

type BuilderBackend interface {
	String() string
	SendMatchedShareBundle(ctx context.Context, bundle *SendMevBundleArgs) error
	CancelBundleByHash(ctx context.Context, hash common.Hash) error
}

// SimulationBackend is an interface for simulating transactions
// There should be one simulation backend per worker node
type SimulationBackend interface {
	SimulateBundle(ctx context.Context, bundle *SendMevBundleArgs, aux *SimMevBundleAuxArgs) (*SimMevBundleResponse, error)
}

type JSONRPCSimulationBackend struct {
	client jsonrpc.RPCClient
}

func NewJSONRPCSimulationBackend(url string) *JSONRPCSimulationBackend {
	return &JSONRPCSimulationBackend{
		client: jsonrpc.NewClient(url),
		// todo here use optsx
	}
}

func (b *JSONRPCSimulationBackend) SimulateBundle(ctx context.Context, bundle *SendMevBundleArgs, aux *SimMevBundleAuxArgs) (*SimMevBundleResponse, error) {
	var result SimMevBundleResponse
	err := b.client.CallFor(ctx, &result, "mev_simBundle", bundle, aux)
	return &result, err
}

type RedisHintBackend struct {
	client     *redis.Client
	pubChannel string
}

func NewRedisHintBackend(redisClient *redis.Client, pubChannel string) *RedisHintBackend {
	return &RedisHintBackend{
		client:     redisClient,
		pubChannel: pubChannel,
	}
}

func (b *RedisHintBackend) NotifyHint(ctx context.Context, hint *Hint) error {
	data, err := json.Marshal(hint)
	if err != nil {
		return err
	}
	return b.client.Publish(ctx, b.pubChannel, data).Err()
}

type JSONRPCBuilder struct {
	url    string
	client jsonrpc.RPCClient
}

func NewJSONRPCBuilder(url string) *JSONRPCBuilder {
	return &JSONRPCBuilder{
		url:    url,
		client: jsonrpc.NewClient(url),
	}
}

func (b *JSONRPCBuilder) String() string {
	return b.url
}

func (b *JSONRPCBuilder) SendMatchedShareBundle(ctx context.Context, bundle *SendMevBundleArgs) error {
	res, err := b.client.Call(ctx, "mev_sendBundle", []*SendMevBundleArgs{bundle})
	if err != nil {
		return err
	}
	if res.Error != nil {
		return res.Error
	}
	return nil
}

func (b *JSONRPCBuilder) CancelBundleByHash(ctx context.Context, hash common.Hash) error {
	res, err := b.client.Call(ctx, "mev_cancelBundleByHash", []common.Hash{hash})
	if err != nil {
		return err
	}
	if res.Error != nil {
		return res.Error
	}
	return nil
}
