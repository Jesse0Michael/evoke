package oidc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAudienceAllowed(t *testing.T) {
	tests := []struct {
		name    string
		tokAud  []string
		allowed []string
		want    bool
	}{
		{name: "single match", tokAud: []string{"desktop-id"}, allowed: []string{"desktop-id"}, want: true},
		{name: "matches one of several allowed", tokAud: []string{"web-id"}, allowed: []string{"desktop-id", "web-id"}, want: true},
		{name: "no match", tokAud: []string{"other-id"}, allowed: []string{"desktop-id"}, want: false},
		{name: "empty allowed rejects", tokAud: []string{"desktop-id"}, allowed: nil, want: false},
		{name: "empty audience rejects", tokAud: nil, allowed: []string{"desktop-id"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, audienceAllowed(tt.tokAud, tt.allowed))
		})
	}
}
