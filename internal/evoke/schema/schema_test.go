package schema_test

import (
	"testing"

	"github.com/jesse0michael/evoke/internal/evoke/schema"
	"github.com/stretchr/testify/require"
)

func TestLookup(t *testing.T) {
	tests := []struct {
		name         string
		declaration  string
		wantFound    bool
		wantMerge    schema.MergeMode
		wantNegative bool
		wantDefault  bool
	}{
		{
			name:        "NAME is singular, positive only",
			declaration: "NAME",
			wantFound:   true,
			wantMerge:   schema.MergeSingular,
		},
		{
			name:         "PERSONALITY supports both channels",
			declaration:  "PERSONALITY",
			wantFound:    true,
			wantMerge:    schema.MergeAccumulating,
			wantNegative: true,
			wantDefault:  true,
		},
		{
			name:        "SCENARIO is singular with default but no negative",
			declaration: "SCENARIO",
			wantFound:   true,
			wantMerge:   schema.MergeSingular,
			wantDefault: true,
		},
		{
			name:        "IDENTITY accumulates, positive only",
			declaration: "IDENTITY",
			wantFound:   true,
			wantMerge:   schema.MergeAccumulating,
		},
		{
			name:        "unknown declaration is not found",
			declaration: "LOCATION",
			wantFound:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def, ok := schema.Lookup(tt.declaration)

			require.Equal(t, tt.wantFound, ok)
			if tt.wantFound {
				require.Equal(t, tt.declaration, def.Name)
				require.Equal(t, tt.wantMerge, def.Merge)
				require.Equal(t, tt.wantNegative, def.Negative)
				require.Equal(t, tt.wantDefault, def.Default)
			}
		})
	}
}

func TestAll(t *testing.T) {
	all := schema.All()

	require.Len(t, all, 9)

	// All declarations are returned in ascending render order.
	for i := 1; i < len(all); i++ {
		require.Greater(t, all[i].Order, all[i-1].Order, "declarations must be ordered by Order")
	}
}
