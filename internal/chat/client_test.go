package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// deltaEvent formats a single SSE chat-completion chunk carrying content.
func deltaEvent(content string) string {
	payload, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{"delta": map[string]string{"content": content}}},
	})
	return fmt.Sprintf("data: %s\n\n", payload)
}

// sseServer returns a test server that streams the given chunks as SSE events.
func sseServer(t *testing.T, chunks []string, captured *wireRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			body, _ := io.ReadAll(r.Body)
			require.NoError(t, json.Unmarshal(body, captured))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprint(w, deltaEvent(c))
			if flusher != nil {
				flusher.Flush()
			}
		}
		// Final usage chunk (as an OpenAI-compatible backend sends with
		// stream_options.include_usage).
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":3}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func sampleRequest() CompletionRequest {
	temp := 0.85
	return CompletionRequest{
		Model:    "roleplay-12b",
		Messages: []Message{{Role: RoleSystem, Content: "be nice"}, {Role: RoleUser, Content: "hi"}},
		Sampling: Sampling{Temperature: &temp, MaxOutputTokens: 128},
	}
}

func TestClientStream(t *testing.T) {
	var captured wireRequest
	srv := sseServer(t, []string{"Hello", ", ", "world"}, &captured)
	defer srv.Close()

	client := NewClient(srv.URL, "")
	var deltas []string
	full, usage, err := client.Stream(t.Context(), sampleRequest(), func(d string) {
		deltas = append(deltas, d)
	})

	require.NoError(t, err)
	require.Equal(t, "Hello, world", full)
	require.Equal(t, []string{"Hello", ", ", "world"}, deltas)
	require.Equal(t, Usage{PromptTokens: 11, CompletionTokens: 3}, usage)

	// Request is encoded correctly, including the usage opt-in.
	require.True(t, captured.Stream)
	require.NotNil(t, captured.StreamOptions)
	require.True(t, captured.StreamOptions.IncludeUsage)
	require.Equal(t, "roleplay-12b", captured.Model)
	require.Equal(t, 128, captured.MaxTokens)
	require.NotNil(t, captured.Temperature)
	require.InEpsilon(t, 0.85, *captured.Temperature, 1e-9)
	require.Equal(t, []wireMessage{{Role: "system", Content: "be nice"}, {Role: "user", Content: "hi"}}, captured.Messages)
}

func TestClientComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "full reply"}}},
			"usage":   map[string]int{"prompt_tokens": 42, "completion_tokens": 7},
		})
	}))
	defer srv.Close()

	reply, usage, err := NewClient(srv.URL, "").Complete(t.Context(), sampleRequest())

	require.NoError(t, err)
	require.Equal(t, "full reply", reply)
	require.Equal(t, Usage{PromptTokens: 42, CompletionTokens: 7}, usage)
}

func TestClientMalformedEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {not json}\n\n")
	}))
	defer srv.Close()

	_, _, err := NewClient(srv.URL, "").Stream(t.Context(), sampleRequest(), nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "malformed stream event")
}

func TestClientHTTPErrorIsBoundedAndSecretFree(t *testing.T) {
	big := strings.Repeat("E", 100<<10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, big)
	}))
	defer srv.Close()

	_, _, err := NewClient(srv.URL, "super-secret-key").Complete(t.Context(), sampleRequest())

	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
	require.NotContains(t, err.Error(), "super-secret-key", "credentials must not appear in errors")
	require.Less(t, len(err.Error()), maxErrorBody+256, "error body must be bounded")
}

func TestClientStreamCancellation(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, deltaEvent("partial"))
		if flusher != nil {
			flusher.Flush()
		}
		close(started)
		<-r.Context().Done() // hold the stream open until the client cancels
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan struct{}, 1)
	errCh := make(chan error, 1)
	go func() {
		_, _, err := NewClient(srv.URL, "").Stream(ctx, sampleRequest(), func(string) {
			select {
			case first <- struct{}{}:
			default:
			}
		})
		errCh <- err
	}()

	<-started
	<-first
	cancel()

	err := <-errCh
	require.ErrorIs(t, err, context.Canceled)
}

// A reasoning model spends its output budget on a separate channel, so an
// answerless response must explain itself rather than reach the user as a blank
// turn. Both transports share the rule.
func TestEmptyReply(t *testing.T) {
	tests := []struct {
		name    string
		stream  bool
		body    string
		wantErr string
	}{
		{
			name:    "streamed reasoning that never answers",
			stream:  true,
			body:    "data: {\"choices\":[{\"delta\":{\"reasoning\":\"thinking\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n",
			wantErr: "produced only reasoning",
		},
		{
			name:    "streamed truncation with no reasoning channel",
			stream:  true,
			body:    "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n",
			wantErr: "hit the output limit before producing any content",
		},
		{
			name:    "complete reasoning that never answers",
			body:    `{"choices":[{"message":{"role":"assistant","reasoning":"thinking"},"finish_reason":"length"}]}`,
			wantErr: "produced only reasoning",
		},
		{
			name:    "complete empty reply for another reason",
			body:    `{"choices":[{"message":{"role":"assistant"},"finish_reason":"stop"}]}`,
			wantErr: `empty reply (finish reason "stop")`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			c := NewClient(srv.URL+"/v1", "")

			var err error
			if tt.stream {
				_, _, err = c.Stream(t.Context(), sampleRequest(), nil)
			} else {
				_, _, err = c.Complete(t.Context(), sampleRequest())
			}

			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// Thinking is toggled through the chat template rather than a sampling field,
// and only when the declaration asked for it.
func TestThinkingOnTheWire(t *testing.T) {
	off := false
	tests := []struct {
		name     string
		thinking *bool
		want     map[string]any
	}{
		{name: "unset sends nothing", thinking: nil, want: nil},
		{name: "off disables thinking", thinking: &off, want: map[string]any{"enable_thinking": false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := sampleRequest()
			cr.Sampling.Thinking = tt.thinking

			require.Equal(t, tt.want, toWireRequest(cr, false).ChatTemplateKwargs)
		})
	}
}

// The repetition penalty travels under both runtimes' spellings, so the same
// CHAT setting reaches llama.cpp (repeat_penalty) and mlx_lm
// (repetition_penalty) alike.
func TestRepeatPenaltyOnTheWire(t *testing.T) {
	penalty := 1.15
	tests := []struct {
		name    string
		penalty *float64
		want    map[string]any
	}{
		{name: "unset sends neither key", penalty: nil, want: map[string]any{}},
		{
			name:    "set sends both keys",
			penalty: &penalty,
			want:    map[string]any{"repeat_penalty": 1.15, "repetition_penalty": 1.15},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := sampleRequest()
			cr.Sampling.RepeatPenalty = tt.penalty

			body, err := json.Marshal(toWireRequest(cr, false))
			require.NoError(t, err)

			var decoded map[string]any
			require.NoError(t, json.Unmarshal(body, &decoded))
			got := map[string]any{}
			for _, key := range []string{"repeat_penalty", "repetition_penalty"} {
				if v, ok := decoded[key]; ok {
					got[key] = v
				}
			}

			require.Equal(t, tt.want, got)
		})
	}
}
