package mevshare

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/flashbots/mev-share-node/simqueue"
	"go.uber.org/zap"
)

// SimulationResult is responsible for processing simulation results
// NOTE: That error should be returned only if simulation should be retried, for example if redis is down or none of the builders responded
type SimulationResult interface {
	SimulatedBundle(ctx context.Context, args *SendMevBundleArgsV1, result *SimMevBundleResponse, info simqueue.QueueItemInfo) error
}

type Storage interface {
	InsertBundleForStats(ctx context.Context, bundle *SendMevBundleArgsV1, result *SimMevBundleResponse) error
	InsertBundleForBuilder(ctx context.Context, bundle *SendMevBundleArgsV1, result *SimMevBundleResponse) error
	InsertHistoricalHint(ctx context.Context, currentBlock uint64, hint *Hint) error
}

type SimulationResultBackend struct {
	log              *zap.Logger
	hint             HintBackend
	eth              EthClient
	store            Storage
	builders         BuildersBackend
	shareGasUsed     bool
	shareMevGasPrice bool
}

func NewSimulationResultBackend(log *zap.Logger, hint HintBackend, builders BuildersBackend, eth EthClient, store Storage, shareGasUsed, shareMevGasPrice bool) *SimulationResultBackend {
	return &SimulationResultBackend{
		log:              log,
		hint:             hint,
		builders:         builders,
		eth:              eth,
		store:            store,
		shareGasUsed:     shareGasUsed,
		shareMevGasPrice: shareMevGasPrice,
	}
}

// SimulatedBundle is called when simulation is done
// NOTE: we return error only if we want to retry the simulation
func (s *SimulationResultBackend) SimulatedBundle(ctx context.Context,
	bundle *SendMevBundleArgsV1, sim *SimMevBundleResponse, _ simqueue.QueueItemInfo,
) error {
	var hash common.Hash
	if bundle.Metadata != nil {
		hash = bundle.Metadata.BundleHash
	}
	logger := s.log.With(zap.String("bundle", hash.Hex()))

	// failed bundle does not go to the builder
	err := s.store.InsertBundleForStats(ctx, bundle, sim)
	if err != nil {
		if errors.Is(err, ErrKnownBundle) {
			logger.Debug("Bundle already known", zap.Error(err))
			return nil
		}
		logger.Error("Failed to insert bundle for stats", zap.Error(err))
	}
	if !sim.Success {
		return nil
	}

	err = s.ProcessHints(ctx, bundle, sim)
	if err != nil {
		logger.Error("Failed to process hints", zap.Error(err))
	}

	s.builders.SendBundle(ctx, logger, bundle)

	err = s.store.InsertBundleForBuilder(ctx, bundle, sim)
	if err != nil {
		logger.Error("Failed to insert bundle for builder", zap.Error(err))
	}

	return nil
}

func (s *SimulationResultBackend) ProcessHints(ctx context.Context, bundle *SendMevBundleArgsV1, sim *SimMevBundleResponse) error {
	if bundle.Privacy == nil {
		return nil
	}
	if !bundle.Privacy.Hints.HasHint(HintHash) {
		return nil
	}

	extractedHints, err := ExtractHints(bundle, sim, s.shareGasUsed, s.shareMevGasPrice)
	if err != nil {
		return err
	}
	err = s.hint.NotifyHint(ctx, &extractedHints)
	if err != nil {
		return err
	}

	// Persist historical hint
	go func(ctx context.Context) {
		block, err := s.eth.BlockNumber(ctx)
		if err != nil {
			s.log.Error("Failed to get block number", zap.Error(err))
			return
		}
		err = s.store.InsertHistoricalHint(ctx, block, &extractedHints)
		if err != nil {
			s.log.Error("Failed to insert historical hint", zap.Error(err))
		}
	}(context.Background())

	return nil
}
