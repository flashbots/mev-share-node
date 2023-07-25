package mevshare

import (
	"context"
	"errors"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ybbus/jsonrpc/v3"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type BuilderAPI uint8

const (
	BuilderAPIRefRecipient BuilderAPI = iota
	BuilderAPIMevShareBeta1
	BuilderAPIMevShareV2
)

var ErrInvalidBuilder = errors.New("invalid builder specification")

type BuildersConfig struct {
	Builders []struct {
		Name     string `yaml:"name"`
		URL      string `yaml:"url"`
		API      string `yaml:"api"`
		Internal bool   `yaml:"internal"`
	} `yaml:"builders"`
}

// LoadBuilderConfig parses a builder config from a file
func LoadBuilderConfig(file string) (BuildersBackend, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return BuildersBackend{}, err
	}

	var config BuildersConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return BuildersBackend{}, err
	}

	externalBuilders := make([]JSONRPCBuilderBackend, 0)
	internalBuilders := make([]JSONRPCBuilderBackend, 0)
	for _, builder := range config.Builders {
		var api BuilderAPI
		switch builder.API {
		case "refund-recipient":
			api = BuilderAPIRefRecipient
		case VersionV1:
			api = BuilderAPIMevShareBeta1
		case VersionV2:
			api = BuilderAPIMevShareV2
		default:
			return BuildersBackend{}, ErrInvalidBuilder
		}

		builderBackend := JSONRPCBuilderBackend{
			Name:   builder.Name,
			Client: jsonrpc.NewClient(builder.URL),
			API:    api,
		}

		if builder.Internal {
			internalBuilders = append(internalBuilders, builderBackend)
		} else {
			externalBuilders = append(externalBuilders, builderBackend)
		}
	}

	externalBuilderMap := make(map[string]JSONRPCBuilderBackend)
	for _, builder := range externalBuilders {
		externalBuilderMap[builder.Name] = builder
	}

	return BuildersBackend{
		externalBuilders: externalBuilderMap,
		internalBuilders: internalBuilders,
	}, nil
}

type JSONRPCBuilderBackend struct {
	Name   string
	Client jsonrpc.RPCClient
	API    BuilderAPI
}

func (b *JSONRPCBuilderBackend) SendBundle(ctx context.Context, bundle *SendMevBundleArgsV1) error {
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
		res, err := b.Client.Call(ctx, "mev_sendBundle", []SendMevBundleArgsV1{*bundle})
		if err != nil {
			return err
		}
		if res.Error != nil {
			return res.Error
		}
	case BuilderAPIMevShareV2:
		mergedBundle, err := ConvertV1BundleToMergedBundleV2(bundle)
		if err != nil {
			return err
		}
		res, err := b.Client.Call(ctx, "mev_sendMergedBundle", []SendMergedBundleArgsV2{*mergedBundle})
		if err != nil {
			return err
		}
		if res.Error != nil {
			return res.Error
		}
	}
	return nil
}

func (b *JSONRPCBuilderBackend) CancelBundleByHash(ctx context.Context, hash common.Hash) error {
	res, err := b.Client.Call(ctx, "mev_cancelBundleByHash", []common.Hash{hash})
	if err != nil {
		return err
	}
	if res.Error != nil {
		return res.Error
	}
	return nil
}

type BuildersBackend struct {
	externalBuilders map[string]JSONRPCBuilderBackend
	internalBuilders []JSONRPCBuilderBackend
}

func (b *BuildersBackend) SendBundle(ctx context.Context, logger *zap.Logger, bundle *SendMevBundleArgsV1) {
	for _, builder := range b.internalBuilders {
		err := builder.SendBundle(ctx, bundle)
		if err != nil {
			logger.Warn("failed to send bundle to internal builder", zap.Error(err), zap.String("builder", builder.Name))
		}
	}

	if bundle.Privacy != nil && len(bundle.Privacy.Builders) > 0 {
		// clean metadata, privacy
		args := *bundle
		// it should already be cleaned while matching, but just in case we do it again here
		MergePrivacyBuilders(&args)
		builders := args.Privacy.Builders
		cleanBundle(&args)

		buildersUsed := make(map[string]struct{})
		for _, target := range builders {
			if target == "default" || target == "flashbots" {
				// right now we always send to flashbots and default means flashbots
				continue
			}
			if _, ok := buildersUsed[target]; ok {
				continue
			}
			buildersUsed[target] = struct{}{}
			if builder, ok := b.externalBuilders[target]; ok {
				err := builder.SendBundle(ctx, &args)
				if err != nil {
					logger.Warn("failed to send bundle to external builder", zap.Error(err), zap.String("builder", builder.Name))
				}
			} else {
				logger.Warn("unknown external builder", zap.String("builder", target))
			}
		}
	}
}

func (b *BuildersBackend) CancelBundleByHash(ctx context.Context, logger *zap.Logger, hash common.Hash) {
	// we cancel bundle only in the internal builders, external cancellations are not supported
	for _, builder := range b.internalBuilders {
		err := builder.CancelBundleByHash(ctx, hash)
		if err != nil {
			logger.Warn("failed to cancel bundle to internal builder", zap.Error(err), zap.String("builder", builder.Name))
		}
	}
}

func cleanBundle(bundle *SendMevBundleArgsV1) {
	for _, el := range bundle.Body {
		if el.Bundle != nil {
			cleanBundle(el.Bundle)
		}
	}
	bundle.Privacy = nil
	bundle.Metadata = nil
}
