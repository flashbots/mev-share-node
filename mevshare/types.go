package mevshare

import (
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

var (
	ErrInvalidHintIntent = errors.New("invalid hint intent")
	ErrNilBundleMetadata = errors.New("bundle metadata is nil")
)

const (
	SendBundleEndpointName         = "mev_sendBundle"
	SimBundleEndpointName          = "mev_simBundle"
	CancelBundleByHashEndpointName = "mev_cancelBundleByHash"
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
		case "special_logs", "default_logs":
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

type SendMevBundleArgs struct {
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
	Hash      *common.Hash       `json:"hash,omitempty"`
	Tx        *hexutil.Bytes     `json:"tx,omitempty"`
	Bundle    *SendMevBundleArgs `json:"bundle,omitempty"`
	CanRevert bool               `json:"canRevert,omitempty"`
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
	BundleHash   common.Hash    `json:"bundleHash,omitempty"`
	BodyHashes   []common.Hash  `json:"bodyHashes,omitempty"`
	Signer       common.Address `json:"signer,omitempty"`
	OriginID     string         `json:"originId,omitempty"`
	ReceivedAt   hexutil.Uint64 `json:"receivedAt,omitempty"`
	MatchingHash common.Hash    `json:"matchingHash,omitempty"`
	Prematched   bool           `json:"prematched"`
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
