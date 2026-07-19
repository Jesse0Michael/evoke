package validate_test

import (
	"testing"

	"github.com/jesse0michael/evoke/internal/evoke/parser"
	"github.com/jesse0michael/evoke/internal/evoke/validate"
	"github.com/stretchr/testify/require"
)

func TestDocument(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantError bool
	}{
		{
			name:      "known declarations are valid",
			src:       "NAME\n    Sumi\n\nPERSONALITY\n    warm\n",
			wantError: false,
		},
		{
			name:      "negative on a declaration that supports it",
			src:       "!APPEARANCE\n    tall\n",
			wantError: false,
		},
		{
			name:      "default on a declaration that supports it",
			src:       "?APPAREL\n    green shirt\n",
			wantError: false,
		},
		{
			name:      "default negative on a declaration that supports both",
			src:       "?!PROMPT\n    blurry\n",
			wantError: false,
		},
		{
			name:      "default on singular SCENARIO is valid",
			src:       "?SCENARIO\n    a quiet morning\n",
			wantError: false,
		},
		{
			name:      "unknown declaration is invalid",
			src:       "LOCATION\n    pine forest\n",
			wantError: true,
		},
		{
			name:      "negative on NAME is invalid",
			src:       "!NAME\n    Sumi\n",
			wantError: true,
		},
		{
			name:      "default on NAME is invalid",
			src:       "?NAME\n    Sumi\n",
			wantError: true,
		},
		{
			name:      "negative on IDENTITY is invalid",
			src:       "!IDENTITY\n    a nurse\n",
			wantError: true,
		},
		{
			name:      "negative on SCENARIO is invalid",
			src:       "!SCENARIO\n    a quiet morning\n",
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, parseErr := parser.Parse([]byte(tt.src))
			require.NoError(t, parseErr, "fixture should be syntactically valid")

			err := validate.Document(doc)

			require.Equal(t, tt.wantError, err != nil, "error mismatch: %v", err)
		})
	}
}
