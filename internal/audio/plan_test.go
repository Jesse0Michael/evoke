package audio_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jesse0michael/evoke/internal/audio"
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

func TestCompile(t *testing.T) {
	dir := t.TempDir()
	refPath := filepath.Join(dir, "sumi.wav")
	require.NoError(t, os.WriteFile(refPath, []byte("not really audio"), 0o600))

	tests := []struct {
		name            string
		composition     *evoke.Composition
		trusted         audio.Trusted
		wantInstruct    string
		wantModel       string
		wantFormat      string
		wantReference   string
		wantSettings    map[string]string
		wantDiagnostics []string
		wantError       bool
	}{
		{
			name: "voice becomes the instruction",
			composition: &evoke.Composition{
				Voice: evoke.Prompt{Positive: []string{"low alto", "slight rasp", "unhurried"}},
			},
			wantInstruct: "low alto, slight rasp, unhurried",
			wantModel:    audio.DefaultModel,
			wantFormat:   "wav",
			wantSettings: map[string]string{},
		},
		{
			name: "negative voice is reported rather than dropped",
			composition: &evoke.Composition{
				Voice: evoke.Prompt{Positive: []string{"low alto"}, Negative: []string{"shrill"}},
			},
			wantInstruct: "low alto",
			wantModel:    audio.DefaultModel,
			wantFormat:   "wav",
			wantSettings: map[string]string{},
			wantDiagnostics: []string{
				"!VOICE is ignored: a speech runtime takes one positive description and has no negative channel",
			},
		},
		{
			name: "audio settings map to runtime flags",
			composition: &evoke.Composition{
				Voice: evoke.Prompt{Positive: []string{"low alto"}},
				Audio: &evoke.AudioConfig{Settings: map[string]string{
					"model": "mlx-community/Kokoro-82M-bf16", "speed": "1.2", "cfg_scale": "3.0", "format": "mp3",
				}},
			},
			wantInstruct: "low alto",
			wantModel:    "mlx-community/Kokoro-82M-bf16",
			wantFormat:   "mp3",
			wantSettings: map[string]string{"speed": "1.2", "cfg_scale": "3.0"},
		},
		{
			name: "audio free text joins the voice description",
			composition: &evoke.Composition{
				Voice: evoke.Prompt{Positive: []string{"low alto"}},
				Audio: &evoke.AudioConfig{Instructions: []string{"speaking quietly"}},
			},
			wantInstruct: "low alto, speaking quietly",
			wantModel:    audio.DefaultModel,
			wantFormat:   "wav",
			wantSettings: map[string]string{},
		},
		{
			name: "unknown settings are reported rather than forwarded",
			composition: &evoke.Composition{
				Voice: evoke.Prompt{Positive: []string{"low alto"}},
				Audio: &evoke.AudioConfig{Settings: map[string]string{"checkpoint": "sdxl.safetensors"}},
			},
			wantInstruct:    "low alto",
			wantModel:       audio.DefaultModel,
			wantFormat:      "wav",
			wantSettings:    map[string]string{},
			wantDiagnostics: []string{"AUDIO checkpoint is not a known setting and was ignored"},
		},
		{
			name: "reference resolves against the model paths",
			composition: &evoke.Composition{
				Voice: evoke.Prompt{Positive: []string{"low alto"}},
				Audio: &evoke.AudioConfig{Settings: map[string]string{"reference": "sumi.wav"}},
			},
			trusted:       audio.Trusted{ModelPaths: []string{dir}},
			wantInstruct:  "low alto",
			wantModel:     audio.DefaultModel,
			wantFormat:    "wav",
			wantReference: refPath,
			wantSettings:  map[string]string{},
		},
		{
			name: "a missing reference is an error",
			composition: &evoke.Composition{
				Voice: evoke.Prompt{Positive: []string{"low alto"}},
				Audio: &evoke.AudioConfig{Settings: map[string]string{"reference": "absent.wav"}},
			},
			wantError: true,
		},
		{
			name:        "nothing describing the speaker is an error",
			composition: &evoke.Composition{},
			wantError:   true,
		},
		{
			name: "a voice preset alone describes the speaker",
			composition: &evoke.Composition{
				Audio: &evoke.AudioConfig{Settings: map[string]string{"voice": "af_heart"}},
			},
			wantModel:    audio.DefaultModel,
			wantFormat:   "wav",
			wantSettings: map[string]string{"voice": "af_heart"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := audio.Compile(tt.composition, tt.trusted)

			require.Equal(t, tt.wantError, err != nil, "error mismatch: %v", err)
			if tt.wantError {
				return
			}
			require.Equal(t, tt.wantInstruct, plan.Instruct)
			require.Equal(t, tt.wantModel, plan.Model)
			require.Equal(t, tt.wantFormat, plan.Format)
			require.Equal(t, tt.wantReference, plan.Reference)
			require.Equal(t, tt.wantSettings, plan.Settings)
			require.Equal(t, tt.wantDiagnostics, plan.Diagnostics)
		})
	}
}

func TestPlanArgs(t *testing.T) {
	tests := []struct {
		name string
		plan *audio.Plan
		want []string
	}{
		{
			name: "description only",
			plan: &audio.Plan{
				Model: "m", Instruct: "low alto", Format: "wav",
				Settings: map[string]string{"speed": "1.2"},
			},
			want: []string{
				"--model", "m", "--text", "Hello.", "--output_path", "/out",
				"--file_prefix", "sumi", "--audio_format", "wav",
				"--instruct", "low alto", "--speed", "1.2",
			},
		},
		{
			name: "reference without a transcript lets the runtime transcribe",
			plan: &audio.Plan{
				Model: "m", Reference: "/clips/sumi.wav", Format: "wav",
				Settings: map[string]string{},
			},
			want: []string{
				"--model", "m", "--text", "Hello.", "--output_path", "/out",
				"--file_prefix", "sumi", "--audio_format", "wav",
				"--ref_audio", "/clips/sumi.wav",
			},
		},
		{
			name: "reference with a transcript passes it through",
			plan: &audio.Plan{
				Model: "m", Reference: "/clips/sumi.wav", ReferenceText: "Sit down.", Format: "wav",
				Settings: map[string]string{},
			},
			want: []string{
				"--model", "m", "--text", "Hello.", "--output_path", "/out",
				"--file_prefix", "sumi", "--audio_format", "wav",
				"--ref_audio", "/clips/sumi.wav", "--ref_text", "Sit down.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.plan.Args("Hello.", "/out", "sumi"))
		})
	}
}
