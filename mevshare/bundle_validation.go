package mevshare

import (
	"errors"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/crypto/sha3"
)

var (
	ErrUnsupportedBundleVersion = errors.New("unsupported bundle version")
	ErrBundleTooDeep            = errors.New("bundle too deep")
	ErrInvalidBundleConstraints = errors.New("invalid bundle constraints")
	ErrInvalidBundlePrivacy     = errors.New("invalid bundle privacy")
)

func cleanBody(bundle *SendMevBundleArgs) {
	for _, el := range bundle.Body {
		if el.Hash != nil {
			el.Tx = nil
			el.Bundle = nil
		}
		if el.Tx != nil {
			el.Hash = nil
			el.Bundle = nil
		}
		if el.Bundle != nil {
			el.Hash = nil
			el.Tx = nil
		}
	}
}

// MergeInclusionIntervals writes to the topLevel inclusion value of overlap between inner and topLevel
// or return error if there is no overlap
func MergeInclusionIntervals(topLevel, inner *MevBundleInclusion) error {
	if topLevel.MaxBlock < inner.BlockNumber || inner.MaxBlock < topLevel.BlockNumber {
		return ErrInvalidInclusion
	}

	if topLevel.BlockNumber < inner.BlockNumber {
		topLevel.BlockNumber = inner.BlockNumber
	}
	if topLevel.MaxBlock > inner.MaxBlock {
		topLevel.MaxBlock = inner.MaxBlock
	}
	return nil
}

// MergeBuilders writes to the topLevel builder value of overlap between inner and topLevel
func MergeBuilders(topLevel, inner *MevBundlePrivacy) {
	if topLevel == nil {
		return
	}
	if inner == nil {
		topLevel.Builders = nil
		return
	}
	topLevel.Builders = Intersect(topLevel.Builders, inner.Builders)
}

func validateBundleInner(level int, bundle *SendMevBundleArgs, currentBlock uint64, signer types.Signer) (hash common.Hash, txs int, unmatched bool, err error) { //nolint:gocognit,gocyclo
	if level > MaxNestingLevel {
		return hash, txs, unmatched, ErrBundleTooDeep
	}
	if bundle.Version != "beta-1" && bundle.Version != "v0.1" {
		return hash, txs, unmatched, ErrUnsupportedBundleVersion
	}

	// validate inclusion
	if bundle.Inclusion.MaxBlock == 0 {
		bundle.Inclusion.MaxBlock = bundle.Inclusion.BlockNumber
	}
	minBlock := uint64(bundle.Inclusion.BlockNumber)
	maxBlock := uint64(bundle.Inclusion.MaxBlock)
	if maxBlock < minBlock {
		return hash, txs, unmatched, ErrInvalidInclusion
	}
	if (maxBlock - minBlock) > MaxBlockRange {
		return hash, txs, unmatched, ErrInvalidInclusion
	}
	if currentBlock >= maxBlock {
		return hash, txs, unmatched, ErrInvalidInclusion
	}
	if minBlock > currentBlock+MaxBlockOffset {
		return hash, txs, unmatched, ErrInvalidInclusion
	}

	// validate body
	cleanBody(bundle)
	if len(bundle.Body) == 0 {
		return hash, txs, unmatched, ErrInvalidBundleBodySize
	}

	bodyHashes := make([]common.Hash, 0, len(bundle.Body))
	for i, el := range bundle.Body {
		if el.Hash != nil {
			// make sure that we have up to one unmatched element and only at the beginning of the body
			if unmatched || i != 0 {
				return hash, txs, unmatched, ErrInvalidBundleBody
			}
			unmatched = true
			bodyHashes = append(bodyHashes, *el.Hash)
			if len(bundle.Body) == 1 {
				// we have unmatched bundle without anything else
				return hash, txs, unmatched, ErrInvalidBundleBody
			}
			txs++
		} else if el.Tx != nil {
			var tx types.Transaction
			err := tx.UnmarshalBinary(*el.Tx)
			if err != nil {
				return hash, txs, unmatched, err
			}
			bodyHashes = append(bodyHashes, tx.Hash())
			txs++
		} else if el.Bundle != nil {
			err = MergeInclusionIntervals(&bundle.Inclusion, &el.Bundle.Inclusion)
			if err != nil {
				return hash, txs, unmatched, err
			}
			MergeBuilders(bundle.Privacy, el.Bundle.Privacy)
			// hash, tx count, has backrun?
			h, t, u, err := validateBundleInner(level+1, el.Bundle, currentBlock, signer)
			if err != nil {
				return hash, txs, unmatched, err
			}
			bodyHashes = append(bodyHashes, h)
			txs += t
			// don't allow unmatched bundles below 1-st level
			if u {
				return hash, txs, unmatched, ErrInvalidBundleBody
			}
		}
	}
	if txs > MaxBodySize {
		return hash, txs, unmatched, ErrInvalidBundleBodySize
	}

	if len(bodyHashes) == 1 {
		// special case of bundle with a single tx
		hash = bodyHashes[0]
	} else {
		hasher := sha3.NewLegacyKeccak256()
		for _, h := range bodyHashes {
			hasher.Write(h[:])
		}
		hash = common.BytesToHash(hasher.Sum(nil))
	}

	// validate validity
	if unmatched && len(bundle.Validity.Refund) > 0 {
		// refunds should be empty for unmatched bundles
		return hash, txs, unmatched, ErrInvalidBundleConstraints
	}
	totalRefundConfigPercent := 0
	for _, c := range bundle.Validity.RefundConfig {
		percent := c.Percent
		if percent < 0 || percent > 100 {
			return hash, txs, unmatched, ErrInvalidBundleConstraints
		}
		totalRefundConfigPercent += c.Percent
	}
	if totalRefundConfigPercent > 100 {
		return hash, txs, unmatched, ErrInvalidBundleConstraints
	}

	usedBodyPos := make(map[int]struct{})
	totalPercent := 0
	for _, c := range bundle.Validity.Refund {
		if c.BodyIdx >= len(bundle.Body) || c.BodyIdx < 0 {
			return hash, txs, unmatched, ErrInvalidBundleConstraints
		}
		if _, ok := usedBodyPos[c.BodyIdx]; ok {
			return hash, txs, unmatched, ErrInvalidBundleConstraints
		}
		usedBodyPos[c.BodyIdx] = struct{}{}

		if c.Percent < 0 || c.Percent > 100 {
			return hash, txs, unmatched, ErrInvalidBundleConstraints
		}
		totalPercent += c.Percent
	}
	if totalPercent > 100 {
		return hash, txs, unmatched, ErrInvalidBundleConstraints
	}

	// validate privacy
	if unmatched && bundle.Privacy != nil && bundle.Privacy.Hints != HintNone {
		return hash, txs, unmatched, ErrInvalidBundlePrivacy
	}

	if bundle.Privacy != nil {
		if bundle.Privacy.Hints != HintNone {
			bundle.Privacy.Hints.SetHint(HintHash)
		}
		if r := bundle.Privacy.WantRefund; r != nil && (*r < 0 || *r > 100) {
			return hash, txs, unmatched, ErrInvalidBundlePrivacy
		}
		// make builders lowercase
		for i, b := range bundle.Privacy.Builders {
			bundle.Privacy.Builders[i] = strings.ToLower(b)
		}
	}

	// clean metadata
	// clean fields owned by the node
	bundle.Metadata = &MevBundleMetadata{}
	bundle.Metadata.BundleHash = hash
	bundle.Metadata.BodyHashes = bodyHashes

	return hash, txs, unmatched, nil
}

func ValidateBundle(bundle *SendMevBundleArgs, currentBlock uint64, signer types.Signer) (hash common.Hash, unmatched bool, err error) {
	hash, _, unmatched, err = validateBundleInner(0, bundle, currentBlock, signer)
	return hash, unmatched, err
}

func mergePrivacyBuildersInner(bundle *SendMevBundleArgs, topLevel *MevBundlePrivacy) {
	MergeBuilders(topLevel, bundle.Privacy)
	for _, el := range bundle.Body {
		if el.Bundle != nil {
			mergePrivacyBuildersInner(el.Bundle, topLevel)
		}
	}
}

// MergePrivacyBuilders Sets privacy.builders to the intersection of all privacy.builders in the bundle
func MergePrivacyBuilders(bundle *SendMevBundleArgs) {
	mergePrivacyBuildersInner(bundle, bundle.Privacy)
}
