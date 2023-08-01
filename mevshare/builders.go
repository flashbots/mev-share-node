package mevshare

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ybbus/jsonrpc/v3"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type BuilderAPI uint8

const (
	BuilderAPIRefRecipient BuilderAPI = iota
	BuilderAPIMevShareBeta1
)

var ErrInvalidBuilder = errors.New("invalid builder specification")

type BuildersConfig struct {
	Builders []struct {
		Name     string `yaml:"name"`
		URL      string `yaml:"url"`
		API      string `yaml:"api"`
		Internal bool   `yaml:"internal"`
		Disabled bool   `yaml:"disabled"`
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
		if builder.Disabled {
			continue
		}

		var api BuilderAPI
		switch builder.API {
		case "refund-recipient":
			api = BuilderAPIRefRecipient
		case "v0.1":
			api = BuilderAPIMevShareBeta1
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

func (b *JSONRPCBuilderBackend) SendBundle(ctx context.Context, bundle *SendMevBundleArgs) error {
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
		res, err := b.Client.Call(ctx, "mev_sendBundle", []SendMevBundleArgs{*bundle})
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

// SendBundle sends a bundle to all builders.
// Bundles are sent to all builders in parallel.
func (b *BuildersBackend) SendBundle(ctx context.Context, logger *zap.Logger, bundle *SendMevBundleArgs) { //nolint:gocognit
	var wg sync.WaitGroup

	// always send to internal builders
	internalBuildersSuccess := make([]bool, len(b.internalBuilders))
	for idx, builder := range b.internalBuilders {
		wg.Add(1)
		go func(builder JSONRPCBuilderBackend, idx int) {
			defer wg.Done()

			start := time.Now()
			err := builder.SendBundle(ctx, bundle)
			logger.Debug("Sent bundle to internal builder", zap.String("builder", builder.Name), zap.Duration("duration", time.Since(start)), zap.Error(err))

			if err != nil {
				logger.Warn("Failed to send bundle to internal builder", zap.Error(err), zap.String("builder", builder.Name))
			} else {
				internalBuildersSuccess[idx] = true
			}
		}(builder, idx)
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
				wg.Add(1)
				go func(builder JSONRPCBuilderBackend) {
					defer wg.Done()

					start := time.Now()
					err := builder.SendBundle(ctx, &args)
					logger.Debug("Sent bundle to external builder", zap.String("builder", builder.Name), zap.Duration("duration", time.Since(start)), zap.Error(err))

					if err != nil {
						logger.Warn("Failed to send bundle to external builder", zap.Error(err), zap.String("builder", builder.Name))
					}
				}(builder)
			} else {
				logger.Warn("Unknown external builder", zap.String("builder", target))
			}
		}
	}

	wg.Wait()

	sentToInternal := false
	for _, success := range internalBuildersSuccess {
		if success {
			sentToInternal = true
			break
		}
	}
	if !sentToInternal {
		logger.Error("Failed to send bundle to any of the internal builders")
	}
}

func (b *BuildersBackend) CancelBundleByHash(ctx context.Context, logger *zap.Logger, hash common.Hash) {
	var wg sync.WaitGroup
	// we cancel bundle only in the internal builders, external cancellations are not supported
	for _, builder := range b.internalBuilders {
		wg.Add(1)
		go func(builder JSONRPCBuilderBackend) {
			err := builder.CancelBundleByHash(ctx, hash)
			if err != nil {
				logger.Warn("Failed to cancel bundle on the internal builder", zap.Error(err), zap.String("builder", builder.Name))
			}
		}(builder)
	}
	wg.Wait()
}

func cleanBundle(bundle *SendMevBundleArgs) {
	for _, el := range bundle.Body {
		if el.Bundle != nil {
			cleanBundle(el.Bundle)
		}
	}
	bundle.Privacy = nil
	bundle.Metadata = nil
}
