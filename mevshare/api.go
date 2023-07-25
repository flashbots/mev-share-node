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
	ErrInvalidSendBundleArgument       = errors.New("invalid mev_sendBundle argument")
	ErrInvalidSendMergedBundleArgument = errors.New("invalid mev_sendMergedBundle argument")

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
	ScheduleBundleSimulation(ctx context.Context, bundle *SendMevBundleArgsV1, highPriority bool) error
}

type BundleStorage interface {
	GetBundle(ctx context.Context, hash common.Hash) (*SendMevBundleArgsV1, error)
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
	builders       BuildersBackend

	knownBundleCache *lru.Cache[common.Hash, struct{}]
}

func NewAPI(log *zap.Logger, scheduler SimScheduler, bundleStorage BundleStorage, eth EthClient, signer types.Signer, simBackends []SimulationBackend, simRateLimit rate.Limit, builders BuildersBackend) *API {
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

func (m *API) SendBundle(ctx context.Context, union SendBundleUnion) (SendMevBundleResponse, error) {
	logger := m.log

	var bundle *SendMevBundleArgsV1
	if union.v1bundle != nil {
		bundle = union.v1bundle
	} else if union.v2bundle != nil {
		b, err := union.v2bundle.ToV1Bundle()
		if err != nil {
			logger.Warn("failed to convert bundle", zap.Error(err))
			return SendMevBundleResponse{}, ErrInvalidSendBundleArgument
		}
		bundle = b
	} else {
		return SendMevBundleResponse{}, ErrInvalidSendBundleArgument
	}

	currentBlock, err := m.eth.BlockNumber(ctx)
	if err != nil {
		logger.Error("failed to get current block", zap.Error(err))
		return SendMevBundleResponse{}, ErrInternalServiceError
	}

	hash, hasUnmatchedHash, err := ValidateBundle(bundle, currentBlock, m.signer)
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
		MergePrivacyBuilders(bundle)
		err = MergeInclusionIntervals(&bundle.Inclusion, &unmatchedBundle.Inclusion)
		if err != nil {
			return SendMevBundleResponse{}, ErrBackrunInclusion
		}
	}

	highPriority := jsonrpcserver.GetPriority(ctx)
	err = m.scheduler.ScheduleBundleSimulation(ctx, bundle, highPriority)
	if err != nil {
		logger.Error("Failed to schedule bundle simulation", zap.Error(err))
		return SendMevBundleResponse{}, ErrInternalServiceError
	}

	return SendMevBundleResponse{
		BundleHash: hash,
	}, nil
}

func (m *API) SendMergedBundle(ctx context.Context, union SendMergedBundleArgsV2) (SendMevBundleResponse, error) {
	bundle, err := union.ToV1Bundle()
	if err != nil {
		return SendMevBundleResponse{}, ErrInvalidSendMergedBundleArgument
	}

	return m.SendBundle(ctx, SendBundleUnion{v1bundle: bundle, v2bundle: nil})
}

func (m *API) SimBundle(ctx context.Context, bundle SendMevBundleArgsV1, aux SimMevBundleAuxArgs) (*SimMevBundleResponse, error) {
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

	m.builders.CancelBundleByHash(ctx, m.log, hash)
	m.log.Info("Bundle cancelled", zap.String("hash", hash.Hex()))
	return nil
}
