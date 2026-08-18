package comfyui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jesse0michael/evoke/internal/generate"
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
	"github.com/stretchr/testify/require"
)

func TestRenderPromptData(t *testing.T) {
	tests := []struct {
		name     string
		doc      *evoke.Composition
		expected promptData
	}{
		{
			name: "declarations no diffusion model can render are excluded from the image prompt",
			doc: &evoke.Composition{
				Name:        "test-character",
				Character:   []string{"test-character-description"},
				Personality: evoke.Prompt{Positive: []string{"test-personality"}, Negative: []string{"test-personality-negative"}},
				Backstory:   []string{"test-backstory"},
				Scenario:    "test-scenario",
				Voice:       evoke.Prompt{Positive: []string{"test-voice"}, Negative: []string{"test-voice-negative"}},
				Appearance:  evoke.Prompt{Positive: []string{"test-appearance"}, Negative: []string{"test-appearance-negative"}},
				Apparel:     evoke.Prompt{Positive: []string{"test-apparel"}, Negative: []string{"test-apparel-negative"}},
				Environment: evoke.Prompt{Positive: []string{"test-environment"}, Negative: []string{"test-environment-negative"}},
				Prompt:      evoke.Prompt{Positive: []string{"test-prompt"}, Negative: []string{"test-prompt-negative"}},
			},
			expected: promptData{
				Positive:    "test-appearance, test-prompt",
				Negative:    "test-appearance-negative, test-prompt-negative",
				Apparel:     prompt{Positive: "test-apparel", Negative: "test-apparel-negative"},
				Environment: prompt{Positive: "test-environment", Negative: "test-environment-negative"},
			},
		},
		{
			name: "a chat-only composition renders an empty image prompt",
			doc: &evoke.Composition{
				Character:   []string{"test-character-description"},
				Personality: evoke.Prompt{Positive: []string{"test-personality"}},
				Backstory:   []string{"test-backstory"},
				Scenario:    "test-scenario",
			},
			expected: promptData{},
		},
		{
			name: "IMAGE text leads the prompt",
			doc: &evoke.Composition{
				Character:  []string{"test-character-description"},
				Backstory:  []string{"test-backstory"},
				Appearance: evoke.Prompt{Positive: []string{"test-appearance"}},
				Images: []evoke.ImageStage{{
					Text: evoke.Prompt{Positive: []string{"test-quality"}, Negative: []string{"test-quality-negative"}},
				}},
			},
			expected: promptData{
				Positive: "test-quality, test-appearance",
				Negative: "test-quality-negative",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderPromptData(tt.doc)

			require.Equal(t, tt.expected, result)
		})
	}
}

func TestRenderTemplateOutputDir(t *testing.T) {
	tests := []struct {
		name     string
		doc      *evoke.Composition
		expected string
	}{
		{
			name:     "no NAME and no sources falls back to evoke for both segments",
			doc:      &evoke.Composition{},
			expected: "images/evoke/evoke",
		},
		{
			name:     "NAME is the output directory and sources are the filename",
			doc:      &evoke.Composition{Name: "Test Character", Sources: []string{"/src/test-character.evoke", "/src/portrait.evoke"}},
			expected: "images/test_character/test_character_portrait",
		},
		{
			name: "an IMAGE group shelves the character directory under it",
			doc: &evoke.Composition{
				Name:    "Test Character",
				Sources: []string{"/src/test-character.evoke"},
				Images:  []evoke.ImageStage{{Settings: map[string]string{"group": "Test Cast"}}},
			},
			expected: "images/test_cast/test_character/test_character",
		},
		{
			name: "a nested group keeps its separators",
			doc: &evoke.Composition{
				Name:    "test-character",
				Sources: []string{"/src/test-character.evoke"},
				Images:  []evoke.ImageStage{{Settings: map[string]string{"group": "tests/noir"}}},
			},
			expected: "images/tests/noir/test_character/test_character",
		},
		{
			name: "a group that normalizes to nothing is ignored",
			doc: &evoke.Composition{
				Name:    "test-character",
				Sources: []string{"/src/test-character.evoke"},
				Images:  []evoke.ImageStage{{Settings: map[string]string{"group": "/../.."}}},
			},
			expected: "images/test_character/test_character",
		},
		{
			name: "a group on a disabled IMAGE stage is not applied",
			doc: &evoke.Composition{
				Name:    "test-character",
				Sources: []string{"/src/test-character.evoke"},
				Images:  []evoke.ImageStage{{Disabled: true, Settings: map[string]string{"group": "test-cast"}}},
			},
			expected: "images/test_character/test_character",
		},
		{
			name: "a group on the upscale stage does not affect the output directory",
			doc: &evoke.Composition{
				Name:    "test-character",
				Sources: []string{"/src/test-character.evoke"},
				Images:  []evoke.ImageStage{{Argument: "upscale", Settings: map[string]string{"group": "test-cast"}}},
			},
			expected: "images/test_character/test_character",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := renderPromptData(tt.doc)
			applyDefaults(&data)

			payload, err := renderTemplate(data, tt.doc.Name, tt.doc.Sources, false)
			require.NoError(t, err)

			var workflow map[string]struct {
				Inputs struct {
					FilenamePrefix string `json:"filename_prefix"`
				} `json:"inputs"`
			}
			require.NoError(t, json.Unmarshal(payload, &workflow))
			require.Equal(t, tt.expected, workflow["save"].Inputs.FilenamePrefix)
		})
	}
}

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
