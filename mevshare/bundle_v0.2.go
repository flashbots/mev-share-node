package mevshare

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var (
	ErrMustBeV2Bundle           = errors.New("must be v2 bundle")
	ErrExtensionNotSupported    = errors.New("extension not supported")
	ErrMergedBundleTooComplex   = errors.New("external merged bundles can only have 2 elements in the body with possible refund in position 1")
	ErrMergedV1BundleTooComplex = errors.New("v0.1 merged bundles can only be all txs or a simple backruns to be converted to v0.2")
)

type SendMevBundleArgsV2 struct {
	Version    string               `json:"version"`
	Extensions []string             `json:"extensions"`
	Inclusion  MevBundleInclusion   `json:"inclusion"`
	Body       []MevBundleBodyV2    `json:"body"`
	Privacy    *MevBundlePrivacyV2  `json:"privacy,omitempty"`
	Validity   *MevBundleValidityV2 `json:"validity,omitempty"`
}

type MevBundleBodyV2 struct {
	Hash     *common.Hash   `json:"hash,omitempty"`
	Tx       *hexutil.Bytes `json:"tx,omitempty"`
	Optional bool           `json:"optional,omitempty"`
}

type MevBundlePrivacyV2 struct {
	Hints    HintIntent `json:"hints,omitempty"`
	Builders []string   `json:"builders,omitempty"`
}

type MevBundleValidityV2 struct {
	Refund []RefundConfig `json:"refund,omitempty"`
}

func (b *SendMevBundleArgsV2) ToV1Bundle() (*SendMevBundleArgsV1, error) {
	var result SendMevBundleArgsV1
	result.Version = VersionV1

	if b.Version != VersionV2 {
		return nil, ErrMustBeV2Bundle
	}

	if len(b.Extensions) > 0 {
		return nil, ErrExtensionNotSupported
	}

	result.Inclusion.BlockNumber = b.Inclusion.BlockNumber
	result.Inclusion.MaxBlock = b.Inclusion.MaxBlock

	result.Body = make([]MevBundleBody, len(b.Body))
	for i, body := range b.Body {
		result.Body[i].CanRevert = body.Optional
		if body.Hash != nil {
			result.Body[i].Hash = body.Hash
		}
		if body.Tx != nil {
			result.Body[i].Tx = body.Tx
		}
	}

	if b.Privacy != nil {
		result.Privacy = &MevBundlePrivacy{
			Hints:      b.Privacy.Hints,
			Builders:   b.Privacy.Builders,
			WantRefund: nil,
		}
	}

	if b.Validity != nil {
		wantRefund, refundConfig, err := ConvertTotalRefundConfigToBundleV1Params(b.Validity.Refund)
		if err != nil {
			return nil, err
		}
		result.Validity.RefundConfig = refundConfig

		if result.Privacy == nil {
			result.Privacy = &MevBundlePrivacy{HintNone, nil, nil}
		}
		result.Privacy.WantRefund = &wantRefund
	}

	return &result, nil
}

type SendMergedBundleArgsV2 struct {
	Version    string                  `json:"version"`
	Extensions []string                `json:"extensions"`
	Inclusion  MergedBundleInclusionV2 `json:"inclusion"`
	Body       []MergedBundleBodyV2    `json:"body"`
}

type MergedBundleInclusionV2 struct {
	BlockNumber hexutil.Uint64 `json:"block"`
}

type MergedBundleBodyV2 struct {
	Items    []MergedBundleBodyItemV2 `json:"items"`
	Validity *MergedBundleValidityV2  `json:"validity"`
	Optional bool                     `json:"optional,omitempty"`
}

type MergedBundleBodyItemV2 struct {
	Tx       hexutil.Bytes `json:"tx,omitempty"`
	Optional bool          `json:"optional,omitempty"`
}

type MergedBundleValidityV2 struct {
	Refund []RefundConfig `json:"refund,omitempty"`
}

func (b *SendMergedBundleArgsV2) ToV1Bundle() (*SendMevBundleArgsV1, error) {
	var result SendMevBundleArgsV1
	result.Version = VersionV1

	if b.Version != VersionV2 {
		return nil, ErrMustBeV2Bundle
	}
	if len(b.Extensions) > 0 {
		return nil, ErrExtensionNotSupported
	}

	result.Inclusion.BlockNumber = b.Inclusion.BlockNumber

	if len(b.Body) == 1 {
		el := b.Body[0]
		if el.Validity != nil && len(el.Validity.Refund) > 0 {
			return nil, ErrMergedBundleTooComplex
		}
		result.Body = make([]MevBundleBody, len(el.Items))
		for i, item := range el.Items {
			tx := item.Tx
			result.Body[i].Tx = &tx
			result.Body[i].CanRevert = item.Optional
		}
		return &result, nil
	}

	if len(b.Body) != 2 {
		return nil, ErrMergedBundleTooComplex
	}

	b1 := b.Body[0]
	b2 := b.Body[1]

	if b1.Validity != nil && len(b1.Validity.Refund) > 0 {
		return nil, ErrMergedBundleTooComplex
	}

	var innerBundle SendMevBundleArgsV1
	innerBundle.Version = result.Version
	innerBundle.Inclusion = result.Inclusion
	innerBundle.Body = make([]MevBundleBody, len(b1.Items))
	for i, item := range b1.Items {
		tx := item.Tx
		innerBundle.Body[i].Tx = &tx
		if item.Optional {
			innerBundle.Body[i].CanRevert = true
		}
	}

	if b2.Validity != nil && len(b2.Validity.Refund) > 0 {
		wantRefund, refundConfig, err := ConvertTotalRefundConfigToBundleV1Params(b2.Validity.Refund)
		if err != nil {
			return nil, err
		}
		innerBundle.Validity.RefundConfig = refundConfig
		result.Validity.Refund = []RefundConstraint{{BodyIdx: 0, Percent: wantRefund}}
	}

	result.Body = []MevBundleBody{{Bundle: &innerBundle}}
	for _, item := range b2.Items {
		tx := item.Tx
		result.Body = append(result.Body, MevBundleBody{
			Tx:        &tx,
			Hash:      nil,
			Bundle:    nil,
			CanRevert: item.Optional,
		})
	}

	return &result, nil
}

func ConvertV1BundleToMergedBundleV2(bundle *SendMevBundleArgsV1) (*SendMergedBundleArgsV2, error) {
	var result SendMergedBundleArgsV2
	result.Version = VersionV2
	result.Inclusion.BlockNumber = bundle.Inclusion.BlockNumber

	if len(bundle.Validity.Refund) == 0 {
		items := make([]MergedBundleBodyItemV2, len(bundle.Body))
		for i, body := range bundle.Body {
			if body.Tx == nil {
				return nil, ErrMergedV1BundleTooComplex
			}
			items[i].Tx = *body.Tx
			items[i].Optional = body.CanRevert
		}
		result.Body = []MergedBundleBodyV2{{Items: items}}
		return &result, nil
	}

	if len(bundle.Validity.Refund) != 1 {
		return nil, ErrMergedV1BundleTooComplex
	}

	if len(bundle.Body) != 2 {
		return nil, ErrMergedV1BundleTooComplex
	}

	var body0 MergedBundleBodyV2
	if innerBundle := bundle.Body[0].Bundle; innerBundle != nil {
		items := make([]MergedBundleBodyItemV2, len(innerBundle.Body))
		for i, el := range innerBundle.Body {
			if el.Tx == nil {
				return nil, ErrMergedV1BundleTooComplex
			}
			items[i].Tx = *el.Tx
			items[i].Optional = el.CanRevert
		}
		body0.Items = items
		body0.Optional = bundle.Body[0].CanRevert
	} else if bundle.Body[0].Tx != nil {
		body0.Items = append(body0.Items, MergedBundleBodyItemV2{Tx: *bundle.Body[0].Tx, Optional: bundle.Body[0].CanRevert})
	} else {
		return nil, ErrMergedV1BundleTooComplex
	}

	var body1 MergedBundleBodyV2
	if bundle.Body[1].Tx != nil {
		body1.Items = append(body1.Items, MergedBundleBodyItemV2{Tx: *bundle.Body[1].Tx, Optional: bundle.Body[1].CanRevert})
	} else {
		return nil, ErrMergedV1BundleTooComplex
	}

	// refund for body1
	refConstraint := bundle.Validity.Refund[0]
	if refConstraint.BodyIdx != 0 {
		return nil, ErrMergedV1BundleTooComplex
	}
	refundPercent := refConstraint.Percent
	refundConfig, err := GetRefundConfigV1FromBody(&bundle.Body[0])
	if err != nil {
		return nil, err
	}
	totalRefundConfig := ConvertBundleV1ParamsToTotalRefundConfig(refundPercent, refundConfig)
	body1.Validity = &MergedBundleValidityV2{Refund: totalRefundConfig}

	result.Body = []MergedBundleBodyV2{body0, body1}
	return &result, nil
}
