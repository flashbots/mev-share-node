package mevshare

import (
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var (
	ErrInvalidHintIntent  = errors.New("invalid hint intent")
	ErrNilBundleMetadata  = errors.New("bundle metadata is nil")
	ErrInvalidUnionBundle = errors.New("invalid union bundle")
)

const (
	VersionV1 = "v0.1"
	VersionV2 = "v0.2"
)

// HintIntent is a set of hint intents
// its marshalled as an array of strings
type HintIntent uint8

const (
	HintContractAddress HintIntent = 1 << iota
	HintFunctionSelector
	HintLogs
	HintCallData
	HintHash
	HintSpecialLogs
	HintTxHash
	HintsAll = HintContractAddress | HintFunctionSelector | HintLogs | HintCallData | HintHash | HintSpecialLogs | HintTxHash
	HintNone = 0
)

func (b *HintIntent) SetHint(flag HintIntent) {
	*b = *b | flag
}

func (b *HintIntent) HasHint(flag HintIntent) bool {
	return *b&flag != 0
}

func (b HintIntent) MarshalJSON() ([]byte, error) {
	var arr []string
	if b.HasHint(HintContractAddress) {
		arr = append(arr, "contract_address")
	}
	if b.HasHint(HintFunctionSelector) {
		arr = append(arr, "function_selector")
	}
	if b.HasHint(HintLogs) {
		arr = append(arr, "logs")
	}
	if b.HasHint(HintCallData) {
		arr = append(arr, "calldata")
	}
	if b.HasHint(HintHash) {
		arr = append(arr, "hash")
	}
	if b.HasHint(HintSpecialLogs) {
		arr = append(arr, "special_logs")
	}
	if b.HasHint(HintTxHash) {
		arr = append(arr, "tx_hash")
	}
	return json.Marshal(arr)
}

func (b *HintIntent) UnmarshalJSON(data []byte) error {
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	for _, v := range arr {
		switch v {
		case "contract_address":
			b.SetHint(HintContractAddress)
		case "function_selector":
			b.SetHint(HintFunctionSelector)
		case "logs":
			b.SetHint(HintLogs)
		case "calldata":
			b.SetHint(HintCallData)
		case "hash":
			b.SetHint(HintHash)
		case "special_logs":
			b.SetHint(HintSpecialLogs)
		case "tx_hash":
			b.SetHint(HintTxHash)
		default:
			return ErrInvalidHintIntent
		}
	}
	return nil
}

type Hint struct {
	Hash        common.Hash     `json:"hash"`
	Logs        []CleanLog      `json:"logs"`
	Txs         []TxHint        `json:"txs"`
	MevGasPrice *hexutil.Big    `json:"mevGasPrice,omitempty"`
	GasUsed     *hexutil.Uint64 `json:"gasUsed,omitempty"`
}

type TxHint struct {
	Hash             *common.Hash    `json:"hash,omitempty"`
	To               *common.Address `json:"to,omitempty"`
	FunctionSelector *hexutil.Bytes  `json:"functionSelector,omitempty"`
	CallData         *hexutil.Bytes  `json:"callData,omitempty"`
}

type CleanLog struct {
	// address of the contract that generated the event
	Address common.Address `json:"address"`
	// list of topics provided by the contract.
	Topics []common.Hash `json:"topics"`
	// supplied by the contract, usually ABI-encoded
	Data hexutil.Bytes `json:"data"`
}

type SendRefundRecBundleArgs struct {
	BlockNumber       hexutil.Uint64  `json:"blockNumber"`
	Txs               []hexutil.Bytes `json:"txs"`
	RevertingTxHashes []common.Hash   `json:"revertingTxHashes,omitempty"`
	RefundPercent     *int            `json:"refundPercent,omitempty"`
	RefundRecipient   *common.Address `json:"refundRecipient,omitempty"`
}

type SendBundleArgsVersioned struct {
	Version string `json:"version"`
}

type SendBundleUnion struct {
	v1bundle *SendMevBundleArgsV1
	v2bundle *SendMevBundleArgsV2
}

func (p *SendBundleUnion) UnmarshalJSON(data []byte) error {
	var versioned SendBundleArgsVersioned
	if err := json.Unmarshal(data, &versioned); err != nil {
		return err
	}

	// if version is v0.1 or beta-1 use V1, if v0.2 use V2
	switch versioned.Version {
	case VersionV1, "beta-1":
		var bundle SendMevBundleArgsV1
		if err := json.Unmarshal(data, &bundle); err != nil {
			return err
		}
		p.v1bundle = &bundle
	case VersionV2:
		var bundle SendMevBundleArgsV2
		if err := json.Unmarshal(data, &bundle); err != nil {
			return err
		}
		p.v2bundle = &bundle
	}
	return nil
}

func (p *SendBundleUnion) MarshalJSON() ([]byte, error) {
	if p.v1bundle != nil {
		return json.Marshal(p.v1bundle)
	}
	if p.v2bundle != nil {
		return json.Marshal(p.v2bundle)
	}
	return nil, ErrInvalidUnionBundle
}
