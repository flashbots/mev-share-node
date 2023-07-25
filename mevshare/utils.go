package mevshare

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
)

var (
	ethDivisor  = new(big.Float).SetUint64(params.Ether)
	gweiDivisor = new(big.Float).SetUint64(params.GWei)

	big1  = big.NewInt(1)
	big10 = big.NewInt(10)

	ErrInvalidRefundConfig = errors.New("invalid refund config")
)

func formatUnits(value *big.Int, unit string) string {
	float := new(big.Float).SetInt(value)
	switch unit {
	case "eth":
		return float.Quo(float, ethDivisor).String()
	case "gwei":
		return float.Quo(float, gweiDivisor).String()
	default:
		return ""
	}
}

type EthCachingClient struct {
	ethClient   *ethclient.Client
	mu          sync.RWMutex
	blockNumber uint64
	lastUpdate  time.Time
}

func NewCachingEthClient(ethClient *ethclient.Client) *EthCachingClient {
	return &EthCachingClient{
		ethClient:   ethClient,
		mu:          sync.RWMutex{},
		blockNumber: 0,
		lastUpdate:  time.Now().Add(-10 * time.Second),
	}
}

// BlockNumber returns the most recent block number, cached for 5 seconds
func (c *EthCachingClient) BlockNumber(ctx context.Context) (uint64, error) {
	c.mu.RLock()
	if time.Since(c.lastUpdate) < 5*time.Second {
		c.mu.RUnlock()
		return c.blockNumber, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	blockNumber, err := c.ethClient.BlockNumber(ctx)
	if err != nil {
		return 0, err
	}

	c.blockNumber = blockNumber
	c.lastUpdate = time.Now()
	return blockNumber, nil
}

// Intersect returns the intersection of two string arrays, without duplicates
func Intersect(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return []string{}
	}
	m := make(map[string]bool)
	for _, v := range a {
		m[v] = true
	}
	ret := []string{}
	for _, v := range b {
		if m[v] {
			ret = append(ret, v)
		}
	}
	return ret
}

// RoundUpWithPrecision rounds number up leaving only precisionDigits non-zero
// examples:
// RoundUpWithPrecision(123456, 2) = 130000
// RoundUpWithPrecision(111, 2) = 120
// RoundUpWithPrecision(199, 2) = 120
func RoundUpWithPrecision(number *big.Int, precisionDigits int) *big.Int {
	// Calculate the number of digits in the input number
	numDigits := len(number.String())

	if numDigits <= precisionDigits {
		return new(big.Int).Set(number)
	}
	// Calculate the power of 16 for the number of zero digits
	power := big.NewInt(int64(numDigits - precisionDigits))
	power.Exp(big10, power, nil)

	// Divide the number by the power of 16 to remove the extra digits
	div := new(big.Int).Div(number, power)

	// If the original number is not a multiple of the power of 16, round up
	if new(big.Int).Mul(div, power).Cmp(number) != 0 {
		div.Add(div, big1)
	}

	// Multiply the result by the power of 16 to add the extra digits back
	result := div.Mul(div, power)

	return result
}

// ConvertTotalRefundConfigToBundleV1Params converts total refund config used in v0.2 to params used in mev_sendBundle v0.1
func ConvertTotalRefundConfigToBundleV1Params(totalRefundConfig []RefundConfig) (wantRefund int, refundConfig []RefundConfig, err error) {
	if len(totalRefundConfig) == 0 {
		return wantRefund, refundConfig, nil
	}

	totalRefund := 0
	for _, r := range totalRefundConfig {
		if r.Percent <= 0 || r.Percent >= 100 {
			return wantRefund, refundConfig, ErrInvalidRefundConfig
		}
		totalRefund += r.Percent
	}
	if totalRefund <= 0 || totalRefund >= 100 {
		return wantRefund, refundConfig, ErrInvalidRefundConfig
	}

	refundConfig = make([]RefundConfig, len(totalRefundConfig))
	copy(refundConfig, totalRefundConfig)

	// normalize refund config percentages
	for i := range refundConfig {
		refundConfig[i].Percent = (refundConfig[i].Percent * 100) / totalRefund
	}

	// should sum to 100
	totalRefundConfDelta := 0
	for _, r := range refundConfig {
		totalRefundConfDelta += r.Percent
	}
	totalRefundConfDelta = 100 - totalRefundConfDelta

	// try to remove delta
	for i, r := range refundConfig {
		if fixed := totalRefundConfDelta + r.Percent; fixed <= 100 && fixed >= 0 {
			refundConfig[i].Percent = fixed
			break
		}
	}
	return totalRefund, refundConfig, nil
}

func ConvertBundleV1ParamsToTotalRefundConfig(wantRefund int, refundConfig []RefundConfig) (totalRefundConfig []RefundConfig) {
	totalRefundConfig = make([]RefundConfig, len(refundConfig))
	for i := range refundConfig {
		totalRefundConfig[i].Address = refundConfig[i].Address
		totalRefundConfig[i].Percent = (refundConfig[i].Percent * wantRefund) / 100
	}
	return totalRefundConfig
}
