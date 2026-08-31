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

// testSource is the uploaded name an edit render redraws, standing in for what
// Client.Upload returns.
const testSource = "evoke/test-source.png"

// renderFullWorkflow renders the every-branch composition through a kind's
// template for a base and returns the submitted prompt graph — byte-identical
// to the PNG "prompt" chunk Image Saver embeds, which is what ReadPNG parses
// back.
func renderFullWorkflow(t *testing.T, kind Kind, name string, debug bool) (promptData, string) {
	t.Helper()

	base, raw, err := resolveBase(kind, name)
	require.NoError(t, err)

	data, _ := renderPromptData(fullComposition(), kind, base)
	switch kind {
	case KindEdit:
		data.Edit.Image = testSource
	case KindPaint:
		data.Paint.Image = testSource
	}
	applyDefaults(&data, defaultsFor(base))

	payload, err := renderTemplate(data, "Test Character", []string{"/src/test-character.evoke"}, debug, raw)
	require.NoError(t, err)

	return data, string(payload)
}

// TestMetadataRoundTrip renders each architecture and reads the result back
// with the parser the viewer uses. It is the guard against writer/reader drift:
// renaming a node class in a template, or adding an architecture that loads its
// model differently, silently empties a section of every reader otherwise.
func TestMetadataRoundTrip(t *testing.T) {
	for _, name := range bases(KindImage) {
		t.Run(name, func(t *testing.T) {
			data, workflow := renderFullWorkflow(t, KindImage, name, false)

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
	for _, name := range bases(KindImage) {
		t.Run(name, func(t *testing.T) {
			var first Metadata
			_, workflow := renderFullWorkflow(t, KindImage, name, false)
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
			_, next := renderFullWorkflow(t, KindImage, name, false)
			parseWorkflow(next, &second)
			require.NotEqual(t, first.Seed, second.Seed, "consecutive renders share a base seed")
		})
	}
}

// TestEditMetadataRoundTrip is TestMetadataRoundTrip for the edit templates.
// It reads back through the same parser the viewer uses, and additionally pins
// the two ways an edit workflow can quietly stop being an edit: the source
// image dropping out of the graph, and the sampler falling back to the base
// stage's denoise instead of the edit stage's.
func TestEditMetadataRoundTrip(t *testing.T) {
	for _, name := range bases(KindEdit) {
		t.Run(name, func(t *testing.T) {
			data, workflow := renderFullWorkflow(t, KindEdit, name, false)

			var m Metadata
			parseWorkflow(workflow, &m)

			wantModel := data.Checkpoint
			if wantModel == "" {
				wantModel = data.Unet
			}
			require.NotEmpty(t, wantModel, "test setup: no model in template data")
			require.Equal(t, wantModel, m.Model)

			// The sampler is the IMAGE edit stage's, not the base stage's.
			require.Equal(t, data.Edit.SamplerName, m.Sampler)
			require.Equal(t, data.Edit.Scheduler, m.Scheduler)
			require.Equal(t, data.Edit.Steps, m.Steps)
			require.Equal(t, data.Edit.CFG, m.CFG)
			require.Equal(t, data.Edit.Denoise, m.Denoise)
			require.NotEqual(t, data.Sampler.Denoise, m.Denoise,
				"edit sampled at the base stage denoise — the source image is being discarded")

			// The edit stage's own text is appended to the composition prompt:
			// an edit redraws the whole frame, so it still needs the character.
			require.Equal(t,
				strings.Join([]string{data.Positive, data.Apparel.Positive, data.Environment.Positive, data.Edit.Positive}, ", "),
				m.Positive)
			require.Equal(t,
				strings.Join([]string{data.Negative, data.Apparel.Negative, data.Environment.Negative, data.Edit.Negative}, ", "),
				m.Negative)

			require.NotEmpty(t, data.Loras,
				"test setup: no LoRA resolves under %s — fullComposition needs a variant for it", name)
			require.Len(t, m.LoRAs, len(data.Loras))
			for i, want := range data.Loras {
				require.Equal(t, want.Name, m.LoRAs[i].Name)
				require.Equal(t, want.Strength, m.LoRAs[i].Model)
			}

			// An edit runs the one pass it was asked for. Upscale and detailer
			// settings are still resolved from the pipeline file and must stay
			// out of the graph rather than silently doubling the work.
			require.Empty(t, m.Detailers)
			require.Zero(t, m.Upscale)

			require.NotZero(t, m.Seed)
			require.Less(t, m.Seed, uint64(maxSeed),
				"seed exceeds float64 precision and cannot survive a JSON round-trip")

			var second Metadata
			_, next := renderFullWorkflow(t, KindEdit, name, false)
			parseWorkflow(next, &second)
			require.NotEqual(t, m.Seed, second.Seed, "consecutive renders share a seed")
		})
	}
}

// TestPaintMetadataRoundTrip is the round trip for the instruction-edit
// templates. It matters more than the others because paint encodes through
// TextEncodeQwenImageEditPlus, whose text lives in a "prompt" input rather than
// a "text" one — a reader that only knew CLIPTextEncode would show every
// painted image with an empty prompt.
func TestPaintMetadataRoundTrip(t *testing.T) {
	for _, name := range bases(KindPaint) {
		t.Run(name, func(t *testing.T) {
			data, workflow := renderFullWorkflow(t, KindPaint, name, false)

			var m Metadata
			parseWorkflow(workflow, &m)

			// The paint model, never the composition's generation model.
			require.Equal(t, data.Paint.Unet, m.Model)
			require.NotEqual(t, data.Checkpoint, m.Model)

			require.Equal(t, data.Paint.SamplerName, m.Sampler)
			require.Equal(t, data.Paint.Scheduler, m.Scheduler)
			require.Equal(t, data.Paint.Steps, m.Steps)
			require.Equal(t, data.Paint.CFG, m.CFG)
			require.Equal(t, data.Paint.Denoise, m.Denoise)

			// The instruction is PROMPT plus the paint stage's own text. The
			// description channels are absent on purpose: telling the model what
			// the subject looks like is what makes it change more than it was
			// asked to.
			require.Equal(t, data.Paint.Positive, m.Positive)
			require.Equal(t, data.Paint.Negative, m.Negative)
			require.NotContains(t, m.Positive, "test-appearance")
			require.NotContains(t, m.Positive, "test-apparel")
			require.NotContains(t, m.Positive, "test-environment")
			require.Contains(t, m.Positive, "test-prompt", "PROMPT is the instruction")

			// The chain comes from IMAGE paint under its own architecture, so
			// the composition's SDXL and Anima LoRAs are guarded out.
			require.Len(t, m.LoRAs, 1)
			require.Equal(t, "test-lora-3.safetensors", m.LoRAs[0].Name)

			require.Empty(t, m.Detailers)
			require.Zero(t, m.Upscale)

			require.NotZero(t, m.Seed)
			require.Less(t, m.Seed, uint64(maxSeed))
		})
	}
}

// TestPaintReferencesTheSourceImage pins what makes an instruction edit an edit
// rather than a generation: the source reaching the text encoder, where it
// becomes vision tokens and a reference latent. A graph that lost it still
// samples and still saves — it just invents an unrelated image.
func TestPaintReferencesTheSourceImage(t *testing.T) {
	for _, name := range bases(KindPaint) {
		t.Run(name, func(t *testing.T) {
			_, workflow := renderFullWorkflow(t, KindPaint, name, false)

			var nodes map[string]workflowNode
			require.NoError(t, json.Unmarshal([]byte(workflow), &nodes))

			require.Equal(t, testSource, nodes["input"].Inputs["image"])
			require.Equal(t, "input", link(nodes["scale"].Inputs, "image"))

			for _, id := range []string{"prompt_pos", "prompt_neg"} {
				require.Equal(t, classTextEncodeQwenEdit, nodes[id].ClassType, id)
				require.Equal(t, "scale", link(nodes[id].Inputs, "image1"), id)
				// Without the vae the encoder builds no reference latent, and
				// the instruction stops being anchored to the source at all.
				require.Equal(t, "vae", link(nodes[id].Inputs, "vae"), id)
			}

			require.Equal(t, "encode", link(nodes[samplerID(t, nodes)].Inputs, "latent_image"))
		})
	}
}

// TestEditLoadsTheSourceImage pins the link the whole command exists for: the
// uploaded name reaching LoadImage and its pixels reaching the sampler's
// latent. A graph that lost either is still valid JSON and still generates.
func TestEditLoadsTheSourceImage(t *testing.T) {
	for _, name := range bases(KindEdit) {
		t.Run(name, func(t *testing.T) {
			_, workflow := renderFullWorkflow(t, KindEdit, name, false)

			var nodes map[string]workflowNode
			require.NoError(t, json.Unmarshal([]byte(workflow), &nodes))

			require.Contains(t, nodes, "input")
			require.Equal(t, "LoadImage", nodes["input"].ClassType)
			require.Equal(t, testSource, nodes["input"].Inputs["image"])

			require.Equal(t, "VAEEncode", nodes["encode"].ClassType)
			require.Equal(t, "input", link(nodes["encode"].Inputs, "pixels"))
			require.Equal(t, "encode", link(nodes[samplerID(t, nodes)].Inputs, "latent_image"))
		})
	}
}

// samplerID returns the graph's only KSampler, failing if a template ever grows
// a second one — the assertions above would otherwise pick one at random.
func samplerID(t *testing.T, nodes map[string]workflowNode) string {
	t.Helper()
	var found string
	for id, node := range nodes {
		if node.ClassType == classKSampler {
			require.Empty(t, found, "more than one KSampler in an edit workflow")
			found = id
		}
	}
	require.NotEmpty(t, found, "no KSampler in the workflow")
	return found
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
	_, workflow := renderFullWorkflow(t, KindImage, DefaultBase, false)

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
