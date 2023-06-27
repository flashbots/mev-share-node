package mevshare

import (
	"context"
	"errors"
	"strings"

	"github.com/ybbus/jsonrpc/v3"
	"go.uber.org/zap"
)

type BuilderAPI uint8

const (
	BuilderAPIRefRecipient BuilderAPI = iota
	BuilderAPIMevShareBeta1
)

var ErrInvalidExternalBuilder = errors.New("invalid external builder")

type ExternalBuilder struct {
	Name   string
	Client jsonrpc.RPCClient
	API    BuilderAPI
}

func (b *ExternalBuilder) SendBundle(ctx context.Context, bundle *SendMevBundleArgs) error {
	switch b.API {
	case BuilderAPIRefRecipient:
		refRec, err := ConvertBundleToRefundRecipient(bundle)
		if err != nil {
			return err
		}
		res, err := b.Client.Call(ctx, "eth_sendBundle", []SendRefundRecBundleArgs{refRec})
		if err != nil {
			return err
		}
		if res.Error != nil {
			return res.Error
		}
	case BuilderAPIMevShareBeta1:
		// clean metadata
		args := *bundle
		args.Metadata = MevBundleMetadata{}
		res, err := b.Client.Call(ctx, "mev_sendBundle", []SendMevBundleArgs{args})
		if err != nil {
			return err
		}
		if res.Error != nil {
			return res.Error
		}
	}
	return nil
}

type ExternalBuildersBackend struct {
	Builders map[string]ExternalBuilder
}

func NewExternalBuildersBackend(builders []ExternalBuilder) *ExternalBuildersBackend {
	builderMap := make(map[string]ExternalBuilder)
	for _, builder := range builders {
		builderMap[builder.Name] = builder
	}
	return &ExternalBuildersBackend{
		Builders: builderMap,
	}
}

func ParseExternalBuilders(str string) (*ExternalBuildersBackend, error) {
	if str == "" {
		return NewExternalBuildersBackend(nil), nil
	}
	builders := strings.Split(str, ";")
	externalBuilders := make([]ExternalBuilder, 0, len(builders))
	for _, builder := range builders {
		builderParts := strings.Split(builder, ",")
		if len(builderParts) != 3 {
			return nil, ErrInvalidExternalBuilder
		}

		var api BuilderAPI
		switch builderParts[2] {
		case "refund-recipient":
			api = BuilderAPIRefRecipient
		case "beta-1.0":
			api = BuilderAPIMevShareBeta1
		default:
			return nil, ErrInvalidExternalBuilder
		}

		externalBuilders = append(externalBuilders, ExternalBuilder{
			Name:   builderParts[0],
			Client: jsonrpc.NewClient(builderParts[1]),
			API:    api,
		})
	}

	return NewExternalBuildersBackend(externalBuilders), nil
}

func (b *ExternalBuildersBackend) SendBundle(ctx context.Context, logger *zap.Logger, bundle *SendMevBundleArgs) {
	if bundle.Privacy != nil && len(bundle.Privacy.Builders) > 0 {
		buildersUsed := make(map[string]struct{})
		for _, target := range bundle.Privacy.Builders {
			if target == "default" || target == "flashbots" {
				// right now we always send to flashbots and default means flashbots
				continue
			}
			if _, ok := buildersUsed[target]; ok {
				continue
			}
			buildersUsed[target] = struct{}{}
			if builder, ok := b.Builders[target]; ok {
				err := builder.SendBundle(ctx, bundle)
				if err != nil {
					logger.Warn("failed to send bundle to external builder", zap.Error(err), zap.String("builder", builder.Name))
				}
			} else {
				logger.Warn("unknown external builder", zap.String("builder", target))
			}
		}
	}
}
