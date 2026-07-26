package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jesse0michael/evoke/internal/chat"
	"github.com/stretchr/testify/require"
)

// fakeBackend is a scripted chatBackend used to drive the interactive loop
// without a real LLM.
type fakeBackend struct {
	replies []string
	i       int
	reqs    []chat.CompletionRequest
	usage   chat.Usage
}

func (f *fakeBackend) next() string {
	r := f.replies[f.i%len(f.replies)]
	f.i++
	return r
}

func (f *fakeBackend) Stream(_ context.Context, req chat.CompletionRequest, onDelta func(string)) (string, chat.Usage, error) {
	f.reqs = append(f.reqs, req)
	reply := f.next()
	if onDelta != nil {
		onDelta(reply)
	}
	return reply, f.usage, nil
}

func (f *fakeBackend) Complete(_ context.Context, req chat.CompletionRequest) (string, chat.Usage, error) {
	f.reqs = append(f.reqs, req)
	return f.next(), f.usage, nil
}

func testChatPlan() *chat.Plan {
	return &chat.Plan{
		Model:        "test-model",
		Backend:      "llama.cpp",
		SystemPrompt: "You are Yasmin.",
		Sampling:     chat.Sampling{MaxOutputTokens: 128},
		History:      chat.HistoryPolicy{ContextWindow: 8192, SafetyMargin: 128, MinRecentTurns: 1},
		Display:      chat.Display{CharacterName: "Yasmin", Backend: "llama.cpp", Model: "test-model", ContextWindow: 8192},
	}
}

func TestRunChatLoop(t *testing.T) {
	plan := testChatPlan()
	fb := &fakeBackend{replies: []string{"hello back"}}
	in := strings.NewReader("hello\n/context\n/reset\n/exit\n")
	var out bytes.Buffer

	err := runChatLoop(t.Context(), plan, fb, true, false, in, &out, nil, nil, nil, chatStyle{})

	require.NoError(t, err)
	s := out.String()
	require.Contains(t, s, "Character: Yasmin")
	require.Contains(t, s, "Yasmin: hello back", "assistant reply is streamed")
	require.Contains(t, s, "retained turns:", "/context reports budget")
	require.Contains(t, s, "(conversation reset)")
	require.Contains(t, s, "Session ended.")

	// Exactly one model call for the one user message; slash commands are local.
	require.Len(t, fb.reqs, 1)
	require.Equal(t, chat.RoleSystem, fb.reqs[0].Messages[0].Role)
	require.Equal(t, "test-model", fb.reqs[0].Model)
	// The request goes system -> user so strict-alternation templates accept it.
	require.Equal(t, chat.RoleUser, fb.reqs[0].Messages[1].Role)
}

func TestRunChatLoopSeedsOpening(t *testing.T) {
	plan := testChatPlan()
	plan.Opening = "Set the scene and begin in character. The situation:\n\nYou answer the door."
	fb := &fakeBackend{replies: []string{"Oh, hello there!", "nice to meet you"}}
	in := strings.NewReader("hi\n/exit\n")
	var out bytes.Buffer

	err := runChatLoop(t.Context(), plan, fb, false, false, in, &out, nil, nil, nil, chatStyle{})

	require.NoError(t, err)
	s := out.String()
	// The character speaks first, before any "You:" prompt, in response to the seed.
	require.Contains(t, s, "Yasmin: Oh, hello there!", "character responds to the opening before the user speaks")
	require.NotContains(t, s, "You: Set the scene", "the seed message is never echoed as the user's line")

	// Two backend calls: the seeded opening, then the user's "hi". The first
	// request carries the opening as the sole user turn after the system prompt.
	require.Len(t, fb.reqs, 2)
	require.Equal(t, chat.RoleSystem, fb.reqs[0].Messages[0].Role)
	require.Equal(t, chat.RoleUser, fb.reqs[0].Messages[1].Role)
	require.Equal(t, plan.Opening, fb.reqs[0].Messages[1].Content)
	// The second request retains the seeded pair plus the user's message.
	require.Equal(t, "hi", fb.reqs[1].Messages[len(fb.reqs[1].Messages)-1].Content)
}

func TestRunChatLoopResetReplaysOpening(t *testing.T) {
	plan := testChatPlan()
	plan.Opening = "Set the scene and begin in character. The situation:\n\nYou answer the door."
	fb := &fakeBackend{replies: []string{"Oh, hello!", "Welcome back!"}}
	in := strings.NewReader("/reset\n/exit\n")
	var out bytes.Buffer

	err := runChatLoop(t.Context(), plan, fb, false, false, in, &out, nil, nil, nil, chatStyle{})

	require.NoError(t, err)
	s := out.String()
	require.Contains(t, s, "(conversation reset)")
	require.Contains(t, s, "Yasmin: Oh, hello!", "character opens at startup")
	require.Contains(t, s, "Yasmin: Welcome back!", "character re-opens after /reset")

	// Two openings: one at startup, one after /reset. Each begins from a cleared
	// transcript, so the request is exactly system + the opening turn.
	require.Len(t, fb.reqs, 2)
	for _, req := range fb.reqs {
		require.Len(t, req.Messages, 2)
		require.Equal(t, chat.RoleSystem, req.Messages[0].Role)
		require.Equal(t, plan.Opening, req.Messages[1].Content)
	}
}

func TestRunChatLoopVerboseDiagnostics(t *testing.T) {
	plan := testChatPlan()
	fb := &fakeBackend{replies: []string{"hello back"}, usage: chat.Usage{PromptTokens: 1234, CompletionTokens: 87}}
	in := strings.NewReader("hello\n/exit\n")
	var out bytes.Buffer
	backendLog := func() string { return "slot released\ncontext shift" }

	err := runChatLoop(t.Context(), plan, fb, true, true, in, &out, nil, nil, backendLog, chatStyle{})

	require.NoError(t, err)
	s := out.String()
	require.Contains(t, s, "prompt 1234", "real token usage is shown in verbose mode")
	require.Contains(t, s, "reply 87")
	require.Contains(t, s, "--- llama-server ---", "backend log is surfaced in verbose mode")
	require.Contains(t, s, "context shift")
}

func TestRunChatLoopExitsOnEOF(t *testing.T) {
	plan := testChatPlan()
	fb := &fakeBackend{replies: []string{"yo"}}
	in := strings.NewReader("hey\n") // no /exit: input ends, loop exits on EOF
	var out bytes.Buffer

	err := runChatLoop(t.Context(), plan, fb, false, false, in, &out, nil, nil, nil, chatStyle{})

	require.NoError(t, err)
	require.Contains(t, out.String(), "Yasmin: yo")
	require.Contains(t, out.String(), "Session ended.")
}

func TestRunChatLoopUnknownCommand(t *testing.T) {
	plan := testChatPlan()
	fb := &fakeBackend{replies: []string{"unused"}}
	in := strings.NewReader("/bogus\n/exit\n")
	var out bytes.Buffer

	err := runChatLoop(t.Context(), plan, fb, true, false, in, &out, nil, nil, nil, chatStyle{})

	require.NoError(t, err)
	require.Contains(t, out.String(), `unknown command "/bogus"`)
	require.Empty(t, fb.reqs, "an unknown slash command is never sent to the model")
}

func TestRunChatLoopExitsWhenBackendDies(t *testing.T) {
	plan := testChatPlan()
	fb := &fakeBackend{replies: []string{"unused"}}
	backendDone := make(chan struct{})
	close(backendDone) // backend already gone
	var out bytes.Buffer

	err := runChatLoop(t.Context(), plan, fb, true, false, strings.NewReader(""), &out,
		backendDone, func() error { return context.Canceled }, nil, chatStyle{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "backend exited")
	require.Contains(t, out.String(), "backend exited unexpectedly")
}

func TestBuildTrustedChatConfig(t *testing.T) {
	cfg := chatConfig{Host: "127.0.0.1", Port: 8080, Executable: "llama-server", ModelPaths: "/env/models"}
	s := &Settings{Chat: &ChatSettings{
		Executable: "/opt/llama/llama-server",
		Port:       9090,
		ModelPaths: []string{"/settings/models"},
	}}

	tc := buildTrustedChatConfig(s, cfg)

	require.Equal(t, "/opt/llama/llama-server", tc.Executable, "settings override env defaults")
	require.Equal(t, 9090, tc.Port, "settings override env defaults")
	require.Equal(t, "127.0.0.1", tc.Host, "env default kept when settings omit it")
	// Model directories from both env and settings are searched.
	require.Equal(t, []string{"/env/models", "/settings/models"}, tc.ModelDirs)
}
