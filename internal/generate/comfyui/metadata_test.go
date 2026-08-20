package comfyui

import (
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// renderFullWorkflow renders the every-branch composition for a base and
// returns the submitted prompt graph — byte-identical to the PNG "prompt"
// chunk Image Saver embeds, which is what ReadPNG parses back.
func renderFullWorkflow(t *testing.T, name string) (promptData, string) {
	t.Helper()

	base, raw, err := resolveBase(name)
	require.NoError(t, err)

	data, _ := renderPromptData(fullComposition(), base)
	applyDefaults(&data, defaultsFor(base))

	payload, err := renderTemplate(data, "Test Character", []string{"/src/test-character.evoke"}, false, raw)
	require.NoError(t, err)

	return data, string(payload)
}

// TestMetadataRoundTrip renders each architecture and reads the result back
// with the parser the viewer uses. It is the guard against writer/reader drift:
// renaming a node class in a template, or adding an architecture that loads its
// model differently, silently empties a section of every reader otherwise.
func TestMetadataRoundTrip(t *testing.T) {
	for _, name := range bases() {
		t.Run(name, func(t *testing.T) {
			data, workflow := renderFullWorkflow(t, name)

			var m Metadata
			parseWorkflow(workflow, &m)

			// Every architecture names its model somewhere: a checkpoint for
			// single-file bases, a unet for the ones loaded as three files.
			wantModel := data.Checkpoint
			if wantModel == "" {
				wantModel = data.Unet
			}
			require.NotEmpty(t, wantModel, "test setup: no model in template data")
			require.Equal(t, wantModel, m.Model)

			require.Equal(t, data.Sampler.SamplerName, m.Sampler)
			require.Equal(t, data.Sampler.Scheduler, m.Scheduler)
			require.Equal(t, data.Sampler.Steps, m.Steps)
			require.Equal(t, data.Sampler.CFG, m.CFG)
			require.Equal(t, data.Sampler.Denoise, m.Denoise)

			// The encoded prompt is what the model saw: the appearance/prompt
			// text with apparel and environment appended, exactly as submitted.
			require.Equal(t,
				strings.Join([]string{data.Positive, data.Apparel.Positive, data.Environment.Positive}, ", "),
				m.Positive)
			require.Equal(t,
				strings.Join([]string{data.Negative, data.Apparel.Negative, data.Environment.Negative}, ", "),
				m.Negative)

			require.NotEmpty(t, data.Loras,
				"test setup: no LoRA resolves under %s — fullComposition needs a variant for it", name)
			require.Len(t, m.LoRAs, len(data.Loras))
			for i, want := range data.Loras {
				require.Equal(t, want.Name, m.LoRAs[i].Name)
				require.Equal(t, want.Strength, m.LoRAs[i].Model)
			}

			require.Equal(t, data.Upscale.Model, m.Upscale.Model)
			require.Equal(t, data.Upscale.Factor, m.Upscale.Factor)
			require.Equal(t, data.Upscale.SamplerName, m.Upscale.Sampler)
			require.Equal(t, data.Upscale.Scheduler, m.Upscale.Scheduler)
			require.Equal(t, data.Upscale.Steps, m.Upscale.Steps)
			require.Equal(t, data.Upscale.CFG, m.Upscale.CFG)
			require.Equal(t, data.Upscale.Denoise, m.Upscale.Denoise)

			names := make([]string, 0, len(m.Detailers))
			for _, d := range m.Detailers {
				names = append(names, d.Name)
				require.Equal(t, data.Face.SamplerName, d.Sampler, "detailer %s", d.Name)
				require.Equal(t, data.Face.Steps, d.Steps, "detailer %s", d.Name)
				require.Equal(t, data.Face.CFG, d.CFG, "detailer %s", d.Name)
				require.Equal(t, data.Face.Denoise, d.Denoise, "detailer %s", d.Name)
				require.Equal(t, data.Face.Positive, d.Positive, "detailer %s", d.Name)
				require.Equal(t, data.Face.Negative, d.Negative, "detailer %s", d.Name)
			}
			require.Equal(t, []string{"eye", "face", "hand", "lower_body", "upper_body"}, names)
		})
	}
}

// TestSeedsAreIndependent covers the two ways seeds go wrong: reused within a
// workflow, and repeated across submissions.
func TestSeedsAreIndependent(t *testing.T) {
	for _, name := range bases() {
		t.Run(name, func(t *testing.T) {
			var first Metadata
			_, workflow := renderFullWorkflow(t, name)
			parseWorkflow(workflow, &first)

			// Each pass samples independently, so no two seeds in one workflow
			// may coincide.
			seeds := map[uint64]string{first.Seed: "base", first.Upscale.Seed: "upscale"}
			require.Len(t, seeds, 2, "base and upscale share a seed")
			for _, d := range first.Detailers {
				require.NotContains(t, seeds, d.Seed, "detailer %s reuses the %s seed", d.Name, seeds[d.Seed])
				seeds[d.Seed] = d.Name
			}

			for seed, owner := range seeds {
				require.NotZero(t, seed, "%s seed is unset", owner)
				require.Less(t, seed, uint64(maxSeed),
					"%s seed exceeds float64 precision and cannot survive a JSON round-trip", owner)
			}

			// Every render is a separate submission and must re-roll.
			var second Metadata
			_, next := renderFullWorkflow(t, name)
			parseWorkflow(next, &second)
			require.NotEqual(t, first.Seed, second.Seed, "consecutive renders share a base seed")
		})
	}
}

// TestParseWorkflowPreservesLargeSeeds pins the decoding bug directly: seeds
// above 2^53 arrive from other tools (ComfyUI accepts the full uint64 range),
// and a float64 decode rounds them — every seed above 2^63 collapsing onto the
// same value, which reads as unrelated generations sharing a seed.
func TestParseWorkflowPreservesLargeSeeds(t *testing.T) {
	const (
		a = uint64(11308837031494826486)
		b = uint64(14643129109696188252)
	)

	graph := func(seed uint64) string {
		raw, err := json.Marshal(map[string]any{
			"sampler": map[string]any{
				"class_type": classKSampler,
				"inputs":     map[string]any{"seed": seed},
			},
		})
		require.NoError(t, err)
		return string(raw)
	}

	var first, second Metadata
	parseWorkflow(graph(a), &first)
	parseWorkflow(graph(b), &second)

	require.Equal(t, a, first.Seed)
	require.Equal(t, b, second.Seed)
	require.NotEqual(t, first.Seed, second.Seed)
}

// chunk builds one PNG chunk with a valid CRC.
func chunk(ctype string, data []byte) []byte {
	out := make([]byte, 0, len(data)+12)
	out = binary.BigEndian.AppendUint32(out, uint32(len(data)))
	body := append([]byte(ctype), data...)
	out = append(out, body...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(body))
}

func textChunk(key, value string) []byte {
	return chunk("tEXt", append(append([]byte(key), 0), value...))
}

// TestReadPNG covers the chunk walk end to end: a real file on disk, read back
// through the same path the viewer uses.
func TestReadPNG(t *testing.T) {
	_, workflow := renderFullWorkflow(t, DefaultBase)

	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], 1216)
	binary.BigEndian.PutUint32(ihdr[4:8], 832)

	png := []byte("\x89PNG\r\n\x1a\n")
	png = append(png, chunk("IHDR", ihdr)...)
	png = append(png, textChunk("prompt", workflow)...)
	png = append(png, textChunk("evoke", `{"sources":["/src/test-character.evoke"],"inputs":["test-character"]}`)...)
	png = append(png, chunk("IEND", nil)...)

	path := filepath.Join(t.TempDir(), "test-image.png")
	require.NoError(t, os.WriteFile(path, png, 0o600))

	m, err := ReadPNG(path)
	require.NoError(t, err)

	require.Equal(t, 1216, m.Width)
	require.Equal(t, 832, m.Height)
	require.Equal(t, int64(len(png)), m.FileSize)
	require.Equal(t, []string{"/src/test-character.evoke"}, m.Sources)
	require.Equal(t, []string{"test-character"}, m.Inputs)
	require.NotZero(t, m.Seed)
	require.Len(t, m.Detailers, 5)
	require.Contains(t, m.Positive, "test-appearance")
}

// TestReadPNGNotAnImage covers the viewer meeting a file it cannot read: a zero
// record, never a failure that would take the pane down.
func TestReadPNGNotAnImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test-empty.png")
	require.NoError(t, os.WriteFile(path, []byte("not a png"), 0o600))

	m, err := ReadPNG(path)
	require.NoError(t, err)
	require.Equal(t, Metadata{FileSize: 9}, m)
}
