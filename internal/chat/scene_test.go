package chat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSessionAside covers the one-off request: it carries the recent transcript
// under a different system prompt, starts on a user turn so a strict template
// accepts it, ends on the instruction, and leaves the session untouched.
func TestSessionAside(t *testing.T) {
	tests := []struct {
		name          string
		contextWindow int
		turns         []Message
		maxTurns      int
		want          []string
	}{
		{
			name:          "no dialogue yet: the instruction alone",
			contextWindow: 1000,
			maxTurns:      4,
			want:          []string{"aside-system", "aside-instruction"},
		},
		{
			name:          "the newest turns, then the instruction",
			contextWindow: 1000,
			turns:         []Message{{Role: RoleUser, Content: u1}, {Role: RoleAssistant, Content: a1}, {Role: RoleUser, Content: u2}, {Role: RoleAssistant, Content: a2}},
			maxTurns:      4,
			want:          []string{"aside-system", u1, a1, u2, a2, "aside-instruction"},
		},
		{
			name:          "a window shorter than the transcript keeps the newest, aligned to a user turn",
			contextWindow: 1000,
			turns:         []Message{{Role: RoleUser, Content: u1}, {Role: RoleAssistant, Content: a1}, {Role: RoleUser, Content: u2}, {Role: RoleAssistant, Content: a2}},
			maxTurns:      3,
			want:          []string{"aside-system", u2, a2, "aside-instruction"},
		},
		{
			// The system prompt and instruction cost 19 tokens together and each
			// 40-character turn costs 14, so 50 fits one pair and not two.
			name:          "over budget drops the oldest pair",
			contextWindow: 50,
			turns:         []Message{{Role: RoleUser, Content: u1}, {Role: RoleAssistant, Content: a1}, {Role: RoleUser, Content: u2}, {Role: RoleAssistant, Content: a2}},
			maxTurns:      4,
			want:          []string{"aside-system", u2, a2, "aside-instruction"},
		},
		{
			name:          "a budget nothing fits still asks the question",
			contextWindow: 1,
			turns:         []Message{{Role: RoleUser, Content: u1}, {Role: RoleAssistant, Content: a1}},
			maxTurns:      4,
			want:          []string{"aside-system", "aside-instruction"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := budgetPlan(tt.contextWindow, sys)
			plan.ModelPath = "/models/test.gguf"
			sess := NewSession(plan)
			for _, m := range tt.turns {
				switch m.Role {
				case RoleUser:
					sess.AddUser(m.Content)
				case RoleAssistant:
					sess.AddAssistant(m.Content)
				}
			}
			sess.SetRetrievedContext("retrieved knowledge")

			req := sess.Aside("aside-system", "aside-instruction", tt.maxTurns, Sampling{MaxOutputTokens: 0})

			require.Equal(t, tt.want, contents(req.Messages))
			require.Equal(t, RoleSystem, req.Messages[0].Role)
			require.Equal(t, RoleUser, req.Messages[len(req.Messages)-1].Role)
			// The request names the resolved model, as the conversation's does.
			require.Equal(t, "/models/test.gguf", req.Model)

			// An aside must not be able to reach the dialogue: no turn recorded,
			// and the pending retrieval left for the conversation to consume.
			require.Equal(t, tt.turns, sess.Turns())
			require.Equal(t, "retrieved knowledge", sess.retrievedContext)
		})
	}
}

// TestSceneRequest pins the sampling an extraction runs under. Each value is
// load-bearing: a seed would stop mlx_lm.server batching the aside into the
// running generation, and thinking left on lets a reasoning model spend the
// whole budget and return nothing.
func TestSceneRequest(t *testing.T) {
	sess := NewSession(budgetPlan(4096, sys))
	sess.AddUser(u1)
	sess.AddAssistant(a1)

	req := SceneRequest(sess)

	require.Nil(t, req.Sampling.Seed)
	require.NotNil(t, req.Sampling.Thinking)
	require.False(t, *req.Sampling.Thinking)
	require.NotNil(t, req.Sampling.Temperature)
	require.Equal(t, sceneTemperature, *req.Sampling.Temperature)
	require.Equal(t, sceneMaxTokens, req.Sampling.MaxOutputTokens)
	require.Nil(t, req.Sampling.RepeatPenalty)
	// The character's own system prompt is replaced, not appended to.
	require.Equal(t, sceneSystem, req.Messages[0].Content)
	require.NotContains(t, contents(req.Messages), sys)
}

// TestParseScene covers the cleanup. Neither backend can constrain the output —
// mlx_lm.server has no response_format at all — so the shape arrives by
// instruction and every one of these is something a model does.
func TestParseScene(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		want      string
		wantError bool
	}{
		{
			name: "a bare tag line",
			text: "sitting, window seat, tank top, rain on window",
			want: "sitting, window seat, tank top, rain on window",
		},
		{
			name: "wrapped in a code fence",
			text: "```\nsitting, tank top\n```",
			want: "sitting, tank top",
		},
		{
			name: "wrapped onto several lines",
			text: "sitting, tank top,\nbare shoulders,\nrain on window",
			want: "sitting, tank top, bare shoulders, rain on window",
		},
		{
			name: "a trailing period is prose punctuation, not a tag",
			text: "sitting, tank top.",
			want: "sitting, tank top",
		},
		{name: "an empty answer is not a scene", text: "   \n  ", wantError: true},
		{name: "a fence with nothing in it", text: "``````", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseScene(tt.text)

			require.Equal(t, tt.wantError, err != nil)
			require.Equal(t, tt.want, got)
		})
	}
}
