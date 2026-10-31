package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitSpokenText(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantInputs []string
		wantText   string
	}{
		{
			name:       "a selector composes and a quoted line speaks",
			args:       []string{"sumi", "Sit down, we have a lot to talk about."},
			wantInputs: []string{"sumi"},
			wantText:   "Sit down, we have a lot to talk about.",
		},
		{
			name:       "multiple literals join in order",
			args:       []string{"sumi", "I didn't think you'd come back.", "Sit down."},
			wantInputs: []string{"sumi"},
			wantText:   "I didn't think you'd come back. Sit down.",
		},
		{
			name:       "a literal before the selector still composes correctly",
			args:       []string{"Sit down.", "sumi", "gem+outfit"},
			wantInputs: []string{"sumi", "gem+outfit"},
			wantText:   "Sit down.",
		},
		{
			name:       "paths and registry refs compose rather than speak",
			args:       []string{"./sumi.evoke", "@jesse/ashley", "Say something."},
			wantInputs: []string{"./sumi.evoke", "@jesse/ashley"},
			wantText:   "Say something.",
		},
		{
			name:       "no literal leaves nothing to speak",
			args:       []string{"sumi"},
			wantInputs: []string{"sumi"},
			wantText:   "",
		},
		{
			name:       "a line with no files still speaks",
			args:       []string{"Just this line."},
			wantInputs: nil,
			wantText:   "Just this line.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs, text := splitSpokenText(tt.args)

			require.Equal(t, tt.wantInputs, inputs)
			require.Equal(t, tt.wantText, text)
		})
	}
}
