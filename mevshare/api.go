package mevshare

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/flashbots/mev-share-node/jsonrpcserver"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

var (
	ErrInvalidInclusion      = errors.New("invalid inclusion")
	ErrInvalidBundleBodySize = errors.New("invalid bundle body size")
	ErrInvalidBundleBody     = errors.New("invalid bundle body")
	ErrBackrunNotFound       = errors.New("backrun not found")
	ErrBackrunInvalidBundle  = errors.New("backrun invalid bundle")
	ErrBackrunInclusion      = errors.New("backrun invalid inclusion")

	ErrInternalServiceError = errors.New("mev-share service error")
	ErrKnownBundle          = errors.New("bundle already known")

	simBundleTimeout    = 500 * time.Millisecond
	cancelBundleTimeout = 3 * time.Second
	bundleCacheSize     = 1000
)

type SimScheduler interface {
	ScheduleBundleSimulation(ctx context.Context, bundle *SendMevBundleArgs, highPriority bool) error
}

type BundleStorage interface {
	GetBundle(ctx context.Context, hash common.Hash) (*SendMevBundleArgs, error)
	CancelBundleByHash(ctx context.Context, hash common.Hash, signer common.Address) error
}

type EthClient interface {
	BlockNumber(ctx context.Context) (uint64, error)
}

type API struct {
	log *zap.Logger

	scheduler      SimScheduler
	bundleStorage  BundleStorage
	eth            EthClient
	signer         types.Signer
	simBackends    []SimulationBackend
	simRateLimiter *rate.Limiter
	builders       []BuilderBackend

	knownBundleCache *lru.Cache[common.Hash, struct{}]
}

func NewAPI(log *zap.Logger, scheduler SimScheduler, bundleStorage BundleStorage, eth EthClient, signer types.Signer, simBackends []SimulationBackend, simRateLimit rate.Limit, builders []BuilderBackend) *API {
	return &API{
		log: log,

		scheduler:      scheduler,
		bundleStorage:  bundleStorage,
		eth:            eth,
		signer:         signer,
		simBackends:    simBackends,
		simRateLimiter: rate.NewLimiter(simRateLimit, 1),
		builders:       builders,

		knownBundleCache: lru.NewCache[common.Hash, struct{}](bundleCacheSize),
	}
}

func (m *API) SendBundle(ctx context.Context, bundle SendMevBundleArgs) (SendMevBundleResponse, error) {
	logger := m.log

	currentBlock, err := m.eth.BlockNumber(ctx)
	if err != nil {
		logger.Error("failed to get current block", zap.Error(err))
		return SendMevBundleResponse{}, ErrInternalServiceError
	}

	hash, hasUnmatchedHash, err := ValidateBundle(&bundle, currentBlock, m.signer)
	if err != nil {
		logger.Warn("failed to validate bundle", zap.Error(err))
		return SendMevBundleResponse{}, err
	}
	if _, ok := m.knownBundleCache.Get(hash); ok {
		logger.Debug("bundle already known, ignoring", zap.String("hash", hash.Hex()))
		return SendMevBundleResponse{hash}, nil
	}
	m.knownBundleCache.Add(hash, struct{}{})

	signerAddress := jsonrpcserver.GetSigner(ctx)
	origin := jsonrpcserver.GetOrigin(ctx)
	if bundle.Metadata == nil {
		bundle.Metadata = &MevBundleMetadata{}
	}
	bundle.Metadata.Signer = signerAddress
	bundle.Metadata.ReceivedAt = hexutil.Uint64(uint64(time.Now().UnixMicro()))
	bundle.Metadata.OriginID = origin

	if hasUnmatchedHash {
		var unmatchedHash common.Hash
		if len(bundle.Body) > 0 && bundle.Body[0].Hash != nil {
			unmatchedHash = *bundle.Body[0].Hash
		} else {
			return SendMevBundleResponse{}, ErrInternalServiceError
		}

		unmatchedBundle, err := m.bundleStorage.GetBundle(ctx, unmatchedHash)
		if err != nil {
			return SendMevBundleResponse{}, ErrBackrunNotFound
		}
		if privacy := unmatchedBundle.Privacy; privacy == nil && privacy.Hints.HasHint(HintHash) {
			// if the unmatched bundle have not configured privacy or has not set the hash hint
			// then we cannot backrun it
			return SendMevBundleResponse{}, ErrBackrunInvalidBundle
		}
		bundle.Body[0].Bundle = unmatchedBundle
		bundle.Body[0].Hash = nil

		// send 90 % of the refund to the unmatched bundle or the suggested refund if set
		refundPercent := RefundPercent
		if unmatchedBundle.Privacy != nil && unmatchedBundle.Privacy.WantRefund != nil {
			refundPercent = *unmatchedBundle.Privacy.WantRefund
		}
		bundle.Validity.Refund = []RefundConstraint{{0, refundPercent}}
		MergePrivacyBuilders(&bundle)
		err = MergeInclusionIntervals(&bundle.Inclusion, &unmatchedBundle.Inclusion)
		if err != nil {
			return SendMevBundleResponse{}, ErrBackrunInclusion
		}
	}

	highPriority := jsonrpcserver.GetPriority(ctx)
	err = m.scheduler.ScheduleBundleSimulation(ctx, &bundle, highPriority)
	if err != nil {
		logger.Error("Failed to schedule bundle simulation", zap.Error(err))
		return SendMevBundleResponse{}, ErrInternalServiceError
	}

	return SendMevBundleResponse{
		BundleHash: hash,
	}, nil
}

func (m *API) SimBundle(ctx context.Context, bundle SendMevBundleArgs, aux SimMevBundleAuxArgs) (*SimMevBundleResponse, error) {
	if len(m.simBackends) == 0 {
		return nil, ErrInternalServiceError
	}
	ctx, cancel := context.WithTimeout(ctx, simBundleTimeout)
	defer cancel()

	simTimeout := int64(simBundleTimeout / time.Millisecond)
	aux.Timeout = &simTimeout

	err := m.simRateLimiter.Wait(ctx)
	if err != nil {
		return nil, err
	}

	// select random backend
	idx := rand.Intn(len(m.simBackends)) //nolint:gosec
	backend := m.simBackends[idx]
	return backend.SimulateBundle(ctx, &bundle, &aux)
}

// CancelBundleByHash cancels a bundle by hash
// This method is not exposed on the bundle relay.
// However, it is used by the Flashbots bundle relay for now to handle the cancellation of private transactions.
func (m *API) CancelBundleByHash(ctx context.Context, hash common.Hash) error {
	ctx, cancel := context.WithTimeout(ctx, cancelBundleTimeout)
	defer cancel()
	signerAddress := jsonrpcserver.GetSigner(ctx)
	err := m.bundleStorage.CancelBundleByHash(ctx, hash, signerAddress)
	if err != nil {
		return ErrBundleNotCancelled
	}

	for _, builder := range m.builders {
		err := builder.CancelBundleByHash(ctx, hash)
		if err != nil {
			m.log.Warn("Failed to cancel bundle by hash", zap.Error(err), zap.String("builder", builder.String()))
		}
	}
	m.log.Info("Bundle cancelled", zap.String("hash", hash.Hex()))
	return nil
}
