package mevshare

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestFilterSpecialLogs(t *testing.T) {
	tests := []struct {
		name  string
		input *types.Log
		want  *types.Log
	}{
		{
			name: "uni2/sushi",
			input: &types.Log{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Topics: []common.Hash{
					common.HexToHash("0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"),
					common.HexToHash("0x000000000000000000000000ef1c6e67703c7bd7107eed8303fbe6ec2554bf6b"),
					common.HexToHash("0x000000000000000000000000b24db79c7cdb9d957fd943e48fec38047623c7b7"),
				},
				Data: []byte("anydata"),
			},
			want: &types.Log{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Topics: []common.Hash{
					common.HexToHash("0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"),
					common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
					common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
				},
				Data: []byte{},
			},
		},
		{
			name: "uni3",
			input: &types.Log{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Topics: []common.Hash{
					common.HexToHash("0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"),
					common.HexToHash("0x000000000000000000000000ef1c6e67703c7bd7107eed8303fbe6ec2554bf6b"),
					common.HexToHash("0x00000000000000000000000009ff98337ffb7893d2ab645666efe52d9f2c935c"),
				},
				Data: []byte("anydata"),
			},
			want: &types.Log{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Topics: []common.Hash{
					common.HexToHash("0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"),
					common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
					common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
				},
				Data: []byte{},
			},
		},
		{
			name: "curve",
			input: &types.Log{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Topics: []common.Hash{
					common.HexToHash("0x8b3e96f2b889fa771c53c981b40daf005f63f637f1869f707052d15a3dd97140"),
					common.HexToHash("0x0000000000000000000000005c7e53630df9323825e98cd6fac3f90f57ac7d68"),
				},
				Data: []byte("anydata"),
			},
			want: &types.Log{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Topics: []common.Hash{
					common.HexToHash("0x8b3e96f2b889fa771c53c981b40daf005f63f637f1869f707052d15a3dd97140"),
					common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
				},
				Data: []byte{},
			},
		},
		{
			name: "balancer",
			input: &types.Log{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Topics: []common.Hash{
					common.HexToHash("0x2170c741c41531aec20e7c107c24eecfdd15e69c9bb0a8dd37b1840b9e0b207b"),
					common.HexToHash("0x831261f44931b7da8ba0dcc547223c60bb75b47f000200000000000000000460"),
					common.HexToHash("0x000000000000000000000000a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"),
					common.HexToHash("0x000000000000000000000000d5a14081a34d256711b02bbef17e567da48e80b5"),
				},
				Data: []byte("anydata"),
			},
			want: &types.Log{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Topics: []common.Hash{
					common.HexToHash("0x2170c741c41531aec20e7c107c24eecfdd15e69c9bb0a8dd37b1840b9e0b207b"),
					common.HexToHash("0x831261f44931b7da8ba0dcc547223c60bb75b47f000200000000000000000460"),
					common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
					common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
				},
				Data: []byte{},
			},
		},
		{
			name: "random event",
			input: &types.Log{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Topics: []common.Hash{
					common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
				},
				Data: []byte("anydata"),
			},
			want: nil,
		},
		{
			name: "broken balancer event",
			input: &types.Log{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Topics: []common.Hash{
					common.HexToHash("0x2170c741c41531aec20e7c107c24eecfdd15e69c9bb0a8dd37b1840b9e0b207b"),
				},
				Data: []byte("anydata"),
			},
			want: nil,
		},
		{
			name: "broken uni3 event",
			input: &types.Log{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Topics: []common.Hash{
					common.HexToHash("0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"),
				},
				Data: []byte("anydata"),
			},
			want: &types.Log{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Topics: []common.Hash{
					common.HexToHash("0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"),
				},
				Data: []byte{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filterSpecialLogs([]*types.Log{tt.input})
			if tt.want == nil {
				require.Empty(t, out)
			} else {
				require.Len(t, out, 1)
				require.Equal(t, tt.want, out[0])
			}
		})
	}
}
