package mevshare

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var ErrCantConvertToRefRecBundle = errors.New("can't convert bundle to ref recipient bundle")

func extractTxsForRefundRecipientBundle(depth int, bundle *SendMevBundleArgs) (txs []hexutil.Bytes, canRevert []common.Hash, err error) {
	if depth > MaxNestingLevel {
		return nil, nil, ErrInvalidBundleBody
	}
	if depth > 0 && len(bundle.Validity.Refund) > 0 {
		// only top-level refund is acceptable
		return nil, nil, ErrCantConvertToRefRecBundle
	}
	for i, el := range bundle.Body {
		if el.Tx != nil {
			txs = append(txs, *el.Tx)
			if el.CanRevert {
				if bundle.Metadata == nil {
					return txs, canRevert, ErrNilBundleMetadata
				}
				if len(bundle.Metadata.BodyHashes) > i {
					canRevert = append(canRevert, bundle.Metadata.BodyHashes[i])
				} else {
					return txs, canRevert, ErrInvalidBundleBody
				}
			}
		} else if el.Bundle != nil {
			t, r, err := extractTxsForRefundRecipientBundle(depth+1, el.Bundle)
			if err != nil {
				return txs, canRevert, err
			}
			txs = append(txs, t...)
			canRevert = append(canRevert, r...)
		} else {
			return txs, canRevert, ErrInvalidBundleBody
		}
	}
	return txs, canRevert, nil
}

func extractRefundDataForRefundRecipientData(bundle *SendMevBundleArgs) (percent *int, address *common.Address, err error) {
	if len(bundle.Validity.Refund) == 0 {
		// no refund is specified
		return nil, nil, nil
	}
	if len(bundle.Validity.Refund) > 1 {
		// only one refund is allowed
		return nil, nil, ErrCantConvertToRefRecBundle
	}

	// exactly one refund

	perc := bundle.Validity.Refund[0].Percent
	idx := bundle.Validity.Refund[0].BodyIdx

	if len(bundle.Body) <= idx {
		return nil, nil, ErrInvalidBundleConstraints
	}
	refundedBody := bundle.Body[idx]
	if refundedBody.Tx != nil {
		if idx == 0 {
			// we can use the fact that the sender of the first tx in the bundle will be refunded
			return &perc, nil, nil
		} else {
			// we should put tx sender here
			return nil, nil, ErrCantConvertToRefRecBundle
		}
	} else if refundedBody.Bundle != nil {
		refundConfig := refundedBody.Bundle.Validity.RefundConfig
		if len(refundConfig) == 0 {
			// same as in one tx case
			if idx == 0 {
				return &perc, nil, nil
			} else {
				return nil, nil, ErrCantConvertToRefRecBundle
			}
		}
		if len(refundConfig) > 1 {
			// only one refund in refund config is allowed
			return nil, nil, ErrCantConvertToRefRecBundle
		}
		// exactly one refund in config
		correctedPercent := refundConfig[0].Percent * perc / 100
		addr := refundConfig[0].Address
		return &correctedPercent, &addr, nil
	} else {
		return nil, nil, ErrInvalidBundleBody
	}
}

func ConvertBundleToRefundRecipient(bundle *SendMevBundleArgs) (res SendRefundRecBundleArgs, err error) {
	res.BlockNumber = bundle.Inclusion.BlockNumber
	txs, canRevert, err := extractTxsForRefundRecipientBundle(0, bundle)
	if err != nil {
		return res, err
	}
	res.Txs = txs
	res.RevertingTxHashes = canRevert

	percent, address, err := extractRefundDataForRefundRecipientData(bundle)
	if err != nil {
		return res, err
	}
	res.RefundPercent = percent
	res.RefundRecipient = address

	return res, err
}
