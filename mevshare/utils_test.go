package mevshare

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

func TestIntersect(t *testing.T) {
	type args struct {
		a []string
		b []string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "no intersection",
			args: args{
				a: []string{"a", "b", "c"},
				b: []string{"d", "e", "f"},
			},
			want: []string{},
		},
		{
			name: "intersection",
			args: args{
				a: []string{"a", "B", "c"},
				b: []string{"B", "C", "d"},
			},
			want: []string{"b", "c"},
		},
		{
			name: "empty",
			args: args{
				a: []string{"a", "b", "c"},
				b: []string{},
			},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Intersect(tt.args.a, tt.args.b))
		})
	}
}

func TestRoundUpWithPrecision(t *testing.T) {
	type args struct {
		number          *big.Int
		precisionDigits int
	}
	tests := []struct {
		name string
		args args
		want *big.Int
	}{
		{
			name: "round up 1",
			args: args{
				number:          big.NewInt(121000000),
				precisionDigits: 2,
			},
			want: big.NewInt(130000000),
		},
		{
			name: "round up 9",
			args: args{
				number:          big.NewInt(129000000),
				precisionDigits: 2,
			},
			want: big.NewInt(130000000),
		},
		{
			name: "round small",
			args: args{
				number:          big.NewInt(120000001),
				precisionDigits: 2,
			},
			want: big.NewInt(130000000),
		},
		{
			name: "no rounding neede",
			args: args{
				number:          big.NewInt(120000),
				precisionDigits: 2,
			},
			want: big.NewInt(120000),
		},
		{
			name: "exact digits",
			args: args{
				number:          big.NewInt(12),
				precisionDigits: 2,
			},
			want: big.NewInt(12),
		},
		{
			name: "small number",
			args: args{
				number:          big.NewInt(12),
				precisionDigits: 3,
			},
			want: big.NewInt(12),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RoundUpWithPrecision(tt.args.number, tt.args.precisionDigits)
			require.Equal(t, tt.want, got, "RoundUpWithPrecision() = %v, want %v", hexutil.EncodeBig(got), hexutil.EncodeBig(tt.want))
		})
	}
}
