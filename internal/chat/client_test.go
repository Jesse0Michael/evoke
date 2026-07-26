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
