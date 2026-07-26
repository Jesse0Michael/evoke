package cli

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jesse0michael/evoke/internal/chat"
	"github.com/stretchr/testify/require"
)

func newTestTUIModel(fb *fakeBackend, opening string) chatTUIModel {
	plan := testChatPlan()
	plan.Opening = opening
	return newChatTUIModel(context.Background(), plan, fb, chatStyle{}, false, nil, nil)
}

// resolveReply runs the pending reply command synchronously (fakeBackend is
// synchronous) and feeds the result back through Update, as the tea runtime would.
func resolveReply(t *testing.T, m chatTUIModel) chatTUIModel {
	t.Helper()
	cmd := m.replyCmd()
	msg, ok := cmd().(replyMsg)
	require.True(t, ok, "reply command must yield a replyMsg")
	model, _ := m.Update(msg)
	return model.(chatTUIModel)
}

func enter(t *testing.T, m chatTUIModel, text string) (chatTUIModel, tea.Cmd) {
	t.Helper()
	m.input.SetValue(text)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return model.(chatTUIModel), cmd
}

func TestChatTUISeedsOpening(t *testing.T) {
	fb := &fakeBackend{replies: []string{"Oh, hello there!"}}
	m := newTestTUIModel(fb, "Set the scene. You answer the door.")

	require.True(t, m.busy, "seeding an opening makes the model busy until the character replies")
	m = resolveReply(t, m)

	require.False(t, m.busy)
	require.Contains(t, strings.Join(m.transcript, "\n"), "Yasmin: Oh, hello there!", "character opens the scene")
	// The seed itself is a user turn but is never shown.
	require.NotContains(t, strings.Join(m.transcript, "\n"), "Set the scene")
	require.Len(t, fb.reqs, 1)
	require.Equal(t, chat.RoleUser, fb.reqs[0].Messages[1].Role)
}

func TestChatTUIEnterGatedWhileBusy(t *testing.T) {
	fb := &fakeBackend{replies: []string{"unused"}}
	m := newTestTUIModel(fb, "")
	m.busy = true
	before := len(m.transcript)

	m, cmd := enter(t, m, "typed ahead while waiting")

	require.Nil(t, cmd, "Enter does nothing while a reply is in flight")
	require.True(t, m.busy)
	require.Equal(t, "typed ahead while waiting", m.input.Value(), "the composed message is preserved, not submitted")
	require.Len(t, m.transcript, before, "no turn is recorded")
	require.Empty(t, fb.reqs, "nothing is sent to the backend")
}

func TestChatTUISubmitAndReply(t *testing.T) {
	fb := &fakeBackend{replies: []string{"nice to meet you"}}
	m := newTestTUIModel(fb, "")

	m, _ = enter(t, m, "hello")

	require.True(t, m.busy, "submitting a message starts a reply")
	require.Empty(t, m.input.Value(), "input is cleared on submit")
	require.Contains(t, strings.Join(m.transcript, "\n"), "You: hello")

	m = resolveReply(t, m)
	require.False(t, m.busy)
	require.Contains(t, strings.Join(m.transcript, "\n"), "Yasmin: nice to meet you")
}

func TestChatTUIResetReplaysOpening(t *testing.T) {
	fb := &fakeBackend{replies: []string{"Opening line", "Reopened line"}}
	m := newTestTUIModel(fb, "Set the scene.")
	m = resolveReply(t, m) // consume the startup opening

	m, _ = enter(t, m, "/reset")

	require.True(t, m.busy, "/reset replays the opening, so the character speaks again")
	require.Contains(t, strings.Join(m.transcript, "\n"), "(conversation reset)")
	m = resolveReply(t, m)
	require.Contains(t, strings.Join(m.transcript, "\n"), "Yasmin: Reopened line")
}

func TestChatTUIExitCommand(t *testing.T) {
	fb := &fakeBackend{replies: []string{"unused"}}
	m := newTestTUIModel(fb, "")

	_, cmd := enter(t, m, "/exit")

	require.NotNil(t, cmd, "/exit returns a command")
	require.IsType(t, tea.QuitMsg{}, cmd(), "/exit quits the program")
}
