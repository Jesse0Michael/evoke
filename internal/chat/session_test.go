package chat

import (
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
