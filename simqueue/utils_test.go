package simqueue

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_packUnpackData(t *testing.T) {
	tests := []struct {
		name  string
		args  packArgs
		want  float64
		want1 []byte
	}{
		{
			name: "simple test",
			args: packArgs{
				data:           []byte("test"),
				minTargetBlock: 17,
				maxTargetBlock: 0x19,
				highPriority:   true,
				timestamp:      time.Unix(0, 0x21),
				iteration:      0x9,
			},
			want:  17.0,
			want1: []byte{0x0, 0x0, 0x9, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x21, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x19, 0x74, 0x65, 0x73, 0x74},
		},
		{
			name: "maxed out",
			args: packArgs{
				data:           []byte("data"),
				minTargetBlock: 0xaffffffff,
				maxTargetBlock: 0xafffffffffffffff,
				highPriority:   false,
				timestamp:      time.Unix(0, 0x99999999),
				iteration:      0xcfff,
			},
			want:  float64(0xaffffffff),
			want1: []byte{0x1, 0xcf, 0xff, 0x0, 0x0, 0x0, 0x0, 0x99, 0x99, 0x99, 0x99, 0xaf, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x64, 0x61, 0x74, 0x61},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := packData(tt.args)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.want1, got1)

			gotArgs, err := unpackData(got, got1)
			require.NoError(t, err)
			require.Equal(t, tt.args, gotArgs)
		})
	}
}
