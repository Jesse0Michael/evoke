package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jesse0michael/evoke/internal/chat"
	"github.com/stretchr/testify/require"
)

func newTestTUIModel(fb *fakeBackend, opening string) chatTUIModel {
	plan := testChatPlan()
	plan.Opening = opening
	return newChatTUIModel(context.Background(), plan, fb, chat.NewSession(plan), chatStyle{}, false, nil, nil, nil, "launched")
}

// newTestTUIMemoryModel builds a model over a session with a stored transcript,
// so resuming can be exercised without a real chat having run.
func newTestTUIMemoryModel(t *testing.T, fb *fakeBackend, opening string, stored []chat.Message) chatTUIModel {
	t.Helper()
	plan := testChatPlan()
	plan.Opening = opening
	path := filepath.Join(t.TempDir(), chat.MemoryKey([]string{"haley"}))
	seed := chat.NewSession(plan)
	require.NoError(t, seed.Remember(path, []string{"haley"}, true))
	for _, m := range stored {
		switch m.Role {
		case chat.RoleUser:
			seed.AddUser(m.Content)
		case chat.RoleAssistant:
			seed.AddAssistant(m.Content)
		}
	}
	require.NoError(t, seed.Save())

	sess := chat.NewSession(plan)
	require.NoError(t, sess.Remember(path, []string{"haley"}, true))
	return newChatTUIModel(context.Background(), plan, fb, sess, chatStyle{}, false, nil, nil, nil, "launched")
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

// TestChatTUIHeader pins the header above the transcript: it is chrome rendered
// outside the log, so the commands stay on screen through scrolling and /reset,
// and it occupies exactly chatHeaderHeight rows at any width so the viewport
// arithmetic in Update holds.
func TestChatTUIHeader(t *testing.T) {
	tests := []struct {
		name     string
		width    int
		setup    func(t *testing.T, m chatTUIModel) chatTUIModel
		contains string
	}{
		{
			name:     "wide terminal shows the whole command list",
			width:    100,
			setup:    func(_ *testing.T, m chatTUIModel) chatTUIModel { return m },
			contains: chatCommands,
		},
		{
			name:  "the header survives /reset, which clears the transcript",
			width: 100,
			setup: func(t *testing.T, m chatTUIModel) chatTUIModel {
				m, _ = enter(t, m, "/reset")
				return m
			},
			contains: chatCommands,
		},
		{
			name:     "a narrow terminal truncates rather than wrapping",
			width:    14,
			setup:    func(_ *testing.T, m chatTUIModel) chatTUIModel { return m },
			contains: "/reset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := &fakeBackend{replies: []string{"unused"}}
			m := newTestTUIModel(fb, "")
			model, _ := m.Update(tea.WindowSizeMsg{Width: tt.width, Height: 24})
			m = tt.setup(t, model.(chatTUIModel))

			header := m.header()
			lines := strings.Split(header, "\n")

			require.Len(t, lines, chatHeaderHeight)
			for _, line := range lines {
				require.LessOrEqual(t, lipgloss.Width(line), tt.width, "header line overflows the terminal: %q", line)
			}
			require.Contains(t, header, tt.contains)
			require.Contains(t, header, "Yasmin", "the header names who you are talking to")
			require.NotContains(t, strings.Join(m.transcript, "\n"), chatCommands, "the header is chrome, not a transcript entry")
			require.Equal(t, 24-chatHeaderHeight-1, m.viewport.Height, "the header and input rows are reserved")
		})
	}
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

// TestChatTUIQuit pins how a session ends: Ctrl-C, with no command of its own.
// A typed /exit is an ordinary unknown command.
func TestChatTUIQuit(t *testing.T) {
	fb := &fakeBackend{replies: []string{"unused"}}
	m := newTestTUIModel(fb, "")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	require.NotNil(t, cmd, "Ctrl-C returns a command")
	require.IsType(t, tea.QuitMsg{}, cmd(), "Ctrl-C quits the program")

	m, cmd = enter(t, m, "/exit")

	require.Nil(t, cmd, "/exit is no longer a command")
	require.Contains(t, strings.Join(m.transcript, "\n"), `unknown command "/exit"`)
}

func TestChatTUIResumesStoredConversation(t *testing.T) {
	opening := "Set the scene. You answer the door."
	fb := &fakeBackend{replies: []string{"still here"}}
	m := newTestTUIMemoryModel(t, fb, opening, []chat.Message{
		{Role: chat.RoleUser, Content: opening},
		{Role: chat.RoleAssistant, Content: "Oh, hello there!"},
		{Role: chat.RoleUser, Content: "how are you?"},
		{Role: chat.RoleAssistant, Content: "good, you?"},
	})

	require.False(t, m.busy, "a resumed conversation does not replay the opening scene")
	log := strings.Join(m.transcript, "\n")
	require.Contains(t, log, "Yasmin: Oh, hello there!", "restored dialogue opens the pane")
	require.Contains(t, log, "You: how are you?")
	require.NotContains(t, log, "Set the scene", "the seeded scene stays hidden after a reload")

	// The restored turns are the session's, so the next reply carries them.
	m, _ = enter(t, m, "back again")
	m = resolveReply(t, m)

	require.Len(t, fb.reqs, 1)
	require.Equal(t, []string{
		"You are Yasmin.", opening, "Oh, hello there!", "how are you?", "good, you?", "back again",
	}, contents(fb.reqs[0].Messages))
}
