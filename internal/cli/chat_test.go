package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
	in := strings.NewReader("hello\n/context\n/reset\n")
	var out bytes.Buffer

	err := runChatLoop(t.Context(), plan, fb, chat.NewSession(plan), true, false, in, &out, nil, nil, nil, chatStyle{}, nil, "launched")

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
	in := strings.NewReader("hi\n")
	var out bytes.Buffer

	err := runChatLoop(t.Context(), plan, fb, chat.NewSession(plan), false, false, in, &out, nil, nil, nil, chatStyle{}, nil, "launched")

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
	in := strings.NewReader("/reset\n")
	var out bytes.Buffer

	err := runChatLoop(t.Context(), plan, fb, chat.NewSession(plan), false, false, in, &out, nil, nil, nil, chatStyle{}, nil, "launched")

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
	in := strings.NewReader("hello\n")
	var out bytes.Buffer
	backendLog := func() string { return "slot released\ncontext shift" }

	err := runChatLoop(t.Context(), plan, fb, chat.NewSession(plan), true, true, in, &out, nil, nil, backendLog, chatStyle{}, nil, "launched")

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
	in := strings.NewReader("hey\n") // input ends, loop exits on EOF
	var out bytes.Buffer

	err := runChatLoop(t.Context(), plan, fb, chat.NewSession(plan), false, false, in, &out, nil, nil, nil, chatStyle{}, nil, "launched")

	require.NoError(t, err)
	require.Contains(t, out.String(), "Yasmin: yo")
	require.Contains(t, out.String(), "Session ended.")
}

func TestRunChatLoopUnknownCommand(t *testing.T) {
	plan := testChatPlan()
	fb := &fakeBackend{replies: []string{"unused"}}
	in := strings.NewReader("/bogus\n")
	var out bytes.Buffer

	err := runChatLoop(t.Context(), plan, fb, chat.NewSession(plan), true, false, in, &out, nil, nil, nil, chatStyle{}, nil, "launched")

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

	err := runChatLoop(t.Context(), plan, fb, chat.NewSession(plan), true, false, strings.NewReader(""), &out,
		backendDone, func() error { return context.Canceled }, nil, chatStyle{}, nil, "launched")

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

// TestRunChatLoopMemory covers the durable conversation end to end through the
// interactive loop: what is restored on start, what the opening scene does about
// it, and what ends up stored afterwards.
func TestRunChatLoopMemory(t *testing.T) {
	opening := "Set the scene and begin in character. The situation:\n\nYou answer the door."
	tests := []struct {
		name string
		// stored is the transcript already on disk when the session starts.
		stored []chat.Message
		// forget simulates the --new flag.
		forget  bool
		opening bool
		input   string
		// wantRequests is the message content of every backend request, system
		// prompt excluded, so both what was restored and what was seeded show up.
		wantRequests [][]string
		wantStored   []chat.Message
	}{
		{
			name:         "a first session stores its turns",
			input:        "hello\n",
			wantRequests: [][]string{{"hello"}},
			wantStored: []chat.Message{
				{Role: chat.RoleUser, Content: "hello"},
				{Role: chat.RoleAssistant, Content: "reply 1"},
			},
		},
		{
			name: "a stored conversation is resumed and extended",
			stored: []chat.Message{
				{Role: chat.RoleUser, Content: "hello"},
				{Role: chat.RoleAssistant, Content: "hi there"},
			},
			input:        "still there?\n",
			wantRequests: [][]string{{"hello", "hi there", "still there?"}},
			wantStored: []chat.Message{
				{Role: chat.RoleUser, Content: "hello"},
				{Role: chat.RoleAssistant, Content: "hi there"},
				{Role: chat.RoleUser, Content: "still there?"},
				{Role: chat.RoleAssistant, Content: "reply 1"},
			},
		},
		{
			name:    "resuming does not replay the opening scene",
			opening: true,
			stored: []chat.Message{
				{Role: chat.RoleUser, Content: opening},
				{Role: chat.RoleAssistant, Content: "Oh, hello!"},
			},
			input:        "hi\n",
			wantRequests: [][]string{{opening, "Oh, hello!", "hi"}},
			wantStored: []chat.Message{
				{Role: chat.RoleUser, Content: opening},
				{Role: chat.RoleAssistant, Content: "Oh, hello!"},
				{Role: chat.RoleUser, Content: "hi"},
				{Role: chat.RoleAssistant, Content: "reply 1"},
			},
		},
		{
			name:         "the opening scene is stored on a first session",
			opening:      true,
			input:        "", // the scene is seeded before any input, so EOF alone exercises it
			wantRequests: [][]string{{opening}},
			wantStored: []chat.Message{
				{Role: chat.RoleUser, Content: opening},
				{Role: chat.RoleAssistant, Content: "reply 1"},
			},
		},
		{
			name: "--new ignores the stored conversation and replaces it",
			stored: []chat.Message{
				{Role: chat.RoleUser, Content: "hello"},
				{Role: chat.RoleAssistant, Content: "hi there"},
			},
			forget:       true,
			input:        "fresh start\n",
			wantRequests: [][]string{{"fresh start"}},
			wantStored: []chat.Message{
				{Role: chat.RoleUser, Content: "fresh start"},
				{Role: chat.RoleAssistant, Content: "reply 1"},
			},
		},
		{
			name: "/reset discards the stored conversation immediately",
			stored: []chat.Message{
				{Role: chat.RoleUser, Content: "hello"},
				{Role: chat.RoleAssistant, Content: "hi there"},
			},
			input:        "/reset\n",
			wantRequests: nil,
			wantStored:   nil,
		},
		{
			name: "/reset returns to the opening scene and stores that",
			stored: []chat.Message{
				{Role: chat.RoleUser, Content: opening},
				{Role: chat.RoleAssistant, Content: "Oh, hello!"},
				{Role: chat.RoleUser, Content: "hi"},
				{Role: chat.RoleAssistant, Content: "hey"},
			},
			opening:      true,
			input:        "/reset\n",
			wantRequests: [][]string{{opening}},
			wantStored: []chat.Message{
				{Role: chat.RoleUser, Content: opening},
				{Role: chat.RoleAssistant, Content: "reply 1"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := testChatPlan()
			if tt.opening {
				plan.Opening = opening
			}
			path := filepath.Join(t.TempDir(), "sessions", chat.MemoryKey([]string{"haley"}))
			if tt.stored != nil {
				seed := chat.NewSession(plan)
				require.NoError(t, seed.Remember(path, []string{"haley"}, true))
				for _, m := range tt.stored {
					switch m.Role {
					case chat.RoleUser:
						seed.AddUser(m.Content)
					case chat.RoleAssistant:
						seed.AddAssistant(m.Content)
					}
				}
				require.NoError(t, seed.Save())
			}
			sess := chat.NewSession(plan)
			// forget is the --new flag: attach storage but do not resume.
			require.NoError(t, sess.Remember(path, []string{"haley"}, !tt.forget))
			fb := &fakeBackend{replies: []string{"reply 1", "reply 2"}}
			var out bytes.Buffer

			err := runChatLoop(t.Context(), plan, fb, sess, false, false, strings.NewReader(tt.input), &out, nil, nil, nil, chatStyle{}, nil, "launched")

			require.NoError(t, err)
			var gotRequests [][]string
			for _, req := range fb.reqs {
				// Drop the pinned system prompt; the dialogue is what memory owns.
				gotRequests = append(gotRequests, contents(req.Messages[1:]))
			}
			require.Equal(t, tt.wantRequests, gotRequests)

			// The transcript on disk is what a following session would resume.
			reopened := chat.NewSession(plan)
			require.NoError(t, reopened.Remember(path, []string{"haley"}, true))
			require.Equal(t, tt.wantStored, reopened.Turns())
		})
	}
}

// contents lists the content of each message in order.
func contents(msgs []chat.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}

// TestAcquireBackend covers the decision made when something is already
// listening on the chat endpoint: use it, refuse, or ask.
func TestAcquireBackend(t *testing.T) {
	const modelPath = "/models/nemo.gguf"
	tests := []struct {
		name string
		// props is the llama-server /props payload the fake backend serves.
		props       string
		interactive bool
		input       string
		wantAdopted bool
		wantErr     bool
		wantOutput  []string
	}{
		{
			name:        "the same model is used without asking",
			props:       `{"model_path":"` + modelPath + `","default_generation_settings":{"n_ctx":8192}}`,
			interactive: true,
			wantAdopted: true,
			wantOutput:  []string{"Using the llama.cpp backend already running"},
		},
		{
			name:        "a different model is refused unless confirmed",
			props:       `{"model_path":"/models/other.gguf","default_generation_settings":{"n_ctx":8192}}`,
			interactive: true,
			input:       "n\n",
			wantErr:     true,
			wantOutput: []string{
				"it has /models/other.gguf loaded",
				"ignores the model named in a request",
				"Connect anyway? [y/N]",
			},
		},
		{
			name:        "a different model is used when confirmed",
			props:       `{"model_path":"/models/other.gguf","default_generation_settings":{"n_ctx":8192}}`,
			interactive: true,
			input:       "y\n",
			wantAdopted: true,
			wantOutput:  []string{"Connect anyway? [y/N]"},
		},
		{
			name:        "a non-interactive run refuses instead of prompting",
			props:       `{"model_path":"/models/other.gguf","default_generation_settings":{"n_ctx":8192}}`,
			interactive: false,
			wantErr:     true,
			wantOutput:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/props" {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write([]byte(tt.props))
			}))
			defer server.Close()
			u, err := url.Parse(server.URL)
			require.NoError(t, err)
			port, err := strconv.Atoi(u.Port())
			require.NoError(t, err)
			plan := testChatPlan()
			plan.ModelPath = modelPath
			plan.Runtime = chat.RuntimeSpec{Host: u.Hostname(), Port: port, ContextWindow: 8192}
			var out bytes.Buffer

			backend, err := acquireBackend(t.Context(), plan, time.Second, &out, strings.NewReader(tt.input), tt.interactive)

			require.Equal(t, tt.wantErr, err != nil)
			if tt.wantErr {
				require.Nil(t, backend)
				require.ErrorContains(t, err, "does not match this chat")
				require.ErrorContains(t, err, "it has /models/other.gguf loaded")
			} else {
				require.Equal(t, tt.wantAdopted, !backend.Launched())
			}
			for _, want := range tt.wantOutput {
				require.Contains(t, out.String(), want)
			}
			if !tt.interactive {
				require.NotContains(t, out.String(), "Connect anyway", "a script must never be asked a question")
			}
		})
	}
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
		// wantRest is what must be left on the stream afterwards: the chat loop
		// reads the same stdin next, so the prompt must consume only its answer.
		wantRest string
	}{
		{name: "y accepts", input: "y\nnext message\n", want: true, wantRest: "next message\n"},
		{name: "yes accepts", input: "yes\nnext message\n", want: true, wantRest: "next message\n"},
		{name: "case and spacing are ignored", input: "  Y  \nnext message\n", want: true, wantRest: "next message\n"},
		{name: "n declines", input: "n\nnext message\n", want: false, wantRest: "next message\n"},
		{name: "an empty answer takes the default", input: "\nnext message\n", want: false, wantRest: "next message\n"},
		{name: "anything else declines", input: "maybe\nnext message\n", want: false, wantRest: "next message\n"},
		{name: "EOF declines", input: "", want: false, wantRest: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			in := strings.NewReader(tt.input)

			got, err := confirm(t.Context(), &out, in, "Connect anyway?")

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Equal(t, "Connect anyway? [y/N]: ", out.String())
			rest, err := io.ReadAll(in)
			require.NoError(t, err)
			require.Equal(t, tt.wantRest, string(rest))
		})
	}
}
