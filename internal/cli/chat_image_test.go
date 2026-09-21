package cli

import (
	"path/filepath"
	"testing"

	"github.com/jesse0michael/evoke/internal/chat"
	"github.com/stretchr/testify/require"
)

// TestChatImage covers the /image command's setting: what a command line does
// to it, what the header reports, and the command line a generation composes.
func TestChatImage(t *testing.T) {
	tests := []struct {
		name       string
		lines      []string // /image command lines, applied in order
		wantStatus string   // reported by the last line applied
		wantFire   bool     // the last line should generate from the reply on screen
		wantLabel  string
		wantOn     bool
		wantBase   []string
	}{
		{
			name:      "off until set",
			wantLabel: "/image",
			wantBase:  []string{"image", "gem.evoke"},
		},
		{
			name:       "a bare command is off: there are no inputs left",
			lines:      []string{"/image"},
			wantStatus: "image generation off",
			wantLabel:  "/image",
			wantBase:   []string{"image", "gem.evoke"},
		},
		{
			name:       "the inputs come first on the command line, then the chat's files",
			lines:      []string{"/image ill mf"},
			wantStatus: "image generation on: ill mf",
			wantFire:   true,
			wantLabel:  "/image - ill mf",
			wantOn:     true,
			wantBase:   []string{"image", "ill", "mf", "gem.evoke"},
		},
		{
			name:       "a quoted input survives as one argument",
			lines:      []string{`/image ill "upper body"`},
			wantStatus: "image generation on: ill upper body",
			wantFire:   true,
			wantLabel:  "/image - ill upper body",
			wantOn:     true,
			wantBase:   []string{"image", "ill", "upper body", "gem.evoke"},
		},
		{
			name:       "clearing the inputs turns it off",
			lines:      []string{"/image ill", "/image"},
			wantStatus: "image generation off",
			wantLabel:  "/image",
			wantBase:   []string{"image", "gem.evoke"},
		},
		{
			name:       "new inputs replace the old ones rather than adding to them",
			lines:      []string{"/image ill mf", "/image sdxl"},
			wantStatus: "image generation on: sdxl",
			wantFire:   true,
			wantLabel:  "/image - sdxl",
			wantOn:     true,
			wantBase:   []string{"image", "sdxl", "gem.evoke"},
		},
		{
			name:       "re-issuing the same inputs fires again",
			lines:      []string{"/image ill", "/image ill"},
			wantStatus: "image generation on: ill",
			wantFire:   true,
			wantLabel:  "/image - ill",
			wantOn:     true,
			wantBase:   []string{"image", "ill", "gem.evoke"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A literal prompt input merges with no file behind it, so its empty
			// source must not reach the argument list.
			plan := &chat.Plan{Sources: []string{"gem.evoke", ""}}
			sess := chat.NewSession(testChatPlan())
			img := newChatImage(plan, sess)

			var status string
			var fire bool
			for _, line := range tt.lines {
				status, fire = img.set(line, sess)
			}

			require.Equal(t, tt.wantStatus, status)
			require.Equal(t, tt.wantFire, fire)
			require.Equal(t, tt.wantLabel, img.label(chatStyle{}))
			require.Equal(t, tt.wantOn, img.on())
			require.Equal(t, tt.wantBase, img.baseArgs())
			// The setting is handed to the session on every change, so quitting
			// right after one still stores it.
			require.Equal(t, img.args, sess.ImageInputs())
		})
	}
}

func TestPromptLiteral(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   string
		wantOK bool
	}{
		{name: "nothing to draw", prompt: "   "},
		{name: "a single word would resolve as a selector, not a prompt", prompt: "Hm."},
		{
			name:   "an extracted tag line passes through",
			prompt: "sitting, window seat, tank top, bare shoulders, rain on window",
			want:   "sitting, window seat, tank top, bare shoulders, rain on window",
			wantOK: true,
		},
		{
			name:   "a multi-line fallback reply collapses onto one line",
			prompt: "She smiles.\n\n*leans in,\nquietly*",
			want:   "She smiles. *leans in, quietly*",
			wantOK: true,
		},
		{
			name:   "quotes need no escaping",
			prompt: `She laughs. "Not a chance," she says.`,
			want:   `She laughs. "Not a chance," she says.`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok := promptLiteral(tt.prompt)

			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, text)
		})
	}
}

func TestLastAssistantReply(t *testing.T) {
	tests := []struct {
		name  string
		turns []chat.Message
		want  string
	}{
		{name: "nothing said yet"},
		{
			name:  "the character's last line, not the user's",
			turns: []chat.Message{{Role: chat.RoleAssistant, Content: "first"}, {Role: chat.RoleAssistant, Content: "second"}, {Role: chat.RoleUser, Content: "and you?"}},
			want:  "second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := chat.NewSession(testChatPlan())
			for _, m := range tt.turns {
				switch m.Role {
				case chat.RoleUser:
					sess.AddUser(m.Content)
				case chat.RoleAssistant:
					sess.AddAssistant(m.Content)
				}
			}

			require.Equal(t, tt.want, lastAssistantReply(sess))
		})
	}
}

func TestLastLine(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{name: "empty output", out: "", want: ""},
		{name: "the result after a resolution trace", out: "gem => chat/gem.evoke\n\nComfyUI accepted (status: 200 OK)\n", want: "ComfyUI accepted (status: 200 OK)"},
		{name: "trailing blank lines are skipped", out: "queued\n\n   \n", want: "queued"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, lastLine(tt.out))
		})
	}
}

// TestChatImageResumes covers the round trip: a setting made in one session is
// what the next one opens generating with, and --new opens off.
func TestChatImageResumes(t *testing.T) {
	tests := []struct {
		name     string
		resume   bool
		wantOn   bool
		wantArgs []string
	}{
		{name: "resuming restores the setting", resume: true, wantOn: true, wantArgs: []string{"ill", "mf"}},
		{name: "--new opens with generation off", resume: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := testChatPlan()
			path := filepath.Join(t.TempDir(), chat.MemoryKey([]string{"gem"}))

			first := chat.NewSession(plan)
			require.NoError(t, first.Remember(path, []string{"gem"}, true))
			img := newChatImage(plan, first)
			_, _ = img.set("/image ill mf", first)
			require.NoError(t, first.Save())

			next := chat.NewSession(plan)
			require.NoError(t, next.Remember(path, []string{"gem"}, tt.resume))
			resumed := newChatImage(plan, next)

			require.Equal(t, tt.wantOn, resumed.on())
			require.Equal(t, tt.wantArgs, resumed.args)
		})
	}
}
