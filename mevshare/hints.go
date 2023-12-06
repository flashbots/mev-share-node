package mevshare

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

var ErrCannotExtractHints = errors.New("cannot extract hints")

func extractTxHints(tx *types.Transaction, want HintIntent) (hint *TxHint) {
	// set sensible defaults
	// if function selector or calldata is set - set address
	if want.HasHint(HintFunctionSelector | HintCallData) {
		want.SetHint(HintContractAddress)
	}
	// if calldata is set - set function selector
	if want.HasHint(HintCallData) {
		want.SetHint(HintFunctionSelector)
	}

	if want.HasHint(HintContractAddress | HintFunctionSelector | HintCallData | HintTxHash) {
		hint = new(TxHint)
	}
	if want.HasHint(HintTxHash) {
		hash := tx.Hash()
		hint.Hash = &hash
	}
	if want.HasHint(HintContractAddress) {
		hint.To = tx.To()
	}
	if want.HasHint(HintFunctionSelector) {
		var s [4]byte
		copy(s[:], tx.Data())
		res := hexutil.Bytes(s[:])
		hint.FunctionSelector = &res
	}
	if want.HasHint(HintCallData) {
		res := hexutil.Bytes(tx.Data())
		hint.CallData = &res
	}
	return hint
}

func extractHintsInner(bundle *SendMevBundleArgs, bundleLogs []SimMevBodyLogs) (logs []CleanLog, txs []TxHint, err error) {
	var want HintIntent = HintNone
	if bundle.Privacy != nil {
		want = bundle.Privacy.Hints
	}
	if !want.HasHint(HintHash) {
		return logs, txs, nil
	}
	if len(bundleLogs) != len(bundle.Body) {
		return nil, nil, ErrCannotExtractHints
	}

	for i, el := range bundle.Body {
		if innerBundle := el.Bundle; innerBundle != nil {
			innerLogs, innerTxs, err := extractHintsInner(innerBundle, bundleLogs[i].BundleLogs)
			if err != nil {
				return nil, nil, err
			}
			logs = append(logs, innerLogs...)
			txs = append(txs, innerTxs...)
		} else if txBytes := el.Tx; txBytes != nil {
			if want.HasHint(HintLogs) {
				logs = append(logs, cleanLogs(bundleLogs[i].TxLogs)...)
			} else if want.HasHint(HintSpecialLogs) {
				logs = append(logs, cleanLogs(filterSpecialLogs(bundleLogs[i].TxLogs))...)
			}

			// skip if we don't need any of the hints for individual txs
			if !want.HasHint(HintContractAddress | HintFunctionSelector | HintCallData | HintTxHash) {
				continue
			}

			var tx types.Transaction
			err := tx.UnmarshalBinary(*txBytes)
			if err != nil {
				return nil, nil, err
			}

			txHint := extractTxHints(&tx, want)
			if txHint != nil {
				txs = append(txs, *txHint)
			}
		}
	}
	return logs, txs, err
}

func ExtractHints(bundle *SendMevBundleArgs, simRes *SimMevBundleResponse, shareGasUsed, shareMevGasPrice bool) (Hint, error) {
	var want HintIntent = HintNone
	if bundle.Privacy != nil {
		want = bundle.Privacy.Hints
	} else {
		return Hint{}, ErrCannotExtractHints
	}
	if !want.HasHint(HintHash) {
		return Hint{}, ErrCannotExtractHints
	}

	var hint Hint

	if shareMevGasPrice {
		hint.MevGasPrice = (*hexutil.Big)(RoundUpWithPrecision(simRes.MevGasPrice.ToInt(), HintGasPriceNumberPrecisionDigits))
	}
	if shareGasUsed {
		roundedGasUsed := RoundUpWithPrecision(big.NewInt(int64(simRes.GasUsed)), HintGasNumberPrecisionDigits).Uint64()
		hint.GasUsed = (*hexutil.Uint64)(&roundedGasUsed)
	}

	if want.HasHint(HintHash) {
		if bundle.Metadata != nil {
			hint.Hash = bundle.Metadata.MatchingHash
		}
	} else {
		return hint, ErrCannotExtractHints
	}

	logs, txs, err := extractHintsInner(bundle, simRes.BodyLogs)
	if err != nil {
		return hint, err
	}
	hint.Logs = logs
	hint.Txs = txs

	return hint, nil
}

func cleanLogs(logs []*types.Log) []CleanLog {
	res := make([]CleanLog, len(logs))
	for i, log := range logs {
		res[i] = CleanLog{
			Address: log.Address,
			Topics:  log.Topics,
			Data:    log.Data,
		}
	}
	return res
}

// Swap (index_topic_1 address sender, uint256 amount0In, uint256 amount1In, uint256 amount0Out, uint256 amount1Out, index_topic_2 address to)
var uni2log = common.HexToHash("0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822")

// Swap (index_topic_1 address sender, index_topic_2 address recipient, int256 amount0, int256 amount1, uint160 sqrtPriceX96, uint128 liquidity, int24 tick)
var uni3log = common.HexToHash("0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67")

// TokenExchange (index_topic_1 address buyer, int128 sold_id, uint256 tokens_sold, int128 bought_id, uint256 tokens_bought)
var curveLog = common.HexToHash("0x8b3e96f2b889fa771c53c981b40daf005f63f637f1869f707052d15a3dd97140")

// Swap (index_topic_1 bytes32 poolId, index_topic_2 address tokenIn, index_topic_3 address tokenOut, uint256 amountIn, uint256 amountOut)
var balancerLog = common.HexToHash("0x2170c741c41531aec20e7c107c24eecfdd15e69c9bb0a8dd37b1840b9e0b207b")

// filterSpecialLogs filters out logs for "special_logs" hint.
// We append this hint on our rpc endpoint for protect transactions.
//
// The only info that we want to leak is pool id and the fact that swap happened.
func filterSpecialLogs(logs []*types.Log) (res []*types.Log) {
	for _, log := range logs {
		if len(log.Topics) == 0 {
			continue
		}
		switch log.Topics[0] {
		// for these logs leaking signature and address is enough to identify pool where swap happened
		case uni2log, uni3log, curveLog:
			removeLogFields(log)
			res = append(res, log)
		// for balancer all swaps go through the same contract, so we also need to leak poolID (first indexed topic)
		case balancerLog:
			if len(log.Topics) < 2 {
				continue
			}
			poolID := log.Topics[1]
			removeLogFields(log)
			log.Topics[1] = poolID
			res = append(res, log)
		}
	}
	return res
}

func removeLogFields(log *types.Log) {
	// we leave the first topic because it is the event signature
	for i := 1; i < len(log.Topics); i++ {
		log.Topics[i] = common.Hash{}
	}
	// we zero out the log data
	log.Data = []byte{}
}
