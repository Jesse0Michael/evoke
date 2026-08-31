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
				Positive:    "test-prompt, test-appearance",
				Negative:    "test-prompt-negative, test-appearance-negative",
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
			result, _ := renderPromptData(tt.doc, KindImage, DefaultBase)

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
			data, _ := renderPromptData(tt.doc, KindImage, DefaultBase)
			base, raw, err := resolveBase(KindImage, DefaultBase)
			require.NoError(t, err)
			applyDefaults(&data, defaultsFor(base))

			payload, err := renderTemplate(data, tt.doc.Name, tt.doc.Sources, false, raw)
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
				SamplerName:   architectureDefaults["sdxl"].Sampler.SamplerName,
				SchedulerName: architectureDefaults["sdxl"].Sampler.Scheduler,
				Denoise:       architectureDefaults["sdxl"].Sampler.Denoise,
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
				ModelName:     architectureDefaults["sdxl"].Checkpoint,
				Steps:         architectureDefaults["sdxl"].Sampler.Steps,
				CFG:           architectureDefaults["sdxl"].Sampler.CFG,
				SamplerName:   architectureDefaults["sdxl"].Sampler.SamplerName,
				SchedulerName: architectureDefaults["sdxl"].Sampler.Scheduler,
				Denoise:       architectureDefaults["sdxl"].Sampler.Denoise,
				Width:         architectureDefaults["sdxl"].Sampler.Width,
				Height:        architectureDefaults["sdxl"].Sampler.Height,
				ClipSkip:      2,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := renderPromptData(tt.doc, KindImage, DefaultBase)
			base, raw, err := resolveBase(KindImage, DefaultBase)
			require.NoError(t, err)
			applyDefaults(&data, defaultsFor(base))

			payload, err := renderTemplate(data, tt.doc.Name, tt.doc.Sources, false, raw)
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

// fullComposition exercises every optional branch a workflow template has:
// a lora chain, the upscale pass, and all five detailers. It carries one LORA
// per architecture so exactly one resolves whichever base is rendered — an
// untagged LORA is an SDXL LORA and would leave the chain empty elsewhere.
func fullComposition() *evoke.Composition {
	det := func(arg, detector string) evoke.DetailerConfig {
		return evoke.DetailerConfig{
			Argument: arg,
			Settings: map[string]string{
				"detector": detector, "guide_size": "1024", "max_size": "1536",
				"steps": "20", "cfg": "4.0", "sampler_name": "euler", "scheduler": "simple",
				"denoise": "0.3", "feather": "10", "bbox_threshold": "0.25",
				"bbox_dilation": "10", "bbox_crop_factor": "2.0", "max_detection": "2",
			},
			Text: evoke.Prompt{Positive: []string{"test-detailer"}, Negative: []string{"test-detailer-negative"}},
		}
	}
	return &evoke.Composition{
		Name:        "Test Character",
		Sources:     []string{"/src/test-character.evoke"},
		Prompt:      evoke.Prompt{Positive: []string{"test-prompt"}, Negative: []string{"test-prompt-negative"}},
		Appearance:  evoke.Prompt{Positive: []string{`test-appearance "quoted"`}, Negative: []string{"test-appearance-negative"}},
		Apparel:     evoke.Prompt{Positive: []string{"test-apparel"}, Negative: []string{"test-apparel-negative"}},
		Environment: evoke.Prompt{Positive: []string{"test-environment"}, Negative: []string{"test-environment-negative"}},
		Loras: []evoke.LoraDefinition{
			{Argument: "test-lora-sdxl", Settings: map[string]string{"model": "test-lora-1.safetensors", "strength": "0.8", "clip": "0.7", "base": "sdxl"}},
			{Argument: "test-lora-anima", Settings: map[string]string{"model": "test-lora-2.safetensors", "strength": "0.6", "clip": "0.5", "base": "anima"}},
			{Argument: "test-lora-qwen", Settings: map[string]string{"model": "test-lora-3.safetensors", "strength": "1.0", "base": "qwen"}},
		},
		Images: []evoke.ImageStage{
			{Loras: []string{"test-lora-sdxl", "test-lora-anima"}, Settings: map[string]string{
				"shift": "3.1", "nag_scale": "5.0", "nag_alpha": "0.5", "nag_tau": "1.5",
			}},
			{Argument: "upscale", Settings: map[string]string{
				"upscale_model": "test-upscale.pth", "factor": "1.5", "steps": "10", "cfg": "4.0",
				"sampler_name": "euler", "scheduler": "simple", "denoise": "0.2",
				"tile_width": "1024", "tile_height": "1024",
			}},
			{Argument: "paint", Loras: []string{"test-lora-qwen"}, Settings: map[string]string{
				"unet": "test-paint-unet.safetensors", "clip": "test-paint-clip.safetensors",
				"vae": "test-paint-vae.safetensors", "shift": "3.0", "cfg_norm": "1.0",
				"steps": "4", "cfg": "1.0", "sampler_name": "euler", "scheduler": "simple", "denoise": "1.0",
			}, Text: evoke.Prompt{Positive: []string{"test-paint"}, Negative: []string{"test-paint-negative"}}},
			{Argument: "edit", Settings: map[string]string{
				"steps": "18", "cfg": "3.5", "sampler_name": "dpmpp_2m",
				"scheduler": "karras", "denoise": "0.45",
			}, Text: evoke.Prompt{Positive: []string{"test-edit"}, Negative: []string{"test-edit-negative"}}},
		},
		Detailers: []evoke.DetailerConfig{
			det("face", "bbox/face_yolov8m.pt"), det("eye", "bbox/Eyes.pt"),
			det("upper_body", "bbox/upper-body-v4.7.pt"), det("lower_body", "bbox/body-v4.2.pt"),
			det("hand", "bbox/hand.pt"),
		},
	}
}

func TestRenderTemplateProducesValidWorkflow(t *testing.T) {
	for _, kind := range allKinds {
		for _, name := range bases(kind) {
			t.Run(string(kind)+"/"+name, func(t *testing.T) {
				for _, debug := range []bool{false, true} {
					_, payload := renderFullWorkflow(t, kind, name, debug)

					// Every value must be a node: ComfyUI rejects a graph whose links
					// name a node the template did not emit.
					var graph map[string]struct {
						ClassType string                     `json:"class_type"`
						Inputs    map[string]json.RawMessage `json:"inputs"`
					}
					require.NoError(t, json.Unmarshal([]byte(payload), &graph), "debug=%v: invalid JSON", debug)
					require.NotEmpty(t, graph)

					for id, node := range graph {
						require.NotEmpty(t, node.ClassType, "node %q has no class_type", id)
						for field, rawVal := range node.Inputs {
							var link []json.RawMessage
							if json.Unmarshal(rawVal, &link) != nil || len(link) != 2 {
								continue
							}
							var target string
							if json.Unmarshal(link[0], &target) != nil {
								continue
							}
							require.Contains(t, graph, target, "node %q input %q links to missing node", id, field)
						}
					}
				}
			})
		}
	}
}

func TestResolveBase(t *testing.T) {
	tests := []struct {
		name      string
		kind      Kind
		base      string
		expected  string
		wantError bool
	}{
		{name: "empty selects the default", kind: KindImage, base: "", expected: DefaultBase},
		{name: "a known base resolves", kind: KindImage, base: "anima", expected: "anima"},
		{name: "an unknown base is rejected", kind: KindImage, base: "test-missing", wantError: true},
		{name: "a path is rejected", kind: KindImage, base: "../image/sdxl", wantError: true},
		// The kinds are separate template families keyed by the same names, so
		// a base is only resolvable within the kind that has a template for it.
		{name: "a known base resolves for edit", kind: KindEdit, base: "anima", expected: "anima"},
		// paint is a family of one and no composition names it, so the base it
		// renders is fixed rather than selected.
		{name: "the paint base resolves", kind: KindPaint, base: PaintBase, expected: PaintBase},
		{name: "an image base is not a paint base", kind: KindPaint, base: "sdxl", wantError: true},
		{name: "a paint base is not an image base", kind: KindImage, base: PaintBase, wantError: true},
		{name: "an unknown kind resolves nothing", kind: Kind("test-missing"), base: "sdxl", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, raw, err := resolveBase(tt.kind, tt.base)

			require.Equal(t, tt.wantError, err != nil)
			require.Equal(t, tt.expected, got)
			require.Equal(t, tt.wantError, raw == nil)
		})
	}
}

func TestCompositionBase(t *testing.T) {
	tests := []struct {
		name     string
		doc      *evoke.Composition
		expected string
	}{
		{
			name:     "no IMAGE stage falls through to the default",
			doc:      &evoke.Composition{},
			expected: "",
		},
		{
			name:     "an IMAGE stage without a base falls through to the default",
			doc:      &evoke.Composition{Images: []evoke.ImageStage{{Settings: map[string]string{"checkpoint": "test-checkpoint.safetensors"}}}},
			expected: "",
		},
		{
			name:     "the unnamed IMAGE stage supplies the base",
			doc:      &evoke.Composition{Images: []evoke.ImageStage{{Settings: map[string]string{"base": "anima"}}}},
			expected: "anima",
		},
		{
			name:     "case and whitespace are not significant",
			doc:      &evoke.Composition{Images: []evoke.ImageStage{{Settings: map[string]string{"base": "  Anima  "}}}},
			expected: "anima",
		},
		{
			name:     "a base on the upscale stage is ignored",
			doc:      &evoke.Composition{Images: []evoke.ImageStage{{Argument: "upscale", Settings: map[string]string{"base": "anima"}}}},
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, compositionBase(tt.doc))
		})
	}
}

func TestResolveLorasBaseGuard(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		expected []lora
		skipped  []string
	}{
		{
			// An untagged LORA is an SDXL LORA: base means the same thing here
			// as it does on IMAGE, so leaving it out selects DefaultBase rather
			// than opting out of the guard.
			name: "the default base loads untagged weights",
			base: "sdxl",
			expected: []lora{
				{Name: "untagged.safetensors", Strength: 0.4, Clip: 1.0},
				{Name: "sdxl.safetensors", Strength: 1.0, Clip: 1.0},
			},
			skipped: []string{"test-lora-anima"},
		},
		{
			name:     "another base skips untagged weights with the rest",
			base:     "anima",
			expected: []lora{{Name: "anima.safetensors", Strength: 1.0, Clip: 1.0}},
			skipped:  []string{"test-lora-untagged", "test-lora-sdxl"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := &evoke.Composition{
				Images: []evoke.ImageStage{{
					Settings: map[string]string{"base": tt.base},
					Loras:    []string{"test-lora-untagged", "test-lora-sdxl", "test-lora-anima", "test-lora-disabled"},
				}},
				Loras: []evoke.LoraDefinition{
					{Argument: "test-lora-untagged", Settings: map[string]string{"model": "untagged.safetensors", "strength": "0.4"}},
					{Argument: "test-lora-sdxl", Settings: map[string]string{"model": "sdxl.safetensors", "base": "sdxl"}},
					{Argument: "test-lora-anima", Settings: map[string]string{"model": "anima.safetensors", "base": "Anima"}},
					{Argument: "test-lora-disabled", Settings: map[string]string{"model": "disabled.safetensors"}, Disabled: true},
				},
			}

			var pd promptData
			skipped := resolveLoras(doc, KindImage, tt.base, &pd)

			require.Equal(t, tt.expected, pd.Loras)
			require.Equal(t, tt.skipped, skipped)
		})
	}
}

func TestResolveLorasReportsOnlyReferencedSkips(t *testing.T) {
	// A definition no stage references was never going to load, so a base
	// mismatch on it is not something the caller needs told about.
	doc := &evoke.Composition{
		Images: []evoke.ImageStage{{
			Settings: map[string]string{"base": "anima"},
			Loras:    []string{"test-lora-referenced"},
		}},
		Loras: []evoke.LoraDefinition{
			{Argument: "test-lora-referenced", Settings: map[string]string{"model": "referenced.safetensors", "base": "sdxl"}},
			{Argument: "test-lora-orphan", Settings: map[string]string{"model": "orphan.safetensors", "base": "sdxl"}},
		},
	}

	var pd promptData
	skipped := resolveLoras(doc, KindImage, "anima", &pd)

	require.Empty(t, pd.Loras)
	require.Equal(t, []string{"test-lora-referenced"}, skipped)
}

func TestDefaultsForMatchesEveryBase(t *testing.T) {
	// A base with no defaults entry silently inherits SDXL's sampler, which is
	// wrong for any other architecture.
	for _, kind := range allKinds {
		for _, name := range bases(kind) {
			require.Contains(t, architectureDefaults, name, "kind %s", kind)
		}
	}
}

// TestPaintNeedsNoStage covers a composition with no IMAGE paint block at all.
// The architecture defaults already carry every model setting, so the only
// thing such a composition contributes is the instruction — and nesting that
// inside the stage check painted an empty prompt instead.
func TestPaintNeedsNoStage(t *testing.T) {
	doc := &evoke.Composition{
		Prompt:     evoke.Prompt{Positive: []string{"test-instruction"}},
		Appearance: evoke.Prompt{Positive: []string{"test-appearance"}},
	}

	data, _ := renderPromptData(doc, KindPaint, PaintBase)
	applyDefaults(&data, defaultsFor(PaintBase))

	require.Equal(t, "test-instruction", data.Paint.Positive)
	require.NotContains(t, data.Paint.Positive, "test-appearance")
	require.Equal(t, architectureDefaults[PaintBase].Paint.Unet, data.Paint.Unet)
	require.Equal(t, architectureDefaults[PaintBase].Paint.Steps, data.Paint.Steps)
}
