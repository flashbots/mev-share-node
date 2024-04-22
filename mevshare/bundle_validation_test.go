package mevshare

import (
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

func TestMergeInclusionIntervals(t *testing.T) {
	cases := []struct {
		name        string
		top         MevBundleInclusion
		bottom      MevBundleInclusion
		expectedTop MevBundleInclusion
		err         error
	}{
		{
			name: "same intervals",
			top: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(1),
				MaxBlock:    hexutil.Uint64(2),
			},
			bottom: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(1),
				MaxBlock:    hexutil.Uint64(2),
			},
			expectedTop: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(1),
				MaxBlock:    hexutil.Uint64(2),
			},
			err: nil,
		},
		{
			name: "overlap, top to the right",
			top: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(1),
				MaxBlock:    hexutil.Uint64(3),
			},
			bottom: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(2),
				MaxBlock:    hexutil.Uint64(4),
			},
			expectedTop: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(2),
				MaxBlock:    hexutil.Uint64(3),
			},
			err: nil,
		},
		{
			name: "overlap, top to the left",
			top: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(2),
				MaxBlock:    hexutil.Uint64(4),
			},
			bottom: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(1),
				MaxBlock:    hexutil.Uint64(3),
			},
			expectedTop: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(2),
				MaxBlock:    hexutil.Uint64(3),
			},
			err: nil,
		},
		{
			name: "overlap, bottom inside top",
			top: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(1),
				MaxBlock:    hexutil.Uint64(4),
			},
			bottom: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(2),
				MaxBlock:    hexutil.Uint64(3),
			},
			expectedTop: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(2),
				MaxBlock:    hexutil.Uint64(3),
			},
			err: nil,
		},
		{
			name: "overlap, top inside bottom",
			top: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(2),
				MaxBlock:    hexutil.Uint64(3),
			},
			bottom: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(1),
				MaxBlock:    hexutil.Uint64(4),
			},
			expectedTop: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(2),
				MaxBlock:    hexutil.Uint64(3),
			},
		},
		{
			name: "no overlap, top to the right",
			top: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(1),
				MaxBlock:    hexutil.Uint64(2),
			},
			bottom: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(3),
				MaxBlock:    hexutil.Uint64(4),
			},
			expectedTop: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(1),
				MaxBlock:    hexutil.Uint64(2),
			},
			err: ErrInvalidInclusion,
		},
		{
			name: "no overlap, top to the left",
			top: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(3),
				MaxBlock:    hexutil.Uint64(4),
			},
			bottom: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(1),
				MaxBlock:    hexutil.Uint64(2),
			},
			expectedTop: MevBundleInclusion{
				BlockNumber: hexutil.Uint64(3),
				MaxBlock:    hexutil.Uint64(4),
			},
			err: ErrInvalidInclusion,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bottomCopy := c.bottom
			topCopy := c.top
			err := MergeInclusionIntervals(&topCopy, bottomCopy)
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, c.expectedTop, topCopy)
			require.Equal(t, c.bottom, bottomCopy)
		})
	}
}
