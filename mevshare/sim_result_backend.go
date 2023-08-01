package mevshare

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/flashbots/mev-share-node/simqueue"
	"go.uber.org/zap"
)

// SimulationResult is responsible for processing simulation results
// NOTE: That error should be returned only if simulation should be retried, for example if redis is down or none of the builders responded
type SimulationResult interface {
	SimulatedBundle(ctx context.Context, args *SendMevBundleArgs, result *SimMevBundleResponse, info simqueue.QueueItemInfo) error
}

type Storage interface {
	InsertBundleForStats(ctx context.Context, bundle *SendMevBundleArgs, result *SimMevBundleResponse) error
	InsertBundleForBuilder(ctx context.Context, bundle *SendMevBundleArgs, result *SimMevBundleResponse) error
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
	bundle *SendMevBundleArgs, sim *SimMevBundleResponse, _ simqueue.QueueItemInfo,
) error {
	start := time.Now()

	var hash common.Hash
	if bundle.Metadata != nil {
		hash = bundle.Metadata.BundleHash
	}
	logger := s.log.With(zap.String("bundle", hash.Hex()))

	ctx, cancelCtx := context.WithCancel(ctx)
	defer cancelCtx()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		start := time.Now()
		err := s.store.InsertBundleForStats(ctx, bundle, sim)
		logger.Debug("Inserted bundle for stats", zap.Duration("duration", time.Since(start)), zap.Error(err))
		if err != nil {
			if errors.Is(err, ErrKnownBundle) {
				logger.Debug("Bundle already known", zap.Error(err))
				cancelCtx()
				return
			}
			logger.Error("Failed to insert bundle for stats", zap.Error(err))
		}
		start = time.Now()
		err = s.store.InsertBundleForBuilder(ctx, bundle, sim)
		logger.Debug("Inserted bundle for builder", zap.Duration("duration", time.Since(start)), zap.Error(err))
		if err != nil {
			logger.Error("Failed to insert bundle for builder", zap.Error(err))
		}
	}()

	// failed bundle does not go to the builder and does not go to the hint backend
	if sim.Success {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			err := s.ProcessHints(ctx, bundle, sim)
			logger.Debug("Processed hints", zap.Duration("duration", time.Since(start)), zap.Error(err))
			if err != nil {
				logger.Error("Failed to process hints", zap.Error(err))
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.builders.SendBundle(ctx, logger, bundle)
		}()
	}

	wg.Wait()
	log.Info("Bundle processed", zap.String("bundle", hash.Hex()), zap.Duration("duration", time.Since(start)))
	return nil
}

func (s *SimulationResultBackend) ProcessHints(ctx context.Context, bundle *SendMevBundleArgs, sim *SimMevBundleResponse) error {
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

	block, err := s.eth.BlockNumber(ctx)
	if err != nil {
		return err
	}
	err = s.store.InsertHistoricalHint(ctx, block, &extractedHints)
	if err != nil {
		return err
	}

	return nil
}
