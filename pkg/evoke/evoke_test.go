package evoke_test

import (
	"testing"

	evoke "github.com/jesse0michael/evoke/pkg/evoke"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		expected  []*evoke.Declaration
		wantError bool
	}{
		{
			name: "single declaration with values",
			src:  "NAME\n    Sumi\n",
			expected: []*evoke.Declaration{
				{Name: "NAME", RawName: "NAME", Line: 1, Values: []string{"Sumi"}},
			},
		},
		{
			name: "accumulating values preserve order",
			src:  "APPEARANCE\n    small\n    round\n    violet skin\n",
			expected: []*evoke.Declaration{
				{Name: "APPEARANCE", RawName: "APPEARANCE", Line: 1, Values: []string{"small", "round", "violet skin"}},
			},
		},
		{
			name: "negative prefix",
			src:  "!APPAREL\n    sandals\n",
			expected: []*evoke.Declaration{
				{Name: "APPAREL", RawName: "APPAREL", Negative: true, Line: 1, Values: []string{"sandals"}},
			},
		},
		{
			name: "default prefix",
			src:  "?APPAREL\n    green shirt\n",
			expected: []*evoke.Declaration{
				{Name: "APPAREL", RawName: "APPAREL", Default: true, Line: 1, Values: []string{"green shirt"}},
			},
		},
		{
			name: "IDENTITY is migrated to CHARACTER",
			src:  "IDENTITY\n    a nurse\n",
			expected: []*evoke.Declaration{
				{Name: "CHARACTER", RawName: "IDENTITY", Line: 1, Values: []string{"a nurse"}},
			},
		},
		{
			name:      "empty declaration block is invalid",
			src:       "NAME\n\nAPPEARANCE\n    violet skin\n",
			wantError: true,
		},
		{
			name:     "empty document is valid",
			src:      "",
			expected: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := evoke.Parse([]byte(tt.src))

			require.Equal(t, tt.wantError, err != nil, "error mismatch: %v", err)
			if !tt.wantError {
				require.Equal(t, tt.expected, doc.Declarations)
			}
		})
	}
}

func TestParse_Tags(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantTags  []string
		wantError bool
	}{
		{
			name:     "basic tags",
			src:      "TAGS\n    nurse\n    medical\n",
			wantTags: []string{"nurse", "medical"},
		},
		{
			name:     "normalized to lowercase and deduped",
			src:      "TAGS\n    Nurse\n    nurse\n    MEDICAL\n",
			wantTags: []string{"nurse", "medical"},
		},
		{
			name:     "comma separated tags",
			src:      "TAGS\n    pipeline, realistic\n",
			wantTags: []string{"pipeline", "realistic"},
		},
		{
			name:     "comma separated tags no spaces",
			src:      "TAGS\n    nurse,medical\n",
			wantTags: []string{"nurse", "medical"},
		},
		{
			name:     "mixed comma and newline tags",
			src:      "TAGS\n    nurse, medical\n    fantasy\n",
			wantTags: []string{"nurse", "medical", "fantasy"},
		},
		{
			name:     "tags with special characters",
			src:      "TAGS\n    nurse outfit\n    sci:fi\n    rock+roll\n",
			wantTags: []string{"nurse outfit", "sci:fi", "rock+roll"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := evoke.Parse([]byte(tt.src))

			require.Equal(t, tt.wantError, err != nil, "error mismatch: %v", err)
			if !tt.wantError {
				require.Equal(t, tt.wantTags, doc.Metadata.Tags)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantError bool
	}{
		{name: "known declarations are valid", src: "NAME\n    Sumi\n"},
		{name: "negative on supported declaration", src: "!APPEARANCE\n    tall\n"},
		{name: "unknown declaration is invalid", src: "LOCATION\n    forest\n", wantError: true},
		{name: "negative on NAME is invalid", src: "!NAME\n    Sumi\n", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, parseErr := evoke.Parse([]byte(tt.src))
			require.NoError(t, parseErr)

			err := evoke.Validate(doc)

			require.Equal(t, tt.wantError, err != nil, "error mismatch: %v", err)
		})
	}
}

func TestParseSelector(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		expected  evoke.Selector
		wantError bool
	}{
		{name: "tag only", raw: "nurse", expected: evoke.Selector{Tags: []string{"nurse"}, Raw: "nurse"}},
		{name: "multi-tag", raw: "nurse+modern", expected: evoke.Selector{Tags: []string{"nurse", "modern"}, Raw: "nurse+modern"}},
		{name: "colon treated as tag", raw: "c:nurse", expected: evoke.Selector{Tags: []string{"c:nurse"}, Raw: "c:nurse"}},
		{name: "empty", raw: "", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel, err := evoke.ParseSelector(tt.raw)

			require.Equal(t, tt.wantError, err != nil, "error mismatch: %v", err)
			if !tt.wantError {
				require.Equal(t, tt.expected, sel)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	tests := []struct {
		name string
		docs []*evoke.Document
		want evoke.Composition
	}{
		{
			name: "single file",
			docs: []*evoke.Document{{Declarations: []*evoke.Declaration{
				{Name: "NAME", Values: []string{"Sumi"}},
				{Name: "CHARACTER", Values: []string{"octopus humanoid"}},
				{Name: "APPEARANCE", Values: []string{"violet skin", "large eyes"}},
			}}},
			want: evoke.Composition{
				Name:       "Sumi",
				Character:  []string{"octopus humanoid"},
				Appearance: evoke.Prompt{Positive: []string{"violet skin", "large eyes"}},
			},
		},
		{
			name: "explicit suppresses default",
			docs: []*evoke.Document{
				{Declarations: []*evoke.Declaration{{Name: "APPAREL", Default: true, Values: []string{"casual"}}}},
				{Declarations: []*evoke.Declaration{{Name: "APPAREL", Values: []string{"scrubs"}}}},
			},
			want: evoke.Composition{Apparel: evoke.Prompt{Positive: []string{"scrubs"}}},
		},
		{
			name: "negative channel",
			docs: []*evoke.Document{{Declarations: []*evoke.Declaration{
				{Name: "APPEARANCE", Values: []string{"violet skin"}},
				{Name: "APPEARANCE", Negative: true, Values: []string{"scary"}},
			}}},
			want: evoke.Composition{Appearance: evoke.Prompt{Positive: []string{"violet skin"}, Negative: []string{"scary"}}},
		},
		{
			name: "deduplicates across files",
			docs: []*evoke.Document{
				{Declarations: []*evoke.Declaration{{Name: "APPEARANCE", Values: []string{"dark hair", "tall"}}}},
				{Declarations: []*evoke.Declaration{{Name: "APPEARANCE", Values: []string{"dark hair", "brown eyes"}}}},
			},
			want: evoke.Composition{Appearance: evoke.Prompt{Positive: []string{"dark hair", "tall", "brown eyes"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evoke.Merge(tt.docs)

			require.Equal(t, tt.want, *got)
		})
	}
}

func TestSelect(t *testing.T) {
	candidates := []evoke.SourceDocument{
		{
			Path: "gwen.evoke",
			Document: &evoke.Document{
				Metadata:     evoke.Metadata{Tags: []string{"character", "nurse"}},
				Declarations: []*evoke.Declaration{{Name: "CHARACTER", Values: []string{"Gwen"}}},
			},
		},
		{
			Path: "scrubs.evoke",
			Document: &evoke.Document{
				Metadata:     evoke.Metadata{Tags: []string{"nurse", "medical", "apparel"}},
				Declarations: []*evoke.Declaration{{Name: "APPAREL", Values: []string{"scrubs"}}},
			},
		},
	}

	selections, err := evoke.Select(candidates, []evoke.Selector{
		{Tags: []string{"nurse", "character"}},
		{Tags: []string{"nurse", "apparel"}},
	}, 0)

	require.NoError(t, err)
	require.Len(t, selections, 2)
	require.Equal(t, "gwen.evoke", selections[0].Source.Path)
	require.Equal(t, "scrubs.evoke", selections[1].Source.Path)
}

func TestParse_Arguments(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		expected  []*evoke.Declaration
		wantError bool
	}{
		{
			name: "declaration with argument",
			src:  "IMAGE base\n    checkpoint = perfection.safetensors\n",
			expected: []*evoke.Declaration{
				{Name: "IMAGE", RawName: "IMAGE", Argument: "base", Line: 1, Values: []string{"checkpoint = perfection.safetensors"}},
			},
		},
		{
			name: "negative declaration with argument",
			src:  "!IMAGE upscale\n    worst quality\n",
			expected: []*evoke.Declaration{
				{Name: "IMAGE", RawName: "IMAGE", Argument: "upscale", Negative: true, Line: 1, Values: []string{"worst quality"}},
			},
		},
		{
			name: "default declaration with argument",
			src:  "?DETAILER face\n    detector = face_yolov8m\n",
			expected: []*evoke.Declaration{
				{Name: "DETAILER", RawName: "DETAILER", Argument: "face", Default: true, Line: 1, Values: []string{"detector = face_yolov8m"}},
			},
		},
		{
			name:      "too many words after declaration name",
			src:       "IMAGE base extra\n    value\n",
			wantError: true,
		},
		{
			name: "LORA with argument",
			src:  "LORA yasmin\n    model = yasmin_v2.safetensors\n",
			expected: []*evoke.Declaration{
				{Name: "LORA", RawName: "LORA", Argument: "yasmin", Line: 1, Values: []string{"model = yasmin_v2.safetensors"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := evoke.Parse([]byte(tt.src))

			require.Equal(t, tt.wantError, err != nil, "error mismatch: %v", err)
			if !tt.wantError {
				require.Equal(t, tt.expected, doc.Declarations)
			}
		})
	}
}

func TestValidate_Arguments(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantError bool
	}{
		{name: "IMAGE with argument is valid", src: "IMAGE base\n    steps = 35\n"},
		{name: "IMAGE without argument is valid", src: "IMAGE\n    steps = 35\n"},
		{name: "LORA with argument is valid", src: "LORA yasmin\n    model = yasmin.safetensors\n"},
		{name: "DETAILER with argument is valid", src: "DETAILER face\n    detector = yolov8\n"},
		{name: "LORA without argument is invalid", src: "LORA\n    model = yasmin.safetensors\n", wantError: true},
		{name: "NAME with argument is invalid", src: "NAME extra\n    Sumi\n", wantError: true},
		{name: "APPEARANCE with argument is invalid", src: "APPEARANCE extra\n    tall\n", wantError: true},
		{name: "negative IMAGE with settings is invalid", src: "!IMAGE base\n    steps = 35\n", wantError: true},
		{name: "negative IMAGE without argument with text is valid", src: "!IMAGE\n    worst quality\n"},
		{name: "negative IMAGE with text only is valid", src: "!IMAGE base\n    worst quality\n"},
		{name: "negative DETAILER with settings is invalid", src: "!DETAILER face\n    steps = 10\n", wantError: true},
		{name: "negative LORA is invalid (no negative support)", src: "!LORA yasmin\n    model = x.safetensors\n", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, parseErr := evoke.Parse([]byte(tt.src))
			require.NoError(t, parseErr)

			err := evoke.Validate(doc)

			require.Equal(t, tt.wantError, err != nil, "error mismatch: %v", err)
		})
	}
}

func TestIsSetting(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{name: "simple setting", line: "steps = 30", expected: true},
		{name: "setting with underscores", line: "sampler_name = euler", expected: true},
		{name: "no spaces", line: "steps=30", expected: true},
		{name: "prompt text", line: "(quality:1.4), masterpiece", expected: false},
		{name: "prompt with equals later", line: "tattoo that looks like a =) smiley", expected: false},
		{name: "comma-separated prompt", line: "webbed fingers, alien fingers", expected: false},
		{name: "uppercase start", line: "Steps = 30", expected: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, evoke.IsSetting(tt.line))
		})
	}
}

func TestParseSetting(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantKey   string
		wantValue string
	}{
		{name: "simple", line: "steps = 30", wantKey: "steps", wantValue: "30"},
		{name: "no spaces", line: "steps=30", wantKey: "steps", wantValue: "30"},
		{name: "model file", line: "model = yasmin_v2.safetensors", wantKey: "model", wantValue: "yasmin_v2.safetensors"},
		{name: "not a setting", line: "(quality:1.4)", wantKey: "", wantValue: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, val := evoke.ParseSetting(tt.line)

			require.Equal(t, tt.wantKey, key)
			require.Equal(t, tt.wantValue, val)
		})
	}
}

func TestMerge_ImageStages(t *testing.T) {
	tests := []struct {
		name string
		docs []*evoke.Document
		want []evoke.ImageStage
	}{
		{
			name: "single IMAGE base",
			docs: []*evoke.Document{{Declarations: []*evoke.Declaration{
				{Name: "IMAGE", Argument: "base", Values: []string{
					"checkpoint = perfection.safetensors",
					"steps = 35",
					"(quality:1.4), masterpiece",
				}},
			}}},
			want: []evoke.ImageStage{
				{
					Argument: "base",
					Settings: map[string]string{"checkpoint": "perfection.safetensors", "steps": "35"},
					Text:     evoke.Prompt{Positive: []string{"(quality:1.4), masterpiece"}},
				},
			},
		},
		{
			name: "default IMAGE base with explicit overlay",
			docs: []*evoke.Document{
				{Declarations: []*evoke.Declaration{
					{Name: "IMAGE", Argument: "base", Default: true, Values: []string{
						"checkpoint = perfection.safetensors",
						"steps = 35",
						"cfg = 5.5",
						"default prompt text",
					}},
				}},
				{Declarations: []*evoke.Declaration{
					{Name: "IMAGE", Argument: "base", Values: []string{
						"steps = 20",
						"explicit prompt text",
					}},
				}},
			},
			want: []evoke.ImageStage{
				{
					Argument: "base",
					Settings: map[string]string{"checkpoint": "perfection.safetensors", "steps": "20", "cfg": "5.5"},
					Text:     evoke.Prompt{Positive: []string{"explicit prompt text"}},
				},
			},
		},
		{
			name: "IMAGE base with negative text",
			docs: []*evoke.Document{{Declarations: []*evoke.Declaration{
				{Name: "IMAGE", Argument: "base", Values: []string{"(quality:1.4), masterpiece"}},
				{Name: "IMAGE", Argument: "base", Negative: true, Values: []string{"worst quality, blurry"}},
			}}},
			want: []evoke.ImageStage{
				{
					Argument: "base",
					Settings: map[string]string{},
					Text:     evoke.Prompt{Positive: []string{"(quality:1.4), masterpiece"}, Negative: []string{"worst quality, blurry"}},
				},
			},
		},
		{
			name: "negative IMAGE with default positive provides negative text",
			docs: []*evoke.Document{
				{Declarations: []*evoke.Declaration{
					{Name: "IMAGE", Argument: "upscale", Default: true, Values: []string{"steps = 4"}},
				}},
				{Declarations: []*evoke.Declaration{
					{Name: "IMAGE", Argument: "upscale", Negative: true, Values: []string{"disabled"}},
				}},
			},
			want: []evoke.ImageStage{
				{
					Argument: "upscale",
					Settings: map[string]string{"steps": "4"},
					Text:     evoke.Prompt{Negative: []string{"disabled"}},
					Disabled: false,
				},
			},
		},
		{
			name: "negative IMAGE only disables when no positive exists",
			docs: []*evoke.Document{
				{Declarations: []*evoke.Declaration{
					{Name: "IMAGE", Argument: "upscale", Negative: true, Values: []string{"bad upscale"}},
				}},
			},
			want: []evoke.ImageStage{
				{
					Argument: "upscale",
					Text:     evoke.Prompt{Negative: []string{"bad upscale"}},
					Disabled: true,
				},
			},
		},
		{
			name: "lora references accumulate across files",
			docs: []*evoke.Document{
				{Declarations: []*evoke.Declaration{
					{Name: "IMAGE", Argument: "base", Default: true, Values: []string{
						"steps = 35",
						"lora = rimix",
					}},
				}},
				{Declarations: []*evoke.Declaration{
					{Name: "IMAGE", Argument: "base", Values: []string{
						"lora = yasmin",
					}},
				}},
			},
			want: []evoke.ImageStage{
				{
					Argument: "base",
					Settings: map[string]string{"steps": "35"},
					Loras:    []string{"rimix", "yasmin"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evoke.Merge(tt.docs)

			require.Equal(t, tt.want, got.Images)
		})
	}
}

func TestMerge_LoraDefinitions(t *testing.T) {
	tests := []struct {
		name string
		docs []*evoke.Document
		want []evoke.LoraDefinition
	}{
		{
			name: "single LORA definition",
			docs: []*evoke.Document{{Declarations: []*evoke.Declaration{
				{Name: "LORA", Argument: "yasmin", Values: []string{
					"model = yasmin_v2.safetensors",
					"strength = 0.8",
					"clip = 0.8",
				}},
			}}},
			want: []evoke.LoraDefinition{
				{
					Argument: "yasmin",
					Settings: map[string]string{"model": "yasmin_v2.safetensors", "strength": "0.8", "clip": "0.8"},
				},
			},
		},
		{
			name: "default LORA with explicit overlay",
			docs: []*evoke.Document{
				{Declarations: []*evoke.Declaration{
					{Name: "LORA", Argument: "yasmin", Default: true, Values: []string{
						"model = yasmin_v1.safetensors",
						"strength = 1.0",
						"clip = 1.0",
					}},
				}},
				{Declarations: []*evoke.Declaration{
					{Name: "LORA", Argument: "yasmin", Values: []string{
						"model = yasmin_v2.safetensors",
						"strength = 0.8",
					}},
				}},
			},
			want: []evoke.LoraDefinition{
				{
					Argument: "yasmin",
					Settings: map[string]string{"model": "yasmin_v2.safetensors", "strength": "0.8", "clip": "1.0"},
				},
			},
		},
		{
			name: "multiple LORA definitions",
			docs: []*evoke.Document{{Declarations: []*evoke.Declaration{
				{Name: "LORA", Argument: "crayon", Values: []string{"model = crayons.safetensors", "strength = 1.8"}},
				{Name: "LORA", Argument: "detail", Values: []string{"model = detail.safetensors", "strength = 0.4"}},
			}}},
			want: []evoke.LoraDefinition{
				{Argument: "crayon", Settings: map[string]string{"model": "crayons.safetensors", "strength": "1.8"}},
				{Argument: "detail", Settings: map[string]string{"model": "detail.safetensors", "strength": "0.4"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evoke.Merge(tt.docs)

			require.Equal(t, tt.want, got.Loras)
		})
	}
}

func TestMerge_DetailerConfigs(t *testing.T) {
	tests := []struct {
		name string
		docs []*evoke.Document
		want []evoke.DetailerConfig
	}{
		{
			name: "DETAILER with mixed content",
			docs: []*evoke.Document{{Declarations: []*evoke.Declaration{
				{Name: "DETAILER", Argument: "face", Values: []string{
					"realistic skin, natural features",
					"detector = face_yolov8m",
					"steps = 15",
					"denoise = 0.3",
				}},
			}}},
			want: []evoke.DetailerConfig{
				{
					Argument: "face",
					Settings: map[string]string{"detector": "face_yolov8m", "steps": "15", "denoise": "0.3"},
					Text:     evoke.Prompt{Positive: []string{"realistic skin, natural features"}},
				},
			},
		},
		{
			name: "default DETAILER with explicit field overlay",
			docs: []*evoke.Document{
				{Declarations: []*evoke.Declaration{
					{Name: "DETAILER", Argument: "face", Default: true, Values: []string{
						"default face prompt",
						"detector = face_yolov8m",
						"steps = 15",
						"cfg = 4",
						"denoise = 0.3",
					}},
				}},
				{Declarations: []*evoke.Declaration{
					{Name: "DETAILER", Argument: "face", Values: []string{
						"custom face prompt",
						"steps = 30",
					}},
				}},
			},
			want: []evoke.DetailerConfig{
				{
					Argument: "face",
					Settings: map[string]string{"detector": "face_yolov8m", "steps": "30", "cfg": "4", "denoise": "0.3"},
					Text:     evoke.Prompt{Positive: []string{"custom face prompt"}},
				},
			},
		},
		{
			name: "negative DETAILER with default positive provides negative text",
			docs: []*evoke.Document{
				{Declarations: []*evoke.Declaration{
					{Name: "DETAILER", Argument: "hand", Default: true, Values: []string{"steps = 20"}},
				}},
				{Declarations: []*evoke.Declaration{
					{Name: "DETAILER", Argument: "hand", Negative: true, Values: []string{"disabled"}},
				}},
			},
			want: []evoke.DetailerConfig{
				{
					Argument: "hand",
					Settings: map[string]string{"steps": "20"},
					Text:     evoke.Prompt{Negative: []string{"disabled"}},
					Disabled: false,
				},
			},
		},
		{
			name: "negative DETAILER only disables when no positive exists",
			docs: []*evoke.Document{
				{Declarations: []*evoke.Declaration{
					{Name: "DETAILER", Argument: "hand", Negative: true, Values: []string{"bad hands"}},
				}},
			},
			want: []evoke.DetailerConfig{
				{
					Argument: "hand",
					Text:     evoke.Prompt{Negative: []string{"bad hands"}},
					Disabled: true,
				},
			},
		},
		{
			name: "DETAILER with lora references accumulate",
			docs: []*evoke.Document{
				{Declarations: []*evoke.Declaration{
					{Name: "DETAILER", Argument: "face", Default: true, Values: []string{
						"detector = face_yolov8m",
						"lora = rimix",
					}},
				}},
				{Declarations: []*evoke.Declaration{
					{Name: "DETAILER", Argument: "face", Values: []string{
						"lora = yasmin",
					}},
				}},
			},
			want: []evoke.DetailerConfig{
				{
					Argument: "face",
					Settings: map[string]string{"detector": "face_yolov8m"},
					Loras:    []string{"rimix", "yasmin"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evoke.Merge(tt.docs)

			require.Equal(t, tt.want, got.Detailers)
		})
	}
}

func TestMerge_FullPipeline(t *testing.T) {
	pipelineDoc := &evoke.Document{Declarations: []*evoke.Declaration{
		{Name: "LORA", Argument: "rimix", Values: []string{
			"model = rimixxO2.safetensors",
			"strength = 1.0",
			"clip = 1.0",
		}},
		{Name: "IMAGE", Argument: "base", Values: []string{
			"checkpoint = perfection.safetensors",
			"steps = 35",
			"cfg = 5.5",
			"lora = rimix",
			"(quality:1.4), masterpiece",
		}},
		{Name: "IMAGE", Argument: "base", Negative: true, Values: []string{"worst quality, blurry"}},
		{Name: "IMAGE", Argument: "upscale", Values: []string{
			"upscale_model = 4x-AnimeSharp",
			"factor = 1.5",
			"steps = 4",
			"lora = rimix",
		}},
		{Name: "DETAILER", Argument: "face", Default: true, Values: []string{
			"realistic skin",
			"detector = face_yolov8m",
			"steps = 15",
			"lora = rimix",
		}},
		{Name: "PROMPT", Values: []string{"(quality:1.4), masterpiece"}},
		{Name: "PROMPT", Negative: true, Values: []string{"worst quality"}},
	}}

	characterDoc := &evoke.Document{Declarations: []*evoke.Declaration{
		{Name: "NAME", Values: []string{"Yasmin"}},
		{Name: "CHARACTER", Values: []string{"dark-haired woman"}},
		{Name: "APPEARANCE", Values: []string{"long dark wavy hair", "green eyes"}},
		{Name: "LORA", Argument: "yasmin", Values: []string{
			"model = yasmin_v2.safetensors",
			"strength = 0.8",
			"clip = 0.8",
		}},
		{Name: "IMAGE", Argument: "base", Values: []string{"lora = yasmin"}},
		{Name: "DETAILER", Argument: "face", Values: []string{"lora = yasmin"}},
	}}

	got := evoke.Merge([]*evoke.Document{pipelineDoc, characterDoc})

	require.Equal(t, "Yasmin", got.Name)
	require.Equal(t, []string{"dark-haired woman"}, got.Character)
	require.Equal(t, []string{"long dark wavy hair", "green eyes"}, got.Appearance.Positive)
	require.Equal(t, []string{"(quality:1.4), masterpiece"}, got.Prompt.Positive)
	require.Equal(t, []string{"worst quality"}, got.Prompt.Negative)

	// IMAGE base: first explicit wins (pipeline's), but lora references accumulate.
	// Since both files have explicit IMAGE base, it's a conflict — first wins.
	base := got.ImageStageByArgument("base")
	require.NotNil(t, base)
	require.Equal(t, "perfection.safetensors", base.Settings["checkpoint"])
	require.Equal(t, "35", base.Settings["steps"])

	// IMAGE upscale from pipeline.
	up := got.ImageStageByArgument("upscale")
	require.NotNil(t, up)
	require.Equal(t, "4x-AnimeSharp", up.Settings["upscale_model"])

	// LORA definitions.
	require.Len(t, got.Loras, 2)

	// DETAILER face: pipeline provides default, character provides explicit.
	face := got.DetailerByArgument("face")
	require.NotNil(t, face)
	require.Equal(t, "face_yolov8m", face.Settings["detector"])
	require.Equal(t, "15", face.Settings["steps"])
	require.Contains(t, face.Loras, "rimix")
	require.Contains(t, face.Loras, "yasmin")
}

func TestComposition_Lookups(t *testing.T) {
	comp := &evoke.Composition{
		Images:    []evoke.ImageStage{{Argument: "base"}, {Argument: "upscale"}},
		Loras:     []evoke.LoraDefinition{{Argument: "yasmin"}, {Argument: "rimix"}},
		Detailers: []evoke.DetailerConfig{{Argument: "face"}, {Argument: "hand"}},
	}

	require.NotNil(t, comp.ImageStageByArgument("base"))
	require.NotNil(t, comp.ImageStageByArgument("upscale"))
	require.Nil(t, comp.ImageStageByArgument("edit"))

	require.NotNil(t, comp.LoraByArgument("yasmin"))
	require.NotNil(t, comp.LoraByArgument("rimix"))
	require.Nil(t, comp.LoraByArgument("detail"))

	require.NotNil(t, comp.DetailerByArgument("face"))
	require.NotNil(t, comp.DetailerByArgument("hand"))
	require.Nil(t, comp.DetailerByArgument("eye"))
}
