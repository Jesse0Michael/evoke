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

// saveMetadata is the subset of "Image Saver" inputs that Civitai reads back.
type saveMetadata struct {
	Positive      string  `json:"positive"`
	Negative      string  `json:"negative"`
	ModelName     string  `json:"modelname"`
	Seed          uint64  `json:"seed_value"`
	Steps         int     `json:"steps"`
	CFG           float64 `json:"cfg"`
	SamplerName   string  `json:"sampler_name"`
	SchedulerName string  `json:"scheduler_name"`
	Denoise       float64 `json:"denoise"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	ClipSkip      int     `json:"clip_skip"`
}

type savePath struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
}

func TestRenderTemplateOutputDir(t *testing.T) {
	tests := []struct {
		name     string
		doc      *evoke.Composition
		expected savePath
	}{
		{
			name:     "no NAME and no sources falls back to evoke for both segments",
			doc:      &evoke.Composition{},
			expected: savePath{Path: "images/evoke", Filename: "evoke"},
		},
		{
			name:     "NAME is the output directory and sources are the filename",
			doc:      &evoke.Composition{Name: "Test Character", Sources: []string{"/src/test-character.evoke", "/src/portrait.evoke"}},
			expected: savePath{Path: "images/test_character", Filename: "test_character_portrait"},
		},
		{
			name: "an IMAGE group shelves the character directory under it",
			doc: &evoke.Composition{
				Name:    "Test Character",
				Sources: []string{"/src/test-character.evoke"},
				Images:  []evoke.ImageStage{{Settings: map[string]string{"group": "Test Cast"}}},
			},
			expected: savePath{Path: "images/test_cast/test_character", Filename: "test_character"},
		},
		{
			name: "a nested group keeps its separators",
			doc: &evoke.Composition{
				Name:    "test-character",
				Sources: []string{"/src/test-character.evoke"},
				Images:  []evoke.ImageStage{{Settings: map[string]string{"group": "tests/noir"}}},
			},
			expected: savePath{Path: "images/tests/noir/test_character", Filename: "test_character"},
		},
		{
			name: "a group that normalizes to nothing is ignored",
			doc: &evoke.Composition{
				Name:    "test-character",
				Sources: []string{"/src/test-character.evoke"},
				Images:  []evoke.ImageStage{{Settings: map[string]string{"group": "/../.."}}},
			},
			expected: savePath{Path: "images/test_character", Filename: "test_character"},
		},
		{
			name: "a group on a disabled IMAGE stage is not applied",
			doc: &evoke.Composition{
				Name:    "test-character",
				Sources: []string{"/src/test-character.evoke"},
				Images:  []evoke.ImageStage{{Disabled: true, Settings: map[string]string{"group": "test-cast"}}},
			},
			expected: savePath{Path: "images/test_character", Filename: "test_character"},
		},
		{
			name: "a group on the upscale stage does not affect the output directory",
			doc: &evoke.Composition{
				Name:    "test-character",
				Sources: []string{"/src/test-character.evoke"},
				Images:  []evoke.ImageStage{{Argument: "upscale", Settings: map[string]string{"group": "test-cast"}}},
			},
			expected: savePath{Path: "images/test_character", Filename: "test_character"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := renderPromptData(tt.doc)
			applyDefaults(&data)

			payload, err := renderTemplate(data, tt.doc.Name, tt.doc.Sources, false)
			require.NoError(t, err)

			var workflow map[string]struct {
				Inputs savePath `json:"inputs"`
			}
			require.NoError(t, json.Unmarshal(payload, &workflow))
			require.Equal(t, tt.expected, workflow["save"].Inputs)
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

func TestRenderTemplateCivitaiMetadata(t *testing.T) {
	tests := []struct {
		name     string
		doc      *evoke.Composition
		expected saveMetadata
	}{
		{
			name: "sampler settings and checkpoint are recorded for civitai",
			doc: &evoke.Composition{
				Appearance: evoke.Prompt{Positive: []string{"test-appearance"}},
				Images: []evoke.ImageStage{{Settings: map[string]string{
					"checkpoint": "test-checkpoint.safetensors",
					"steps":      "30",
					"cfg":        "5",
					"width":      "832",
					"height":     "1216",
				}}},
			},
			expected: saveMetadata{
				Positive:      "test-appearance, , ",
				Negative:      ", , ",
				ModelName:     "test-checkpoint.safetensors",
				Steps:         30,
				CFG:           5,
				SamplerName:   defaultSamplerName,
				SchedulerName: defaultScheduler,
				Denoise:       defaultDenoise,
				Width:         832,
				Height:        1216,
				ClipSkip:      2,
			},
		},
		{
			name: "loras are appended as civitai tags so the saver can hash them",
			doc: &evoke.Composition{
				Appearance: evoke.Prompt{Positive: []string{"test-appearance"}},
				Loras: []evoke.LoraDefinition{
					{Argument: "test-lora", Settings: map[string]string{"model": "test-lora-1.safetensors", "strength": "0.8"}},
				},
				Images: []evoke.ImageStage{{Loras: []string{"test-lora"}}},
			},
			expected: saveMetadata{
				Positive:      "test-appearance, ,  <lora:test-lora-1:0.8>",
				Negative:      ", , ",
				ModelName:     defaultCheckpoint,
				Steps:         defaultSteps,
				CFG:           defaultCFG,
				SamplerName:   defaultSamplerName,
				SchedulerName: defaultScheduler,
				Denoise:       defaultDenoise,
				Width:         defaultWidth,
				Height:        defaultHeight,
				ClipSkip:      2,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := renderPromptData(tt.doc)
			applyDefaults(&data)

			payload, err := renderTemplate(data, tt.doc.Name, tt.doc.Sources, false)
			require.NoError(t, err)

			var workflow map[string]struct {
				ClassType string          `json:"class_type"`
				Inputs    json.RawMessage `json:"inputs"`
			}
			require.NoError(t, json.Unmarshal(payload, &workflow))
			require.Equal(t, "Image Saver", workflow["save"].ClassType)

			var got saveMetadata
			require.NoError(t, json.Unmarshal(workflow["save"].Inputs, &got))

			var sampler struct {
				Seed uint64 `json:"seed"`
			}
			require.NoError(t, json.Unmarshal(workflow["sampler"].Inputs, &sampler))

			var encoded struct {
				Text string `json:"text"`
			}
			require.NoError(t, json.Unmarshal(workflow["prompt_pos"].Inputs, &encoded))

			// The seed recorded in the metadata must be the one the sampler used,
			// or the image cannot be reproduced from what Civitai displays.
			require.Equal(t, sampler.Seed, got.Seed)

			// <lora:> tags are metadata-only and must never reach the encoded prompt.
			require.NotContains(t, encoded.Text, "<lora:")

			got.Seed = 0
			require.Equal(t, tt.expected, got)
		})
	}
}
