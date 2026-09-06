package chat

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// budgetPlan builds a plan whose token budget equals contextWindow (reserve and
// margin zero) so tests can reason about estimateTokens directly.
func budgetPlan(contextWindow int, system string) *Plan {
	return &Plan{
		Model:        "test-model",
		SystemPrompt: system,
		Sampling:     Sampling{MaxOutputTokens: 0},
		History:      HistoryPolicy{ContextWindow: contextWindow, SafetyMargin: 0, MinRecentTurns: 1},
	}
}

func contents(msgs []Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}

// msg40 returns a distinct 40-character message, which estimateTokens counts as
// exactly 14 tokens ((40+3)/4 + 4), so budget arithmetic in these tests is exact.
func msg40(tag string) string {
	return tag + strings.Repeat("-", 40-len(tag))
}

var (
	sys = msg40("sys")
	u1  = msg40("u1")
	a1  = msg40("a1")
	u2  = msg40("u2")
	a2  = msg40("a2")
	u3  = msg40("u3")
)

func TestSessionBuildMessages(t *testing.T) {
	t.Run("drops oldest pair, pins system and newest user", func(t *testing.T) {
		// system + 5 turns = 87 tokens; budget 60 fits after dropping one pair
		// (system + u2 + a2 + u3 = 59), but not two.
		sess := NewSession(budgetPlan(60, sys))
		sess.AddUser(u1)
		sess.AddAssistant(a1)
		sess.AddUser(u2)
		sess.AddAssistant(a2)
		sess.AddUser(u3)

		req, err := sess.Request()

		require.NoError(t, err)
		require.Equal(t, []string{sys, u2, a2, u3}, contents(req.Messages))
		require.Equal(t, RoleSystem, req.Messages[0].Role)
		require.Equal(t, RoleUser, req.Messages[len(req.Messages)-1].Role)
	})

	t.Run("request begins system then user", func(t *testing.T) {
		// A request must begin system -> user so it satisfies strict-alternation
		// chat templates (Mistral, etc.).
		sess := NewSession(budgetPlan(100000, sys))
		sess.AddUser(u1)

		req, err := sess.Request()

		require.NoError(t, err)
		require.Equal(t, []string{sys, u1}, contents(req.Messages))
		require.Equal(t, RoleSystem, req.Messages[0].Role)
		require.Equal(t, RoleUser, req.Messages[1].Role)
	})

	t.Run("irreducible prompt returns an error", func(t *testing.T) {
		sess := NewSession(budgetPlan(50, strings.Repeat("s", 400)))
		sess.AddUser("hello")

		_, err := sess.Request()

		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot fit the context window")
	})
}

func TestSessionTranscript(t *testing.T) {
	sess := NewSession(budgetPlan(100000, "system"))

	sess.AddUser("hello")
	sess.AddAssistant("hi")
	require.Equal(t, 2, sess.Context().RetainedTurns)

	// A failed send drops the pending user, not a completed pair.
	sess.AddUser("orphan")
	require.Equal(t, 3, sess.Context().RetainedTurns)
	sess.DropPendingUser()
	require.Equal(t, 2, sess.Context().RetainedTurns)

	// Reset clears the turns.
	sess.Reset()
	require.Equal(t, 0, sess.Context().RetainedTurns)
}

func TestSessionRequestCarriesModelAndSampling(t *testing.T) {
	temp := 0.7
	tests := []struct {
		name      string
		modelPath string
		wantModel string
	}{
		{
			// mlx_lm.server loads whatever the body names, so the request must
			// name the same reference the backend was launched with.
			name:      "resolved path names the loaded model",
			modelPath: "/models/test-model",
			wantModel: "/models/test-model",
		},
		{
			name:      "unresolved model falls back to the logical name",
			modelPath: "",
			wantModel: "test-model",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := budgetPlan(100000, "system")
			plan.ModelPath = tt.modelPath
			plan.Sampling.Temperature = &temp
			sess := NewSession(plan)
			sess.AddUser("hello")

			req, err := sess.Request()

			require.NoError(t, err)
			require.Equal(t, tt.wantModel, req.Model)
			require.Same(t, &temp, req.Sampling.Temperature)
		})
	}
}

func TestSessionRestore(t *testing.T) {
	u := func(c string) Message { return Message{Role: RoleUser, Content: c} }
	a := func(c string) Message { return Message{Role: RoleAssistant, Content: c} }

	tests := []struct {
		name  string
		turns []Message
		want  []Message
	}{
		{
			name:  "nothing stored",
			turns: nil,
			want:  nil,
		},
		{
			name:  "complete pairs are restored whole",
			turns: []Message{u("u1"), a("a1"), u("u2"), a("a2")},
			want:  []Message{u("u1"), a("a1"), u("u2"), a("a2")},
		},
		{
			name:  "a user message with no reply is dropped",
			turns: []Message{u("u1"), a("a1"), u("u2")},
			want:  []Message{u("u1"), a("a1")},
		},
		{
			name:  "a transcript that does not start with a user turn is discarded",
			turns: []Message{a("a1"), u("u1"), a("a2")},
			want:  []Message{},
		},
		{
			name:  "a transcript is truncated at the first break in alternation",
			turns: []Message{u("u1"), a("a1"), a("a2"), u("u2")},
			want:  []Message{u("u1"), a("a1")},
		},
		{
			name:  "a system message in the dialogue is not restored as a turn",
			turns: []Message{u("u1"), a("a1"), {Role: RoleSystem, Content: "sys"}, a("a2")},
			want:  []Message{u("u1"), a("a1")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := NewSession(budgetPlan(8192, sys))

			sess.restore(tt.turns)

			require.Equal(t, tt.want, sess.Turns())
		})
	}
}

// TestSessionRestoreBeyondBudget covers the reason a transcript can be stored
// verbatim: the sliding window, not the store, decides how much of a long
// history a request carries.
func TestSessionRestoreBeyondBudget(t *testing.T) {
	sess := NewSession(budgetPlan(60, sys))
	sess.restore([]Message{
		{Role: RoleUser, Content: u1}, {Role: RoleAssistant, Content: a1},
		{Role: RoleUser, Content: u2}, {Role: RoleAssistant, Content: a2},
	})
	sess.AddUser(u3)

	req, err := sess.Request()

	require.NoError(t, err)
	require.Equal(t, []string{sys, u2, a2, u3}, contents(req.Messages))
}

func TestMemoryKey(t *testing.T) {
	tests := []struct {
		name     string
		inputs   []string
		wantSlug string
		// equal must produce the same key as inputs, differs a different one.
		equal   []string
		differs []string
	}{
		{
			name:     "inputs are sorted so argument order does not fork the conversation",
			inputs:   []string{"girlfriend", "haley"},
			wantSlug: "girlfriend+haley",
			equal:    []string{"haley", "girlfriend"},
			differs:  []string{"girlfriend", "haley", "beach"},
		},
		{
			name:     "case and surrounding whitespace are normalized",
			inputs:   []string{" Haley ", "GIRLFRIEND"},
			wantSlug: "girlfriend+haley",
			equal:    []string{"haley", "girlfriend"},
			differs:  []string{"haley"},
		},
		{
			name:     "path separators are sanitized out of the name",
			inputs:   []string{"./chars/haley.evoke"},
			wantSlug: "chars-haley.evoke",
			equal:    []string{"./chars/haley.evoke"},
			differs:  []string{"chars-haley.evoke"},
		},
		{
			name:     "a literal prompt is keyed like any other input",
			inputs:   []string{"haley", "you meet at a bar"},
			wantSlug: "haley+you-meet-at-a-bar",
			equal:    []string{"you meet at a bar", "haley"},
			differs:  []string{"haley", "you meet at a cafe"},
		},
		{
			name:     "adjacent inputs cannot collide by concatenation",
			inputs:   []string{"ab", "c"},
			wantSlug: "ab+c",
			equal:    []string{"c", "ab"},
			differs:  []string{"a", "bc"},
		},
		{
			name:     "a long input truncates to a readable slug but keeps a unique key",
			inputs:   []string{strings.Repeat("x", 80)},
			wantSlug: strings.Repeat("x", maxSlugLen),
			equal:    []string{strings.Repeat("x", 80)},
			differs:  []string{strings.Repeat("x", 79)},
		},
		{
			name:     "an unnameable input falls back to a fixed slug",
			inputs:   []string{"!!!"},
			wantSlug: "session",
			equal:    []string{"!!!"},
			differs:  []string{"???"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := MemoryKey(tt.inputs)

			require.Regexp(t, `^`+regexp.QuoteMeta(tt.wantSlug)+`-[0-9a-f]{8}\.json$`, key)
			require.Equal(t, key, MemoryKey(tt.equal))
			require.NotEqual(t, key, MemoryKey(tt.differs))
		})
	}
}

func TestSessionMemory(t *testing.T) {
	dialogue := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, Content: "hello"},
	}
	tests := []struct {
		name string
		// stored is written to the transcript path before the session opens it;
		// empty means no file exists yet.
		stored string
		// resume is false for --new.
		resume    bool
		wantTurns []Message
		wantErr   bool
	}{
		{
			name:      "a missing file is an empty session, not an error",
			resume:    true,
			wantTurns: nil,
		},
		{
			name:      "a stored transcript is restored",
			stored:    `{"version":1,"inputs":["haley"],"updated":"2026-09-05T00:00:00Z","turns":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]}`,
			resume:    true,
			wantTurns: dialogue,
		},
		{
			name:      "a malformed file reports the problem and starts fresh",
			stored:    "{ not json",
			resume:    true,
			wantTurns: nil,
			wantErr:   true,
		},
		{
			name:      "an unsupported version reports the problem and starts fresh",
			stored:    `{"version":99,"turns":[{"role":"user","content":"hi"}]}`,
			resume:    true,
			wantTurns: nil,
			wantErr:   true,
		},
		{
			// --new: the stored conversation is not read, and is left on disk
			// until this session has a turn of its own to replace it with.
			name:      "not resuming ignores a stored transcript",
			stored:    `{"version":1,"turns":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]}`,
			resume:    false,
			wantTurns: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions", MemoryKey([]string{"haley"}))
			if tt.stored != "" {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
				require.NoError(t, os.WriteFile(path, []byte(tt.stored), 0o600))
			}
			sess := NewSession(budgetPlan(8192, sys))

			err := sess.Remember(path, []string{"haley"}, tt.resume)

			require.Equal(t, tt.wantErr, err != nil)
			require.Equal(t, tt.wantTurns, sess.Turns())

			// Whatever was there, saving replaces it and reads back intact.
			sess.AddUser("hi")
			sess.AddAssistant("hello")
			require.NoError(t, sess.Save())
			reopened := NewSession(budgetPlan(8192, sys))
			require.NoError(t, reopened.Remember(path, []string{"haley"}, true))
			require.Equal(t, append(append([]Message{}, tt.wantTurns...), dialogue...), reopened.Turns())

			// Staging files are renamed into place, never left behind.
			entries, err := os.ReadDir(filepath.Dir(path))
			require.NoError(t, err)
			require.Len(t, entries, 1)
		})
	}
}

func TestSessionWithoutMemoryStoresNothing(t *testing.T) {
	sess := NewSession(budgetPlan(8192, sys))
	sess.AddUser("hi")
	sess.AddAssistant("hello")

	// A session never told where to store its transcript simply does not.
	require.NoError(t, sess.Save())
}
