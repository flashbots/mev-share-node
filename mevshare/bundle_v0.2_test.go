package mevshare

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

var (
	hash = common.HexToHash("0xaabbcc")
	// two transactions signed by addr1
	tx1   = common.Hex2Bytes("02f8700118843b9aca00850ec14a8d608255f094324a74db217d5c12add31c5f3545f45c57da7dd080850102030405c001a0ba6c9d75a7dba4eb26879d465a8a7505c0caa3fa6a50c763cb511b57d06ae996a047a84667d4a0fef94f08372538cfedc96e0d027caf46914b3c436337d3a0e82c")
	tx2   = common.Hex2Bytes("02f870010784059a5381851599296b148255f094324a74db217d5c12add31c5f3545f45c57da7dd080850102030415c001a0ef4027807cbf414334458cee0b6dc2b4741b381ca01da91f2759473cd9c05c41a040a947ab6273e390dc0a12bae6e7d2cf51cebf1cc7ed8f1d7b1365cd0b9fb219")
	addr1 = common.HexToAddress("0x324A74DB217D5c12aDd31c5f3545F45C57DA7dd0")
	addr2 = common.HexToAddress("0x222")
)

func TestSendMevBundleArgsV2_ToV1Bundle(t *testing.T) {
	iptr := func(i int) *int { return &i }

	input := SendMevBundleArgsV2{
		Version:    VersionV2,
		Extensions: nil,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBodyV2{
			{
				Hash: &hash,
			},
			{
				Hash:     &hash,
				Optional: true,
			},
			{
				Tx: (*hexutil.Bytes)(&tx1),
			},
			{
				Tx:       (*hexutil.Bytes)(&tx1),
				Optional: true,
			},
		},
		Privacy: &MevBundlePrivacyV2{
			Hints:    HintsAll,
			Builders: []string{"builder1", "builder2"},
		},
		Validity: &MevBundleValidityV2{
			Refund: []RefundConfig{
				{
					Address: addr1,
					Percent: 10,
				},
				{
					Address: addr2,
					Percent: 20,
				},
			},
		},
	}

	expectedOutput := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBody{
			{Hash: &hash},
			{Hash: &hash, CanRevert: true},
			{Tx: (*hexutil.Bytes)(&tx1)},
			{Tx: (*hexutil.Bytes)(&tx1), CanRevert: true},
		},
		Validity: MevBundleValidity{
			Refund: nil,
			RefundConfig: []RefundConfig{
				{
					Address: addr1,
					Percent: 34,
				},
				{
					Address: addr2,
					Percent: 66,
				},
			},
		},
		Privacy: &MevBundlePrivacy{
			Hints:      HintsAll,
			Builders:   []string{"builder1", "builder2"},
			WantRefund: iptr(30),
		},
		Metadata: nil,
	}

	result, err := input.ToV1Bundle()
	require.NoError(t, err)
	require.Equal(t, expectedOutput, *result)
}

func TestConvertV1BundleToMergedBundleV2_WithInnerBundleRefundConfig(t *testing.T) {
	innerbundle := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBody{
			{Tx: (*hexutil.Bytes)(&tx1)},
		},
		Validity: MevBundleValidity{
			Refund: nil,
			RefundConfig: []RefundConfig{
				{Address: addr1, Percent: 34},
				{Address: addr2, Percent: 66},
			},
		},
		Privacy:  nil,
		Metadata: nil,
	}

	input := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBody{
			{Bundle: &innerbundle},
			{Tx: (*hexutil.Bytes)(&tx2)},
		},
		Validity: MevBundleValidity{
			Refund:       []RefundConstraint{{BodyIdx: 0, Percent: 30}},
			RefundConfig: nil,
		},
		Privacy:  nil,
		Metadata: nil,
	}

	expectedOutput := SendMergedBundleArgsV2{
		Version:    VersionV2,
		Extensions: nil,
		Inclusion: MergedBundleInclusionV2{
			BlockNumber: 1,
		},
		Body: []MergedBundleBodyV2{
			{
				Items: []MergedBundleBodyItemV2{{Tx: hexutil.Bytes(tx1)}},
			},
			{
				Items: []MergedBundleBodyItemV2{{Tx: hexutil.Bytes(tx2)}},
				Validity: &MergedBundleValidityV2{
					Refund: []RefundConfig{
						{Address: addr1, Percent: 10},
						{Address: addr2, Percent: 19},
					},
				},
			},
		},
	}

	result, err := ConvertV1BundleToMergedBundleV2(&input)
	require.NoError(t, err)
	require.Equal(t, expectedOutput, *result)
}

func TestConvertV1BundleToMergedBundleV2_WithInnerBundleNoRefundConfig(t *testing.T) {
	innerbundle := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBody{
			{Tx: (*hexutil.Bytes)(&tx1)},
		},
		Validity: MevBundleValidity{
			Refund:       nil,
			RefundConfig: nil,
		},
		Privacy:  nil,
		Metadata: nil,
	}

	input := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBody{
			{Bundle: &innerbundle},
			{Tx: (*hexutil.Bytes)(&tx2)},
		},
		Validity: MevBundleValidity{
			Refund:       []RefundConstraint{{BodyIdx: 0, Percent: 30}},
			RefundConfig: nil,
		},
		Privacy:  nil,
		Metadata: nil,
	}

	expectedOutput := SendMergedBundleArgsV2{
		Version:    VersionV2,
		Extensions: nil,
		Inclusion: MergedBundleInclusionV2{
			BlockNumber: 1,
		},
		Body: []MergedBundleBodyV2{
			{
				Items: []MergedBundleBodyItemV2{{Tx: hexutil.Bytes(tx1)}},
			},
			{
				Items: []MergedBundleBodyItemV2{{Tx: hexutil.Bytes(tx2)}},
				Validity: &MergedBundleValidityV2{
					Refund: []RefundConfig{
						{Address: addr1, Percent: 30},
					},
				},
			},
		},
	}

	result, err := ConvertV1BundleToMergedBundleV2(&input)
	require.NoError(t, err)
	require.Equal(t, expectedOutput, *result)
}

func TestConvertV1BundleToMergedBundleV2_RefundToTx(t *testing.T) { //nolint:dupl
	input := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBody{
			{Tx: (*hexutil.Bytes)(&tx1)},
			{Tx: (*hexutil.Bytes)(&tx2)},
		},
		Validity: MevBundleValidity{
			Refund:       []RefundConstraint{{BodyIdx: 0, Percent: 30}},
			RefundConfig: nil,
		},
		Privacy:  nil,
		Metadata: nil,
	}

	expectedOutput := SendMergedBundleArgsV2{
		Version:    VersionV2,
		Extensions: nil,
		Inclusion: MergedBundleInclusionV2{
			BlockNumber: 1,
		},
		Body: []MergedBundleBodyV2{
			{
				Items: []MergedBundleBodyItemV2{{Tx: hexutil.Bytes(tx1)}},
			},
			{
				Items: []MergedBundleBodyItemV2{{Tx: hexutil.Bytes(tx2)}},
				Validity: &MergedBundleValidityV2{
					Refund: []RefundConfig{
						{Address: addr1, Percent: 30},
					},
				},
			},
		},
	}

	result, err := ConvertV1BundleToMergedBundleV2(&input)
	require.NoError(t, err)
	require.Equal(t, expectedOutput, *result)
}

func TestConvertV1BundleToMergedBundleV2_AllTxsBundle(t *testing.T) {
	input := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBody{
			{Tx: (*hexutil.Bytes)(&tx1)},
			{Tx: (*hexutil.Bytes)(&tx2), CanRevert: true},
			{Tx: (*hexutil.Bytes)(&tx1)},
		},
		Validity: MevBundleValidity{
			Refund:       nil,
			RefundConfig: nil,
		},
		Privacy:  nil,
		Metadata: nil,
	}

	expectedOutput := SendMergedBundleArgsV2{
		Version:    VersionV2,
		Extensions: nil,
		Inclusion: MergedBundleInclusionV2{
			BlockNumber: 1,
		},
		Body: []MergedBundleBodyV2{
			{
				Items: []MergedBundleBodyItemV2{
					{Tx: hexutil.Bytes(tx1)},
					{Tx: hexutil.Bytes(tx2), Optional: true},
					{Tx: hexutil.Bytes(tx1)},
				},
			},
		},
	}

	result, err := ConvertV1BundleToMergedBundleV2(&input)
	require.NoError(t, err)
	require.Equal(t, expectedOutput, *result)
}

func TestConvertV1BundleToMergedBundleV2_BundleWithRefundToTx(t *testing.T) { //nolint:dupl
	input := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBody{
			{Tx: (*hexutil.Bytes)(&tx1)},
			{Tx: (*hexutil.Bytes)(&tx2)},
		},
		Validity: MevBundleValidity{
			Refund:       []RefundConstraint{{BodyIdx: 0, Percent: 30}},
			RefundConfig: nil,
		},
		Privacy:  nil,
		Metadata: nil,
	}

	expectedOutput := SendMergedBundleArgsV2{
		Version:    VersionV2,
		Extensions: nil,
		Inclusion: MergedBundleInclusionV2{
			BlockNumber: 1,
		},
		Body: []MergedBundleBodyV2{
			{
				Items: []MergedBundleBodyItemV2{
					{Tx: hexutil.Bytes(tx1)},
				},
			},
			{
				Items: []MergedBundleBodyItemV2{
					{Tx: hexutil.Bytes(tx2)},
				},
				Validity: &MergedBundleValidityV2{
					Refund: []RefundConfig{
						{Address: addr1, Percent: 30},
					},
				},
			},
		},
	}

	result, err := ConvertV1BundleToMergedBundleV2(&input)
	require.NoError(t, err)
	require.Equal(t, expectedOutput, *result)
}

func TestConvertV1BundleToMergedBundleV2_BundleWithRefundToInnerBundleWithRefundConfig(t *testing.T) {
	inner := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBody{
			{Tx: (*hexutil.Bytes)(&tx1)},
			{Tx: (*hexutil.Bytes)(&tx2), CanRevert: true},
		},
		Validity: MevBundleValidity{
			Refund: nil,
			RefundConfig: []RefundConfig{
				{Address: addr1, Percent: 34},
				{Address: addr2, Percent: 66},
			},
		},
		Privacy:  nil,
		Metadata: nil,
	}
	input := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBody{
			{Bundle: &inner, CanRevert: true},
			{Tx: (*hexutil.Bytes)(&tx2)},
		},
		Validity: MevBundleValidity{
			Refund:       []RefundConstraint{{BodyIdx: 0, Percent: 30}},
			RefundConfig: nil,
		},
		Privacy:  nil,
		Metadata: nil,
	}

	expectedOutput := SendMergedBundleArgsV2{
		Version:    VersionV2,
		Extensions: nil,
		Inclusion: MergedBundleInclusionV2{
			BlockNumber: 1,
		},
		Body: []MergedBundleBodyV2{
			{
				Items: []MergedBundleBodyItemV2{
					{Tx: hexutil.Bytes(tx1)},
					{Tx: hexutil.Bytes(tx2), Optional: true},
				},
				Optional: true,
			},
			{
				Items: []MergedBundleBodyItemV2{
					{Tx: hexutil.Bytes(tx2)},
				},
				Validity: &MergedBundleValidityV2{
					Refund: []RefundConfig{
						{Address: addr1, Percent: 10},
						{Address: addr2, Percent: 19},
					},
				},
			},
		},
	}

	result, err := ConvertV1BundleToMergedBundleV2(&input)
	require.NoError(t, err)
	require.Equal(t, expectedOutput, *result)
}

func TestConvertV1BundleToMergedBundleV2_BundleWithRefundToInnerBundleWithoutRefundConfig(t *testing.T) {
	inner := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBody{
			{Tx: (*hexutil.Bytes)(&tx1)},
			{Tx: (*hexutil.Bytes)(&tx2), CanRevert: true},
		},
		Validity: MevBundleValidity{
			Refund:       nil,
			RefundConfig: nil,
		},
		Privacy:  nil,
		Metadata: nil,
	}
	input := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    3,
		},
		Body: []MevBundleBody{
			{Bundle: &inner, CanRevert: true},
			{Tx: (*hexutil.Bytes)(&tx2)},
		},
		Validity: MevBundleValidity{
			Refund:       []RefundConstraint{{BodyIdx: 0, Percent: 30}},
			RefundConfig: nil,
		},
		Privacy:  nil,
		Metadata: nil,
	}

	expectedOutput := SendMergedBundleArgsV2{
		Version:    VersionV2,
		Extensions: nil,
		Inclusion: MergedBundleInclusionV2{
			BlockNumber: 1,
		},
		Body: []MergedBundleBodyV2{
			{
				Items: []MergedBundleBodyItemV2{
					{Tx: hexutil.Bytes(tx1)},
					{Tx: hexutil.Bytes(tx2), Optional: true},
				},
				Optional: true,
			},
			{
				Items: []MergedBundleBodyItemV2{
					{Tx: hexutil.Bytes(tx2)},
				},
				Validity: &MergedBundleValidityV2{
					Refund: []RefundConfig{
						{Address: addr1, Percent: 30},
					},
				},
			},
		},
	}

	result, err := ConvertV1BundleToMergedBundleV2(&input)
	require.NoError(t, err)
	require.Equal(t, expectedOutput, *result)
}

func TestSendMergedBundleArgsV2_ToV1Bundle_AllTxOneBundle(t *testing.T) {
	input := SendMergedBundleArgsV2{
		Version:    VersionV2,
		Extensions: nil,
		Inclusion: MergedBundleInclusionV2{
			BlockNumber: 1,
		},
		Body: []MergedBundleBodyV2{
			{
				Items: []MergedBundleBodyItemV2{
					{Tx: hexutil.Bytes(tx1)},
					{Tx: hexutil.Bytes(tx2), Optional: true},
				},
			},
		},
	}

	expectedOutput := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    0,
		},
		Body: []MevBundleBody{
			{Tx: (*hexutil.Bytes)(&tx1)},
			{Tx: (*hexutil.Bytes)(&tx2), CanRevert: true},
		},
		Validity: MevBundleValidity{
			Refund:       nil,
			RefundConfig: nil,
		},
		Privacy:  nil,
		Metadata: nil,
	}

	result, err := input.ToV1Bundle()
	require.NoError(t, err)
	require.Equal(t, expectedOutput, *result)
}

func TestSendMergedBundleArgsV2_ToV1Bundle_WithRefund(t *testing.T) {
	input := SendMergedBundleArgsV2{
		Version:    VersionV2,
		Extensions: nil,
		Inclusion: MergedBundleInclusionV2{
			BlockNumber: 1,
		},
		Body: []MergedBundleBodyV2{
			{
				Items: []MergedBundleBodyItemV2{
					{Tx: hexutil.Bytes(tx1)},
				},
			},
			{
				Items: []MergedBundleBodyItemV2{
					{Tx: hexutil.Bytes(tx2)},
				},
				Validity: &MergedBundleValidityV2{
					Refund: []RefundConfig{
						{Address: addr1, Percent: 10},
						{Address: addr2, Percent: 20},
					},
				},
			},
		},
	}

	innerBundle := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    0,
		},
		Body: []MevBundleBody{
			{Tx: (*hexutil.Bytes)(&tx1)},
		},
		Validity: MevBundleValidity{
			Refund: nil,
			RefundConfig: []RefundConfig{
				{Address: addr1, Percent: 34},
				{Address: addr2, Percent: 66},
			},
		},
		Privacy:  nil,
		Metadata: nil,
	}

	expectedOutput := SendMevBundleArgsV1{
		Version: VersionV1,
		Inclusion: MevBundleInclusion{
			BlockNumber: 1,
			MaxBlock:    0,
		},
		Body: []MevBundleBody{
			{Bundle: &innerBundle},
			{Tx: (*hexutil.Bytes)(&tx2)},
		},
		Validity: MevBundleValidity{
			Refund:       []RefundConstraint{{BodyIdx: 0, Percent: 30}},
			RefundConfig: nil,
		},
		Privacy:  nil,
		Metadata: nil,
	}

	result, err := input.ToV1Bundle()
	require.NoError(t, err)
	require.Equal(t, expectedOutput, *result)
}
