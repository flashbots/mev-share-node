package mevshare

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/flashbots/go-utils/cli"
	"github.com/stretchr/testify/require"
)

var testPostgresDSN = cli.GetEnv("TEST_POSTGRES_DSN", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable")

func TestDBBackend_GetBundle(t *testing.T) {
	b, err := NewDBBackend(testPostgresDSN)
	require.NoError(t, err)
	defer b.Close()

	hash := common.HexToHash("0x0102030405060708091011121314151617181920212223242526272829303132")

	// Delete bundle if it exists
	_, err = b.db.Exec("DELETE FROM sbundle WHERE hash = $1", hash.Bytes())
	require.NoError(t, err)

	// Get bundle that doesn't exist
	_, err = b.GetBundle(context.Background(), hash)
	require.ErrorIs(t, err, ErrBundleNotFound)

	// Insert a bundle, that allow matching
	_, err = b.db.Exec("INSERT INTO sbundle (hash, body, signer, body_size, allow_matching) VALUES ($1, $2, $3, $4, $5)",
		hash.Bytes(), []byte("{}"), []byte{1}, 1, true)
	require.NoError(t, err)
	_, err = b.GetBundle(context.Background(), hash)
	require.NoError(t, err)

	// update allow matching to false
	_, err = b.db.Exec("UPDATE sbundle SET allow_matching = false WHERE hash = $1", hash.Bytes())
	require.NoError(t, err)

	// Get bundle that exists, but doesn't allow matching
	_, err = b.GetBundle(context.Background(), hash)
	require.ErrorIs(t, err, ErrBundleNotFound)
}

func TestDBBackend_InsertBundleForStats(t *testing.T) {
	b, err := NewDBBackend(testPostgresDSN)
	require.NoError(t, err)
	defer b.Close()

	bundleHash := common.HexToHash("0x0102030405060708091011121314151617181920212223242526272829303132")
	// Delete bundle if it exists
	_, err = b.db.Exec("DELETE FROM sbundle WHERE hash = $1", bundleHash.Bytes())
	require.NoError(t, err)
	_, err = b.db.Exec("DELETE FROM sbundle_body WHERE hash = $1", bundleHash.Bytes())
	require.NoError(t, err)

	receivedAt := time.Now()
	tx := common.Hex2Bytes("0x0102030405060708091011121314151617181920212223242526272829303132")
	signer := common.HexToAddress("0x0102030405060708091011121314151617181920")

	bundle := SendMevBundleArgs{
		Version: "v0.1",
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    2,
		},
		Body: []MevBundleBody{{Tx: (*hexutil.Bytes)(&tx)}},
		Privacy: &MevBundlePrivacy{
			Hints:      HintHash,
			Builders:   nil,
			WantRefund: nil,
		},
		Validity: MevBundleValidity{},
		Metadata: &MevBundleMetadata{
			BundleHash: bundleHash,
			BodyHashes: []common.Hash{bundleHash},
			Signer:     signer,
			OriginID:   "test-origin",
			ReceivedAt: hexutil.Uint64(receivedAt.UnixMicro()),
		},
	}

	simResult1 := SimMevBundleResponse{
		Success:         true,
		Error:           "",
		StateBlock:      5,
		MevGasPrice:     hexutil.Big(*big.NewInt(5)),
		Profit:          hexutil.Big(*big.NewInt(10)),
		RefundableValue: hexutil.Big(*big.NewInt(7)),
		GasUsed:         1700,
		BodyLogs:        nil,
	}

	known, err := b.InsertBundleForStats(context.Background(), &bundle, &simResult1)
	require.NoError(t, err)
	require.False(t, known)

	var dbBundle DBSbundle
	err = b.db.Get(&dbBundle, "SELECT * FROM sbundle WHERE hash = $1", bundleHash.Bytes())
	require.NoError(t, err)

	require.Equal(t, bundleHash.Bytes(), dbBundle.Hash)
	require.Equal(t, signer.Bytes(), dbBundle.Signer)
	require.False(t, dbBundle.Cancelled)
	require.True(t, dbBundle.AllowMatching)
	require.Equal(t, receivedAt.UnixMicro(), dbBundle.ReceivedAt.UnixMicro())
	require.Equal(t, 1, dbBundle.BodySize)
	require.Equal(t, "test-origin", dbBundle.OriginID.String)
	// sim results
	require.Equal(t, true, dbBundle.SimSuccess)
	require.False(t, dbBundle.SimError.Valid)
	require.True(t, dbBundle.SimEffGasPrice.Valid)
	require.Equal(t, "0.000000000000000005", dbBundle.SimEffGasPrice.String)
	require.True(t, dbBundle.SimProfit.Valid)
	require.Equal(t, "0.000000000000000010", dbBundle.SimProfit.String)
	require.True(t, dbBundle.SimRefundableValue.Valid)
	require.Equal(t, "0.000000000000000007", dbBundle.SimRefundableValue.String)
	require.True(t, dbBundle.SimGasUsed.Valid)
	require.Equal(t, int64(1700), dbBundle.SimGasUsed.Int64)
	require.True(t, dbBundle.SimAllSimsGasUsed.Valid)
	require.Equal(t, int64(1700), dbBundle.SimAllSimsGasUsed.Int64)
	require.True(t, dbBundle.SimTotalSimCount.Valid)
	require.Equal(t, int64(1), dbBundle.SimTotalSimCount.Int64)

	simResult2 := SimMevBundleResponse{
		Success:         false,
		Error:           "error",
		StateBlock:      6,
		MevGasPrice:     hexutil.Big(*big.NewInt(0)),
		Profit:          hexutil.Big(*big.NewInt(0)),
		RefundableValue: hexutil.Big(*big.NewInt(0)),
		GasUsed:         700,
		BodyLogs:        nil,
	}

	known, err = b.InsertBundleForStats(context.Background(), &bundle, &simResult2)
	require.NoError(t, err)
	require.True(t, known)

	var dbBundle2 DBSbundle
	err = b.db.Get(&dbBundle2, "SELECT * FROM sbundle WHERE hash = $1", bundleHash.Bytes())
	require.NoError(t, err)

	// sim results
	require.Equal(t, false, dbBundle2.SimSuccess)
	require.True(t, dbBundle2.SimError.Valid)
	require.Equal(t, "error", dbBundle2.SimError.String)
	require.False(t, dbBundle2.SimEffGasPrice.Valid)
	require.False(t, dbBundle2.SimProfit.Valid)
	require.False(t, dbBundle2.SimRefundableValue.Valid)
	require.True(t, dbBundle2.SimGasUsed.Valid)
	require.Equal(t, int64(700), dbBundle2.SimGasUsed.Int64)
	require.True(t, dbBundle2.SimAllSimsGasUsed.Valid)
	require.Equal(t, int64(1700+700), dbBundle2.SimAllSimsGasUsed.Int64)
	require.True(t, dbBundle2.SimTotalSimCount.Valid)
	require.Equal(t, int64(2), dbBundle2.SimTotalSimCount.Int64)
}

func TestDBBackend_CancelBundleByHash(t *testing.T) {
	b, err := NewDBBackend(testPostgresDSN)
	require.NoError(t, err)
	defer b.Close()

	hash := common.HexToHash("0x0102030405060708091011121314151617181920212223242526272829303132")
	signer := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	signer2 := common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// Delete bundle if it exists
	_, err = b.db.Exec("DELETE FROM sbundle WHERE hash = $1", hash.Bytes())
	require.NoError(t, err)

	// Insert a bundle, with signer
	_, err = b.db.Exec("INSERT INTO sbundle (hash, body, signer, body_size, allow_matching) VALUES ($1, $2, $3, $4, $5)",
		hash.Bytes(), []byte("{}"), signer, 1, true)
	require.NoError(t, err)

	// try to cancel with wrong signer
	err = b.CancelBundleByHash(context.Background(), hash, signer2)
	require.ErrorIs(t, err, ErrBundleNotCancelled)

	var dbBundle DBSbundle
	err = b.db.Get(&dbBundle, "SELECT * FROM sbundle WHERE hash = $1", hash.Bytes())
	require.NoError(t, err)
	require.False(t, dbBundle.Cancelled)

	// cancel with correct signer
	err = b.CancelBundleByHash(context.Background(), hash, signer)
	require.NoError(t, err)

	err = b.db.Get(&dbBundle, "SELECT * FROM sbundle WHERE hash = $1", hash.Bytes())
	require.NoError(t, err)
	require.True(t, dbBundle.Cancelled)
}

func TestDBBackend_InsertBundleForBuilder(t *testing.T) {
	b, err := NewDBBackend(testPostgresDSN)
	require.NoError(t, err)
	defer b.Close()

	bundleHash := common.HexToHash("0x0102030405060708091011121314151617181920212223242526272829303132")
	// Delete bundle if it exists
	_, err = b.db.Exec("DELETE FROM sbundle_builder WHERE hash = $1", bundleHash.Bytes())
	require.NoError(t, err)

	receivedAt := time.Now()
	tx := common.Hex2Bytes("0x0102030405060708091011121314151617181920212223242526272829303132")
	signer := common.HexToAddress("0x0102030405060708091011121314151617181920")

	bundle := SendMevBundleArgs{
		Version: "v0.1",
		Inclusion: MevBundleInclusion{
			BlockNumber: 6,
			MaxBlock:    8,
		},
		Body: []MevBundleBody{{Tx: (*hexutil.Bytes)(&tx)}},
		Privacy: &MevBundlePrivacy{
			Hints:      HintHash,
			Builders:   nil,
			WantRefund: nil,
		},
		Validity: MevBundleValidity{},
		Metadata: &MevBundleMetadata{
			BundleHash: bundleHash,
			BodyHashes: []common.Hash{bundleHash},
			Signer:     signer,
			OriginID:   "test-origin",
			ReceivedAt: hexutil.Uint64(receivedAt.UnixMicro()),
		},
	}

	sim := SimMevBundleResponse{
		Success:         true,
		Error:           "",
		StateBlock:      5,
		MevGasPrice:     hexutil.Big(*big.NewInt(5)),
		Profit:          hexutil.Big(*big.NewInt(10)),
		RefundableValue: hexutil.Big(*big.NewInt(7)),
		GasUsed:         1700,
		BodyLogs:        nil,
	}

	err = b.InsertBundleForBuilder(context.Background(), &bundle, &sim, 6)
	require.NoError(t, err)

	var dbBundle DBSbundleBuilder
	err = b.db.Get(&dbBundle, "SELECT * FROM sbundle_builder WHERE hash = $1", bundleHash.Bytes())
	require.NoError(t, err)

	require.Equal(t, bundleHash.Bytes(), dbBundle.Hash)
	require.Equal(t, int64(6), dbBundle.Block)
	require.Equal(t, int64(6), dbBundle.MaxBlock)
	require.True(t, dbBundle.SimStateBlock.Valid)
	require.Equal(t, int64(5), dbBundle.SimStateBlock.Int64)
	require.True(t, dbBundle.SimEffGasPrice.Valid)
	require.Equal(t, "0.000000000000000005", dbBundle.SimEffGasPrice.String)
	require.True(t, dbBundle.SimProfit.Valid)
	require.Equal(t, "0.000000000000000010", dbBundle.SimProfit.String)

	sim = SimMevBundleResponse{
		Success:         true,
		Error:           "",
		StateBlock:      6,
		MevGasPrice:     hexutil.Big(*big.NewInt(6)),
		Profit:          hexutil.Big(*big.NewInt(11)),
		RefundableValue: hexutil.Big(*big.NewInt(8)),
		GasUsed:         1750,
		BodyLogs:        nil,
	}

	err = b.InsertBundleForBuilder(context.Background(), &bundle, &sim, 7)
	require.NoError(t, err)

	err = b.db.Get(&dbBundle, "SELECT * FROM sbundle_builder WHERE hash = $1", bundleHash.Bytes())
	require.NoError(t, err)

	require.Equal(t, bundleHash.Bytes(), dbBundle.Hash)
	require.Equal(t, int64(7), dbBundle.Block)
	require.Equal(t, int64(7), dbBundle.MaxBlock)
	require.True(t, dbBundle.SimStateBlock.Valid)
	require.Equal(t, int64(6), dbBundle.SimStateBlock.Int64)
	require.True(t, dbBundle.SimEffGasPrice.Valid)
	require.Equal(t, "0.000000000000000006", dbBundle.SimEffGasPrice.String)
	require.True(t, dbBundle.SimProfit.Valid)
	require.Equal(t, "0.000000000000000011", dbBundle.SimProfit.String)
}
