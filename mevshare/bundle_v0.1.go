package mevshare

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

type SendMevBundleArgsV1 struct {
	Version   string             `json:"version"`
	Inclusion MevBundleInclusion `json:"inclusion"`
	Body      []MevBundleBody    `json:"body"`
	Validity  MevBundleValidity  `json:"validity"`
	Privacy   *MevBundlePrivacy  `json:"privacy,omitempty"`
	Metadata  *MevBundleMetadata `json:"metadata,omitempty"`
}

type MevBundleInclusion struct {
	BlockNumber hexutil.Uint64 `json:"block"`
	MaxBlock    hexutil.Uint64 `json:"maxBlock"`
}

type MevBundleBody struct {
	Hash      *common.Hash         `json:"hash,omitempty"`
	Tx        *hexutil.Bytes       `json:"tx,omitempty"`
	Bundle    *SendMevBundleArgsV1 `json:"bundle,omitempty"`
	CanRevert bool                 `json:"canRevert,omitempty"`
}

type MevBundleValidity struct {
	Refund       []RefundConstraint `json:"refund,omitempty"`
	RefundConfig []RefundConfig     `json:"refundConfig,omitempty"`
}

type RefundConstraint struct {
	BodyIdx int `json:"bodyIdx"`
	Percent int `json:"percent"`
}

type RefundConfig struct {
	Address common.Address `json:"address"`
	Percent int            `json:"percent"`
}

type MevBundlePrivacy struct {
	Hints      HintIntent `json:"hints,omitempty"`
	Builders   []string   `json:"builders,omitempty"`
	WantRefund *int       `json:"wantRefund,omitempty"`
}

type MevBundleMetadata struct {
	BundleHash common.Hash    `json:"bundleHash,omitempty"`
	BodyHashes []common.Hash  `json:"bodyHashes,omitempty"`
	Signer     common.Address `json:"signer,omitempty"`
	OriginID   string         `json:"originId,omitempty"`
	ReceivedAt hexutil.Uint64 `json:"receivedAt,omitempty"`
}

type SendMevBundleResponse struct {
	BundleHash common.Hash `json:"bundleHash"`
}

type SimMevBundleResponse struct {
	Success         bool             `json:"success"`
	Error           string           `json:"error,omitempty"`
	StateBlock      hexutil.Uint64   `json:"stateBlock"`
	MevGasPrice     hexutil.Big      `json:"mevGasPrice"`
	Profit          hexutil.Big      `json:"profit"`
	RefundableValue hexutil.Big      `json:"refundableValue"`
	GasUsed         hexutil.Uint64   `json:"gasUsed"`
	BodyLogs        []SimMevBodyLogs `json:"logs,omitempty"`
}

type SimMevBundleAuxArgs struct {
	ParentBlock *rpc.BlockNumberOrHash `json:"parentBlock"`
	// override the default values for the block header
	BlockNumber *hexutil.Big    `json:"blockNumber"`
	Coinbase    *common.Address `json:"coinbase"`
	Timestamp   *hexutil.Uint64 `json:"timestamp"`
	GasLimit    *hexutil.Uint64 `json:"gasLimit"`
	BaseFee     *hexutil.Big    `json:"baseFee"`
	Timeout     *int64          `json:"timeout"`
}

type SimMevBodyLogs struct {
	TxLogs     []*types.Log     `json:"txLogs,omitempty"`
	BundleLogs []SimMevBodyLogs `json:"bundleLogs,omitempty"`
}

func GetRefundConfigV1FromBody(args *MevBundleBody) ([]RefundConfig, error) {
	if args.Tx != nil {
		var tx types.Transaction
		err := tx.UnmarshalBinary(*args.Tx)
		if err != nil {
			return nil, err
		}
		signer := types.LatestSignerForChainID(tx.ChainId())
		from, err := types.Sender(signer, &tx)
		if err != nil {
			return nil, err
		}
		return []RefundConfig{{Address: from, Percent: 100}}, nil
	} else if args.Bundle != nil {
		if len(args.Bundle.Validity.RefundConfig) > 0 {
			return args.Bundle.Validity.RefundConfig, nil
		} else {
			if len(args.Bundle.Body) == 0 {
				return nil, ErrInvalidBundleBodySize
			}
			return GetRefundConfigV1FromBody(&args.Bundle.Body[0])
		}
	} else {
		return nil, ErrInvalidBundleBody
	}
}
