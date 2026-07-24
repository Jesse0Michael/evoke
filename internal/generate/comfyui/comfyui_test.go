package comfyui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jesse0michael/evoke/internal/generate"
	"github.com/stretchr/testify/require"
)

func TestClient_Queue(t *testing.T) {
	tests := []struct {
		name            string
		response        string
		statusCode      int
		expectedRunning []generate.QueueItem
		expectedPending []generate.QueueItem
		wantError       bool
	}{
		{
			name:       "empty queue",
			response:   `{"queue_running": [], "queue_pending": []}`,
			statusCode: http.StatusOK,
		},
		{
			name:       "items in queue",
			response:   `{"queue_running": [[5, "test-prompt-running", {}, {}, []]], "queue_pending": [[6, "test-prompt-pending", {}, {}, []], [7, "test-prompt-pending-2", {}, {}, []]]}`,
			statusCode: http.StatusOK,
			expectedRunning: []generate.QueueItem{
				{PromptID: "test-prompt-running", Number: 5},
			},
			expectedPending: []generate.QueueItem{
				{PromptID: "test-prompt-pending", Number: 6},
				{PromptID: "test-prompt-pending-2", Number: 7},
			},
		},
		{
			name:       "server error",
			response:   "internal error",
			statusCode: http.StatusInternalServerError,
			wantError:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/queue", r.URL.Path)
				require.Equal(t, "GET", r.Method)
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer srv.Close()

			client := New(srv.URL)
			running, pending, err := client.Queue(t.Context())

			require.Equal(t, tt.wantError, err != nil)
			if !tt.wantError {
				require.Equal(t, tt.expectedRunning, running)
				require.Equal(t, tt.expectedPending, pending)
			}
		})
	}
}

func TestClient_ClearQueue(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantError  bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			wantError:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/queue", r.URL.Path)
				require.Equal(t, "POST", r.Method)

				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, true, body["clear"])

				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			client := New(srv.URL)
			err := client.ClearQueue(t.Context())

			require.Equal(t, tt.wantError, err != nil)
		})
	}
}

func TestClient_ResolveOutputs(t *testing.T) {
	tests := []struct {
		name     string
		promptID string
		response string
		expected []generate.Output
	}{
		{
			name:     "completed with outputs",
			promptID: "test-prompt-1",
			response: `{"test-prompt-1": {"outputs": {"9": {"images": [{"filename": "evoke_00001_.png", "subfolder": "", "type": "output"}]}}}}`,
			expected: []generate.Output{
				{Filename: "evoke_00001_.png", Subfolder: "", Type: "output"},
			},
		},
		{
			name:     "multiple outputs",
			promptID: "test-prompt-2",
			response: `{"test-prompt-2": {"outputs": {"9": {"images": [{"filename": "img1.png", "subfolder": "batch", "type": "output"}, {"filename": "img2.png", "subfolder": "batch", "type": "output"}]}}}}`,
			expected: []generate.Output{
				{Filename: "img1.png", Subfolder: "batch", Type: "output"},
				{Filename: "img2.png", Subfolder: "batch", Type: "output"},
			},
		},
		{
			name:     "not found",
			promptID: "test-prompt-missing",
			response: `{}`,
			expected: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/history/"+tt.promptID, r.URL.Path)
				require.Equal(t, "GET", r.Method)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer srv.Close()

			client := New(srv.URL)
			outputs, err := client.ResolveOutputs(context.Background(), tt.promptID)

			require.NoError(t, err)
			require.Equal(t, tt.expected, outputs)
		})
	}
}
