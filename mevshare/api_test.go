package mevshare

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var signingKey, _ = crypto.HexToECDSA("f14240ad715b780803f613f636b05bacc2db6622c21eb48bf4302ec3e44c0acb")

func randomTx() *types.Transaction {
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       nil,
		Value:    big.NewInt(rand.Int63()), //nolint:gosec
		Data:     []byte{1, 2, 3, 4},
	})
	signer := types.NewLondonSigner(big.NewInt(1))
	tx, err := types.SignTx(tx, signer, signingKey)
	if err != nil {
		panic(err)
	}
	return tx
}

func BenchmarkTxValidation(b *testing.B) {
	signer := types.NewLondonSigner(big.NewInt(1))
	randTx := randomTx()
	txBytes, err := randTx.MarshalBinary()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var tx types.Transaction
		err = tx.UnmarshalBinary(txBytes)
		if err != nil {
			b.Fatal(err)
		}
		_ = tx.Hash()
		_, err = types.Sender(signer, &tx)
		if err != nil {
			b.Fatal(err)
		}
	}
}
