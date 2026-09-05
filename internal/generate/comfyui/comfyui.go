// Package comfyui implements the generate.Generator interface for ComfyUI.
// It converts a resolved Evoke document into ComfyUI-specific prompt data,
// merges it with a workflow config, renders a Go template, and submits the
// resulting JSON to ComfyUI's /prompt endpoint.
package comfyui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/jesse0michael/evoke/internal/generate"
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

//go:embed templates/image/*.tmpl templates/edit/*.tmpl templates/paint/*.tmpl
var templates embed.FS

// DefaultBase is the architecture rendered when a composition names none.
const DefaultBase = "sdxl"

// Kind is a family of workflow templates. Both families are keyed by the same
// architecture names, so the composition still picks the architecture and the
// command picks what to do with it — an edit of an Anima composition renders
// through Anima, and no flag can say otherwise.
type Kind string

const (
	KindImage Kind = "image"
	KindEdit  Kind = "edit"
	KindPaint Kind = "paint"
)

// PaintBase is the architecture `paint` renders. Unlike image and edit, the
// composition does not choose it: an instruction-edit model has nothing to do
// with the architecture that generated the image — you paint an SDXL render and
// an Anima render with the same weights — so the composition's `base` would be
// answering a different question. There is exactly one today, so nothing has to
// select. A second one earns a setting of its own on IMAGE paint at that point,
// not a reinterpretation of `base`.
const PaintBase = "qwen"

// allKinds is every template family, in the order they are documented.
var allKinds = []Kind{KindImage, KindEdit, KindPaint}

// baseFor reports which architecture a kind renders for a composition.
func baseFor(kind Kind, doc *evoke.Composition) string {
	if kind == KindPaint {
		return PaintBase
	}
	return compositionBase(doc)
}

// bases lists the architectures a kind can render — one embedded workflow
// template each, named without the .tmpl extension, in sorted order.
func bases(kind Kind) []string {
	entries, err := templates.ReadDir("templates/" + string(kind))
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".tmpl"); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// normalizeBase canonicalizes a base name written in a .evoke file. Base is a
// keyword rather than a file name, so case is not significant.
func normalizeBase(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// compositionBase reports the architecture a composition targets, from the
// unnamed IMAGE stage's base setting. It is a declaration setting rather than a
// CLI flag because the pipeline file supplying the checkpoint or unet is what
// makes a composition SDXL or Anima; a flag could only ever contradict it.
func compositionBase(doc *evoke.Composition) string {
	stage := doc.ImageStageByArgument("")
	if stage == nil {
		return ""
	}
	return normalizeBase(stage.Settings["base"])
}

// resolveBase loads a kind's workflow template for a base, returning the
// canonical base name and the template source. An empty base selects
// DefaultBase.
func resolveBase(kind Kind, name string) (string, []byte, error) {
	if name == "" {
		name = DefaultBase
	}
	// The name comes from a .evoke file and indexes an embedded path, so reject
	// anything that could escape rather than relying on embed.FS to catch it.
	if strings.ContainsAny(name, "/\\") {
		return "", nil, fmt.Errorf("invalid IMAGE base %q (available: %s)", name, strings.Join(bases(kind), ", "))
	}
	raw, err := templates.ReadFile("templates/" + string(kind) + "/" + name + ".tmpl")
	if err != nil {
		return "", nil, fmt.Errorf("unknown IMAGE base %q for %s (available: %s)", name, kind, strings.Join(bases(kind), ", "))
	}
	return name, raw, nil
}

// promptData is the ComfyUI-specific structure passed to workflow templates.
type promptData struct {
	Checkpoint  string   `json:"checkpoint"`
	Unet        string   `json:"unet"`
	Clip        string   `json:"clip"`
	ClipType    string   `json:"clip_type"`
	Vae         string   `json:"vae"`
	WeightDtype string   `json:"weight_dtype"`
	Shift       float64  `json:"shift"`
	NagScale    float64  `json:"nag_scale"`
	NagAlpha    float64  `json:"nag_alpha"`
	NagTau      float64  `json:"nag_tau"`
	Group       string   `json:"group"`
	Positive    string   `json:"positive"`
	Negative    string   `json:"negative"`
	Loras       []lora   `json:"loras"`
	Sampler     sampler  `json:"sampler"`
	Apparel     prompt   `json:"apparel"`
	Environment prompt   `json:"environment"`
	Upscale     upscale  `json:"upscale"`
	Edit        edit     `json:"edit"`
	Paint       paint    `json:"paint"`
	Face        detailer `json:"face"`
	Eye         detailer `json:"eye"`
	UpperBody   detailer `json:"upper_body"`
	LowerBody   detailer `json:"lower_body"`
	Hand        detailer `json:"hand"`
}

type prompt struct {
	Disabled bool   `json:"disabled"`
	Positive string `json:"positive"`
	Negative string `json:"negative"`
}

type sampler struct {
	Steps       int     `json:"steps"`
	CFG         float64 `json:"cfg"`
	SamplerName string  `json:"sampler_name"`
	Scheduler   string  `json:"scheduler"`
	Denoise     float64 `json:"denoise"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
}

type lora struct {
	Name     string  `json:"name"`
	Strength float64 `json:"strength"`
	Clip     float64 `json:"clip"`
}

type upscale struct {
	Disabled    bool    `json:"disabled"`
	Model       string  `json:"model"`
	Positive    string  `json:"positive"`
	Negative    string  `json:"negative"`
	Factor      float64 `json:"factor"`
	Steps       int     `json:"steps"`
	CFG         float64 `json:"cfg"`
	SamplerName string  `json:"sampler_name"`
	Scheduler   string  `json:"scheduler"`
	Denoise     float64 `json:"denoise"`
	TileWidth   int     `json:"tile_width"`
	TileHeight  int     `json:"tile_height"`
	// SeamFix is UltimateSDUpscale's seam_fix_mode. Anything but "None" runs a
	// second pass over every tile boundary, which costs more than the redraw it
	// is fixing — so it is opt-in per architecture, not a global default.
	SeamFix string `json:"seam_fix"`
}

// edit is the IMAGE edit stage: a redraw of a source image. Like upscale it is
// a complete sampler spec rather than an adjustment of the base stage's, since
// the base stage samples from noise at denoise 1.0 and has to keep doing that
// for the same pipeline file to still serve `evoke image`.
type edit struct {
	// Image is how the backend addresses the source, not a local path — for
	// ComfyUI, a name relative to its input directory that the CLI uploaded.
	Image       string  `json:"image"`
	Positive    string  `json:"positive"`
	Negative    string  `json:"negative"`
	Steps       int     `json:"steps"`
	CFG         float64 `json:"cfg"`
	SamplerName string  `json:"sampler_name"`
	Scheduler   string  `json:"scheduler"`
	// Denoise is the whole dial: how much of the source survives. There is no
	// meaningful default that suits every pipeline, which is why an edit stage
	// belongs in the pipeline file next to the sampler it is a variant of.
	Denoise float64 `json:"denoise"`
}

// paint is the IMAGE paint stage: an instruction edit through a model that has
// nothing to do with the one that generated the image. It carries its own unet,
// clip, and vae for exactly that reason — nothing here layers over the unnamed
// IMAGE stage, and a composition can name a paint model and a generation model
// without either meaning anything to the other.
type paint struct {
	// Image is how the backend addresses the source, as Upload returned it.
	Image       string  `json:"image"`
	Unet        string  `json:"unet"`
	Clip        string  `json:"clip"`
	ClipType    string  `json:"clip_type"`
	Vae         string  `json:"vae"`
	WeightDtype string  `json:"weight_dtype"`
	Shift       float64 `json:"shift"`
	CFGNorm     float64 `json:"cfg_norm"`
	// Positive is the instruction, not a description. Negative is normally
	// empty: the reference workflow leaves it so, and the model was not trained
	// on the negative vocabulary an SDXL composition carries.
	Positive    string  `json:"positive"`
	Negative    string  `json:"negative"`
	Steps       int     `json:"steps"`
	CFG         float64 `json:"cfg"`
	SamplerName string  `json:"sampler_name"`
	Scheduler   string  `json:"scheduler"`
	Denoise     float64 `json:"denoise"`
}

type detailer struct {
	Disabled         bool    `json:"disabled"`
	Positive         string  `json:"positive"`
	Negative         string  `json:"negative"`
	Detector         string  `json:"detector"`
	GuideSize        int     `json:"guide_size"`
	MaxSize          int     `json:"max_size"`
	Steps            int     `json:"steps"`
	CFG              float64 `json:"cfg"`
	SamplerName      string  `json:"sampler_name"`
	Scheduler        string  `json:"scheduler"`
	Denoise          float64 `json:"denoise"`
	Feather          int     `json:"feather"`
	BBoxThreshold    float64 `json:"bbox_threshold"`
	BBoxDialation    int     `json:"bbox_dilation"`
	BBoxCropFactor   float64 `json:"bbox_crop_factor"`
	NoiseMaskFeather int     `json:"noise_mask_feather"`
	DropSize         int     `json:"drop_size"`
	MaxDetections    int     `json:"max_detections"`
}

// templateData is the top-level structure passed to the workflow template.
type templateData struct {
	// OutputDir is the save path under the images root; it may contain slashes.
	OutputDir string
	Prompt    promptData
	Time      int64
	Index     int
	Debug     bool
	Name      string
}

// Client implements generate.Generator by submitting to a ComfyUI instance.
type Client struct {
	baseURL string
	http    *http.Client
	Verbose bool
}

// New creates a ComfyUI generator client targeting the given base URL.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Generate converts the resolved Evoke document to ComfyUI prompt data,
// applies sane defaults for missing values, renders the template, and submits to ComfyUI.
func (c *Client) Generate(ctx context.Context, doc *evoke.Composition) (*generate.Result, error) {
	return c.submit(ctx, doc, KindImage, "")
}

// Edit renders the composition through the edit workflow for its architecture,
// redrawing image — a name relative to ComfyUI's input directory, as returned
// by Upload — rather than sampling from noise.
func (c *Client) Edit(ctx context.Context, doc *evoke.Composition, image string) (*generate.Result, error) {
	if image == "" {
		return nil, fmt.Errorf("edit requires a source image")
	}
	return c.submit(ctx, doc, KindEdit, image)
}

// Paint renders the composition through the instruction-edit workflow, altering
// image according to the composition's PROMPT rather than redrawing it.
func (c *Client) Paint(ctx context.Context, doc *evoke.Composition, image string) (*generate.Result, error) {
	if image == "" {
		return nil, fmt.Errorf("paint requires a source image")
	}
	return c.submit(ctx, doc, KindPaint, image)
}

// Upload copies a local image into ComfyUI's input directory and returns the
// name the workflow addresses it by. The name is the content hash, so editing
// the same source repeatedly reuses one file instead of growing the input
// directory a copy at a time.
func (c *Client) Upload(ctx context.Context, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}

	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:8]) + strings.ToLower(filepath.Ext(path))

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("image", name)
	if err != nil {
		return "", fmt.Errorf("failed to build upload: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("failed to build upload: %w", err)
	}
	for field, value := range map[string]string{
		"type":      "input",
		"subfolder": uploadSubfolder,
		"overwrite": "true",
	} {
		if err := w.WriteField(field, value); err != nil {
			return "", fmt.Errorf("failed to build upload: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("failed to build upload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/upload/image", &body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to upload to ComfyUI: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ComfyUI returned %d: %s", resp.StatusCode, string(respBody))
	}

	var uploaded struct {
		Name      string `json:"name"`
		Subfolder string `json:"subfolder"`
	}
	if err := json.Unmarshal(respBody, &uploaded); err != nil {
		return "", fmt.Errorf("failed to decode upload response: %w", err)
	}
	if uploaded.Name == "" {
		return "", fmt.Errorf("ComfyUI accepted the upload but named no file")
	}
	if uploaded.Subfolder != "" {
		return uploaded.Subfolder + "/" + uploaded.Name, nil
	}
	return uploaded.Name, nil
}

// uploadSubfolder keeps CLI uploads out of the input directory ComfyUI's own
// UI lists, so a user's own inputs and evoke's stay separable.
const uploadSubfolder = "evoke"

// submit renders the composition through a kind's template for its
// architecture and posts the workflow to ComfyUI.
func (c *Client) submit(ctx context.Context, doc *evoke.Composition, kind Kind, image string) (*generate.Result, error) {
	base, raw, err := resolveBase(kind, baseFor(kind, doc))
	if err != nil {
		return nil, err
	}

	evokeData, skipped := renderPromptData(doc, kind, base)
	evokeData.Edit.Image = image
	evokeData.Paint.Image = image
	applyDefaults(&evokeData, defaultsFor(base))

	payload, err := renderTemplate(evokeData, doc.Name, doc.Sources, c.Verbose, raw)
	if err != nil {
		return nil, fmt.Errorf("failed to render template: %w", err)
	}

	// Build evoke metadata for PNG embedding via ComfyUI's extra_pnginfo.
	type evokePNGMeta struct {
		Sources []string `json:"sources,omitempty"`
		Inputs  []string `json:"inputs,omitempty"`
	}
	meta := evokePNGMeta{Sources: doc.Sources, Inputs: doc.Inputs}
	metaJSON, _ := json.Marshal(meta)

	body := fmt.Sprintf(`{"prompt": %s, "extra_data": {"extra_pnginfo": {"evoke": %s}}, "client_id": "evoke-cli"}`, string(payload), string(metaJSON))

	// A rejected workflow is precisely when the rendered graph is needed, and the
	// caller only prints it on success, so print it here before failing.
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/prompt", strings.NewReader(body))
	if err != nil {
		if c.Verbose {
			fmt.Println("=== ComfyUI Request ===")
			fmt.Println(body)
			fmt.Println()
		}
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		if c.Verbose {
			fmt.Println("=== ComfyUI Request ===")
			fmt.Println(body)
			fmt.Println()
		}
		return nil, fmt.Errorf("failed to submit to ComfyUI: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		if c.Verbose {
			fmt.Println("=== ComfyUI Request ===")
			fmt.Println(body)
			fmt.Println()
		}
		return nil, fmt.Errorf("ComfyUI returned %d: %s", resp.StatusCode, string(respBody))
	}

	var promptResp struct {
		PromptID string `json:"prompt_id"`
	}
	_ = json.Unmarshal(respBody, &promptResp)

	result := &generate.Result{
		Message:  fmt.Sprintf("ComfyUI accepted (status: %s)", resp.Status),
		PromptID: promptResp.PromptID,
	}
	if len(skipped) > 0 {
		result.Message = fmt.Sprintf("skipped LoRA %s: not built for %s\n%s", strings.Join(skipped, ", "), base, result.Message)
	}
	if c.Verbose {
		result.Payload = body
	}

	return result, nil
}

// renderPromptData converts a composition into template data for the given kind
// and base, returning the names of any LoRAs skipped as incompatible with it.
func renderPromptData(doc *evoke.Composition, kind Kind, base string) (promptData, []string) {
	var pd promptData

	// CHARACTER, PERSONALITY, BACKSTORY, and SCENARIO are chat-only: they carry
	// identity, disposition, history, and narrative situation, none of which a
	// diffusion model can render. Feeding them here spent prompt tokens on
	// unrenderable text and — because APPAREL and ENVIRONMENT are appended last —
	// diluted the parts that do render. Everything drawable about a subject belongs
	// in APPEARANCE. VOICE is excluded for the same reason from the other side: it
	// describes how the subject sounds, which is a future audio target's input, not
	// a diffusion model's.
	//
	// Earlier tokens carry more weight, so composition leads appearance.
	pd.Positive = joinValues(doc.Prompt.Positive, doc.Appearance.Positive)
	pd.Negative = joinValues(doc.Prompt.Negative, doc.Appearance.Negative)
	pd.Apparel.Positive = joinAll(doc.Apparel.Positive)
	pd.Apparel.Negative = joinAll(doc.Apparel.Negative)
	pd.Environment.Positive = joinAll(doc.Environment.Positive)
	pd.Environment.Negative = joinAll(doc.Environment.Negative)

	// Apply IMAGE stage settings (unnamed IMAGE declaration is the base generation).
	if base := doc.ImageStageByArgument(""); base != nil && !base.Disabled {
		applyImageSettings(&pd, base)
		pd.Positive = joinComma(joinAll(base.Text.Positive), pd.Positive)
		pd.Negative = joinComma(joinAll(base.Text.Negative), pd.Negative)
	}

	// Apply IMAGE upscale stage settings.
	if up := doc.ImageStageByArgument("upscale"); up != nil {
		if up.Disabled {
			pd.Upscale.Disabled = true
		} else {
			applyUpscaleSettings(&pd.Upscale, up)
			pd.Upscale.Positive = joinAll(up.Text.Positive)
			pd.Upscale.Negative = joinAll(up.Text.Negative)
		}
	}

	// Apply IMAGE edit stage settings. Unlike upscale this stage is inert
	// unless the caller supplies a source image, so it is resolved for every
	// render and simply goes unread by the image templates.
	if ed := doc.ImageStageByArgument("edit"); ed != nil && !ed.Disabled {
		applyEditSettings(&pd.Edit, ed)
		pd.Edit.Positive = joinAll(ed.Text.Positive)
		pd.Edit.Negative = joinAll(ed.Text.Negative)
	}

	// The instruction is PROMPT, plus whatever an IMAGE paint stage adds. The
	// description channels are deliberately absent: an instruction edit is told
	// what to change, and re-describing the whole subject is what makes it
	// change more than it was asked to.
	//
	// The instruction is set outside the stage check on purpose. A paint
	// composition needs no IMAGE paint block at all — the architecture defaults
	// carry every model setting — so nesting it inside one painted nothing.
	if kind == KindPaint {
		pd.Paint.Positive = joinAll(doc.Prompt.Positive)
		pd.Paint.Negative = joinAll(doc.Prompt.Negative)
		if pt := doc.ImageStageByArgument("paint"); pt != nil && !pt.Disabled {
			applyPaintSettings(&pd.Paint, pt)
			pd.Paint.Positive = joinComma(pd.Paint.Positive, joinAll(pt.Text.Positive))
			pd.Paint.Negative = joinComma(pd.Paint.Negative, joinAll(pt.Text.Negative))
		}
	}

	// Apply LORA definitions and resolve references.
	skipped := resolveLoras(doc, kind, base, &pd)

	// Apply DETAILER configs.
	applyDetailer(doc, &pd, "face", &pd.Face)
	applyDetailer(doc, &pd, "eye", &pd.Eye)
	applyDetailer(doc, &pd, "upper_body", &pd.UpperBody)
	applyDetailer(doc, &pd, "lower_body", &pd.LowerBody)
	applyDetailer(doc, &pd, "hand", &pd.Hand)

	return pd, skipped
}

func applyImageSettings(pd *promptData, stage *evoke.ImageStage) {
	if v, ok := stage.Settings["checkpoint"]; ok {
		pd.Checkpoint = v
	}
	// Split-architecture models (Anima and other Qwen-Image derivatives) ship the
	// diffusion model, text encoder, and VAE as three separate files rather than
	// one checkpoint, so they are named independently.
	if v, ok := stage.Settings["unet"]; ok {
		pd.Unet = v
	}
	if v, ok := stage.Settings["clip"]; ok {
		pd.Clip = v
	}
	if v, ok := stage.Settings["clip_type"]; ok {
		pd.ClipType = v
	}
	if v, ok := stage.Settings["vae"]; ok {
		pd.Vae = v
	}
	if v, ok := stage.Settings["weight_dtype"]; ok {
		pd.WeightDtype = v
	}
	if v, ok := stage.Settings["shift"]; ok {
		pd.Shift = parseFloat(v)
	}
	// Normalized Attention Guidance restores negative prompting at cfg 1, where
	// CFG contributes nothing — the turbo path. nag_scale is the on switch.
	if v, ok := stage.Settings["nag_scale"]; ok {
		pd.NagScale = parseFloat(v)
	}
	if v, ok := stage.Settings["nag_alpha"]; ok {
		pd.NagAlpha = parseFloat(v)
	}
	if v, ok := stage.Settings["nag_tau"]; ok {
		pd.NagTau = parseFloat(v)
	}
	if v, ok := stage.Settings["group"]; ok {
		pd.Group = v
	}
	if v, ok := stage.Settings["steps"]; ok {
		pd.Sampler.Steps = parseInt(v)
	}
	if v, ok := stage.Settings["cfg"]; ok {
		pd.Sampler.CFG = parseFloat(v)
	}
	if v, ok := stage.Settings["sampler_name"]; ok {
		pd.Sampler.SamplerName = v
	}
	if v, ok := stage.Settings["scheduler"]; ok {
		pd.Sampler.Scheduler = v
	}
	if v, ok := stage.Settings["width"]; ok {
		pd.Sampler.Width = parseInt(v)
	}
	if v, ok := stage.Settings["height"]; ok {
		pd.Sampler.Height = parseInt(v)
	}
	if v, ok := stage.Settings["denoise"]; ok {
		pd.Sampler.Denoise = parseFloat(v)
	}
}

func applyUpscaleSettings(up *upscale, stage *evoke.ImageStage) {
	if v, ok := stage.Settings["upscale_model"]; ok {
		up.Model = v
	}
	if v, ok := stage.Settings["factor"]; ok {
		up.Factor = parseFloat(v)
	}
	if v, ok := stage.Settings["steps"]; ok {
		up.Steps = parseInt(v)
	}
	if v, ok := stage.Settings["cfg"]; ok {
		up.CFG = parseFloat(v)
	}
	if v, ok := stage.Settings["sampler_name"]; ok {
		up.SamplerName = v
	}
	if v, ok := stage.Settings["scheduler"]; ok {
		up.Scheduler = v
	}
	if v, ok := stage.Settings["denoise"]; ok {
		up.Denoise = parseFloat(v)
	}
	if v, ok := stage.Settings["tile_width"]; ok {
		up.TileWidth = parseInt(v)
	}
	if v, ok := stage.Settings["tile_height"]; ok {
		up.TileHeight = parseInt(v)
	}
	if v, ok := stage.Settings["seam_fix"]; ok {
		up.SeamFix = v
	}
}

func applyEditSettings(ed *edit, stage *evoke.ImageStage) {
	if v, ok := stage.Settings["steps"]; ok {
		ed.Steps = parseInt(v)
	}
	if v, ok := stage.Settings["cfg"]; ok {
		ed.CFG = parseFloat(v)
	}
	if v, ok := stage.Settings["sampler_name"]; ok {
		ed.SamplerName = v
	}
	if v, ok := stage.Settings["scheduler"]; ok {
		ed.Scheduler = v
	}
	if v, ok := stage.Settings["denoise"]; ok {
		ed.Denoise = parseFloat(v)
	}
}

func applyPaintSettings(pt *paint, stage *evoke.ImageStage) {
	if v, ok := stage.Settings["unet"]; ok {
		pt.Unet = v
	}
	if v, ok := stage.Settings["clip"]; ok {
		pt.Clip = v
	}
	if v, ok := stage.Settings["clip_type"]; ok {
		pt.ClipType = v
	}
	if v, ok := stage.Settings["vae"]; ok {
		pt.Vae = v
	}
	if v, ok := stage.Settings["weight_dtype"]; ok {
		pt.WeightDtype = v
	}
	if v, ok := stage.Settings["shift"]; ok {
		pt.Shift = parseFloat(v)
	}
	if v, ok := stage.Settings["cfg_norm"]; ok {
		pt.CFGNorm = parseFloat(v)
	}
	if v, ok := stage.Settings["steps"]; ok {
		pt.Steps = parseInt(v)
	}
	if v, ok := stage.Settings["cfg"]; ok {
		pt.CFG = parseFloat(v)
	}
	if v, ok := stage.Settings["sampler_name"]; ok {
		pt.SamplerName = v
	}
	if v, ok := stage.Settings["scheduler"]; ok {
		pt.Scheduler = v
	}
	if v, ok := stage.Settings["denoise"]; ok {
		pt.Denoise = parseFloat(v)
	}
}

// resolveLoras builds the LoRA chain for the active base and returns the names
// of the referenced LoRAs it left out.
//
// A LORA whose base names a different architecture is skipped rather than an
// error: weights are trained against one base model and cannot load into
// another, so a file carries a variant per base and only the compatible one
// resolves. An unset base means DefaultBase, exactly as it does on IMAGE — the
// setting has one meaning in the format, and weights that never named an
// architecture were trained against the default one. Only a skip the caller
// would otherwise have seen in the chain is reported — a definition no stage
// references was never going to load, base or not.
func resolveLoras(doc *evoke.Composition, kind Kind, base string, pd *promptData) []string {
	var skipped []string
	reported := make(map[string]bool)

	// take reports whether a referenced LORA loads under the active base,
	// recording the first skip of each name for the caller.
	take := func(def *evoke.LoraDefinition) bool {
		b := normalizeBase(def.Settings["base"])
		if b == "" {
			b = DefaultBase
		}
		if b != base {
			if !reported[def.Argument] {
				reported[def.Argument] = true
				skipped = append(skipped, def.Argument)
			}
			return false
		}
		return true
	}

	loraDefs := make(map[string]*evoke.LoraDefinition)
	for i := range doc.Loras {
		if !doc.Loras[i].Disabled {
			loraDefs[doc.Loras[i].Argument] = &doc.Loras[i]
		}
	}

	// The chain belongs to whichever stage supplies the model being sampled: for
	// paint that is IMAGE paint, which loads a different architecture entirely,
	// so the unnamed stage's LoRAs would be guarded out one at a time anyway.
	chainStage := ""
	if kind == KindPaint {
		chainStage = "paint"
	}
	if stage := doc.ImageStageByArgument(chainStage); stage != nil && !stage.Disabled {
		for _, name := range stage.Loras {
			if def, ok := loraDefs[name]; ok && take(def) {
				pd.Loras = append(pd.Loras, loraFromDefinition(def))
			}
		}
	}

	// Resolve lora references from IMAGE upscale into upscale loras.
	if up := doc.ImageStageByArgument("upscale"); up != nil && !up.Disabled {
		for _, name := range up.Loras {
			if def, ok := loraDefs[name]; ok && take(def) {
				l := loraFromDefinition(def)
				pd.Upscale.Positive = joinComma(pd.Upscale.Positive, "")
				_ = l // Upscale loras are resolved but the template handles them through the main chain for now.
			}
		}
	}

	return skipped
}

func loraFromDefinition(def *evoke.LoraDefinition) lora {
	l := lora{
		Name:     def.Settings["model"],
		Strength: 1.0,
		Clip:     1.0,
	}
	if v, ok := def.Settings["strength"]; ok {
		l.Strength = parseFloat(v)
	}
	if v, ok := def.Settings["clip"]; ok {
		l.Clip = parseFloat(v)
	}
	return l
}

func applyDetailer(doc *evoke.Composition, _ *promptData, arg string, det *detailer) {
	dc := doc.DetailerByArgument(arg)
	if dc == nil {
		return
	}
	if dc.Disabled {
		det.Disabled = true
		return
	}

	if v, ok := dc.Settings["detector"]; ok {
		det.Detector = v
	}
	if v, ok := dc.Settings["guide_size"]; ok {
		det.GuideSize = parseInt(v)
	}
	if v, ok := dc.Settings["max_size"]; ok {
		det.MaxSize = parseInt(v)
	}
	if v, ok := dc.Settings["steps"]; ok {
		det.Steps = parseInt(v)
	}
	if v, ok := dc.Settings["cfg"]; ok {
		det.CFG = parseFloat(v)
	}
	if v, ok := dc.Settings["sampler_name"]; ok {
		det.SamplerName = v
	}
	if v, ok := dc.Settings["scheduler"]; ok {
		det.Scheduler = v
	}
	if v, ok := dc.Settings["denoise"]; ok {
		det.Denoise = parseFloat(v)
	}
	if v, ok := dc.Settings["feather"]; ok {
		det.Feather = parseInt(v)
	}
	if v, ok := dc.Settings["bbox_threshold"]; ok {
		det.BBoxThreshold = parseFloat(v)
	}
	if v, ok := dc.Settings["bbox_dilation"]; ok {
		det.BBoxDialation = parseInt(v)
	}
	if v, ok := dc.Settings["bbox_crop_factor"]; ok {
		det.BBoxCropFactor = parseFloat(v)
	}
	if v, ok := dc.Settings["noise_mask_feather"]; ok {
		det.NoiseMaskFeather = parseInt(v)
	}
	if v, ok := dc.Settings["drop_size"]; ok {
		det.DropSize = parseInt(v)
	}
	if v, ok := dc.Settings["max_detection"]; ok {
		det.MaxDetections = parseInt(v)
	}
	det.Positive = joinAll(dc.Text.Positive)
	det.Negative = joinAll(dc.Text.Negative)
}

func parseInt(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// A base is an architecture — one workflow template each — so the fallbacks for
// a composition that names nothing live with the base rather than globally: Anima is a
// Qwen-Image derivative that wants euler/simple around 1024px, while SDXL wants
// euler_ancestral/karras at 1216x832. Model file names are included because a
// split-architecture template has no checkpoint to fall back on.
var architectureDefaults = map[string]promptData{
	"sdxl": {
		Checkpoint: "riMixIllustriousAnima_riMixV2.safetensors",
		Upscale:    upscale{SeamFix: "Half Tile"},
		Edit: edit{
			Steps:       24,
			CFG:         4.0,
			SamplerName: "euler_ancestral",
			Scheduler:   "karras",
			Denoise:     0.5,
		},
		Sampler: sampler{
			Steps:       40,
			CFG:         4.0,
			SamplerName: "euler_ancestral",
			Scheduler:   "karras",
			Denoise:     1.0,
			Width:       1216,
			Height:      832,
		},
	},
	// The instruction-edit architecture. It has no image or edit template — a
	// model trained to alter a source cannot generate from noise — so only the
	// Paint block here is ever read. Defaults follow Comfy-Org's
	// image_qwen_image_edit_2509 reference with no step-distillation LoRA.
	"qwen": {
		Paint: paint{
			Unet:        "qwen_image_edit_2509_fp8_e4m3fn.safetensors",
			Clip:        "qwen_2.5_vl_7b_fp8_scaled.safetensors",
			ClipType:    "qwen_image",
			Vae:         "qwen_image_vae.safetensors",
			WeightDtype: "default",
			Shift:       3.0,
			CFGNorm:     1.0,
			Steps:       20,
			CFG:         4.0,
			SamplerName: "euler",
			Scheduler:   "simple",
			Denoise:     1.0,
		},
	},
	"anima": {
		Upscale: upscale{SeamFix: "None"},
		Edit: edit{
			Steps:       30,
			CFG:         4.0,
			SamplerName: "euler",
			Scheduler:   "simple",
			Denoise:     0.5,
		},
		Unet:        "anima-base-v1.0.safetensors",
		Clip:        "qwen_3_06b_base.safetensors",
		ClipType:    "stable_diffusion",
		Vae:         "qwen_image_vae.safetensors",
		WeightDtype: "default",
		Sampler: sampler{
			Steps:       30,
			CFG:         4.0,
			SamplerName: "euler",
			Scheduler:   "simple",
			Denoise:     1.0,
			Width:       1024,
			Height:      1024,
		},
	},
}

// defaultsFor returns the fallbacks for a resolved base. An unknown name cannot
// reach here — resolveBase rejects it first — but a template added without a
// matching entry falls back to the default architecture.
func defaultsFor(name string) promptData {
	if def, ok := architectureDefaults[name]; ok {
		return def
	}
	return architectureDefaults[DefaultBase]
}

// applyDefaults fills every value the composition left unset from the
// architecture defaults, then disables the optional passes that name no model.
func applyDefaults(data *promptData, def promptData) {
	if data.Checkpoint == "" {
		data.Checkpoint = def.Checkpoint
	}
	if data.Unet == "" {
		data.Unet = def.Unet
	}
	if data.Clip == "" {
		data.Clip = def.Clip
	}
	if data.ClipType == "" {
		data.ClipType = def.ClipType
	}
	if data.Vae == "" {
		data.Vae = def.Vae
	}
	if data.WeightDtype == "" {
		data.WeightDtype = def.WeightDtype
	}
	if data.Shift == 0 {
		data.Shift = def.Shift
	}
	if data.NagAlpha == 0 {
		data.NagAlpha = def.NagAlpha
	}
	if data.NagTau == 0 {
		data.NagTau = def.NagTau
	}
	if data.Sampler.Steps == 0 {
		data.Sampler.Steps = def.Sampler.Steps
	}
	if data.Sampler.CFG == 0 {
		data.Sampler.CFG = def.Sampler.CFG
	}
	if data.Sampler.SamplerName == "" {
		data.Sampler.SamplerName = def.Sampler.SamplerName
	}
	if data.Sampler.Scheduler == "" {
		data.Sampler.Scheduler = def.Sampler.Scheduler
	}
	if data.Sampler.Denoise == 0 {
		data.Sampler.Denoise = def.Sampler.Denoise
	}
	if data.Sampler.Width == 0 {
		data.Sampler.Width = def.Sampler.Width
	}
	if data.Sampler.Height == 0 {
		data.Sampler.Height = def.Sampler.Height
	}
	if data.Upscale.SeamFix == "" {
		data.Upscale.SeamFix = def.Upscale.SeamFix
	}
	if data.Edit.Steps == 0 {
		data.Edit.Steps = def.Edit.Steps
	}
	if data.Edit.CFG == 0 {
		data.Edit.CFG = def.Edit.CFG
	}
	if data.Edit.SamplerName == "" {
		data.Edit.SamplerName = def.Edit.SamplerName
	}
	if data.Edit.Scheduler == "" {
		data.Edit.Scheduler = def.Edit.Scheduler
	}
	if data.Edit.Denoise == 0 {
		data.Edit.Denoise = def.Edit.Denoise
	}
	if data.Paint.Unet == "" {
		data.Paint.Unet = def.Paint.Unet
	}
	if data.Paint.Clip == "" {
		data.Paint.Clip = def.Paint.Clip
	}
	if data.Paint.ClipType == "" {
		data.Paint.ClipType = def.Paint.ClipType
	}
	if data.Paint.Vae == "" {
		data.Paint.Vae = def.Paint.Vae
	}
	if data.Paint.WeightDtype == "" {
		data.Paint.WeightDtype = def.Paint.WeightDtype
	}
	if data.Paint.Shift == 0 {
		data.Paint.Shift = def.Paint.Shift
	}
	if data.Paint.CFGNorm == 0 {
		data.Paint.CFGNorm = def.Paint.CFGNorm
	}
	if data.Paint.Steps == 0 {
		data.Paint.Steps = def.Paint.Steps
	}
	if data.Paint.CFG == 0 {
		data.Paint.CFG = def.Paint.CFG
	}
	if data.Paint.SamplerName == "" {
		data.Paint.SamplerName = def.Paint.SamplerName
	}
	if data.Paint.Scheduler == "" {
		data.Paint.Scheduler = def.Paint.Scheduler
	}
	if data.Paint.Denoise == 0 {
		data.Paint.Denoise = def.Paint.Denoise
	}

	// Disable upscale/detailers if not explicitly configured.
	if data.Upscale.Model == "" {
		data.Upscale.Disabled = true
	}
	if data.Face.Detector == "" {
		data.Face.Disabled = true
	}
	if data.Eye.Detector == "" {
		data.Eye.Disabled = true
	}
	if data.UpperBody.Detector == "" {
		data.UpperBody.Disabled = true
	}
	if data.LowerBody.Detector == "" {
		data.LowerBody.Disabled = true
	}
	if data.Hand.Detector == "" {
		data.Hand.Disabled = true
	}
}

func renderTemplate(data promptData, compositionName string, sources []string, debug bool, raw []byte) ([]byte, error) {

	group := sanitizeGroup(data.Group)

	data = escapePromptData(data)

	// Use the composition NAME as the output directory, falling back to "evoke".
	dir := "evoke"
	if compositionName != "" {
		dir = toSnakeCase(compositionName)
	}

	// An IMAGE group shelves the character directory under it rather than replacing it.
	if group != "" {
		dir = group + "/" + dir
	}

	// Build the filename prefix from source file basenames joined with underscores.
	name := buildFilePrefix(sources)

	td := templateData{
		OutputDir: dir,
		Prompt:    data,
		Time:      time.Now().Unix(),
		Index:     0,
		Debug:     debug,
		Name:      name,
	}

	tmpl, err := template.New("workflow").Funcs(templateFuncs()).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.Bytes(), nil
}

// maxSeed bounds a generated seed to the largest integer that survives a
// float64 round-trip. ComfyUI itself accepts the full uint64 range, but the
// workflow travels as JSON through consumers whose numbers are float64 — the
// ComfyUI web UI's JSON.parse among them — and a seed above 2^53 reads back
// rounded, so reloading the embedded workflow no longer reproduces the image.
const maxSeed = 1 << 53

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"RandomSeed": func() uint64 { return rand.Uint64N(maxSeed) },
		"StripExt":   stripExt,
	}
}

func stripExt(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[:i]
	}
	return s
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// toSnakeCase converts a string to lowercase with non-alphanumeric runs replaced by underscores.
func toSnakeCase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlphanumeric.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

// buildFilePrefix constructs a filename prefix from source file basenames,
// stripped of extensions, lowercased, and joined with underscores.
func buildFilePrefix(sources []string) string {
	if len(sources) == 0 {
		return "evoke"
	}
	parts := make([]string, 0, len(sources))
	for _, src := range sources {
		base := filepath.Base(src)
		base = stripExt(base)
		parts = append(parts, toSnakeCase(base))
	}
	return strings.Join(parts, "_")
}

// sanitizeGroup normalizes an IMAGE group into a relative path segment list.
// Each segment is snake-cased independently so nesting ("tests/noir") survives,
// and segments that normalize to nothing are dropped — which is what strips
// traversal ("..") and leading slashes, since ComfyUI resolves this prefix
// against its own output root.
func sanitizeGroup(s string) string {
	parts := make([]string, 0, strings.Count(s, "/")+1)
	for seg := range strings.SplitSeq(s, "/") {
		if seg = toSnakeCase(seg); seg != "" {
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "/")
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

func escapePromptData(p promptData) promptData {
	p.Positive = jsonEscape(p.Positive)
	p.Negative = jsonEscape(p.Negative)
	p.Apparel.Positive = jsonEscape(p.Apparel.Positive)
	p.Apparel.Negative = jsonEscape(p.Apparel.Negative)
	p.Environment.Positive = jsonEscape(p.Environment.Positive)
	p.Environment.Negative = jsonEscape(p.Environment.Negative)
	p.Face.Positive = jsonEscape(p.Face.Positive)
	p.Face.Negative = jsonEscape(p.Face.Negative)
	p.UpperBody.Positive = jsonEscape(p.UpperBody.Positive)
	p.UpperBody.Negative = jsonEscape(p.UpperBody.Negative)
	p.LowerBody.Positive = jsonEscape(p.LowerBody.Positive)
	p.LowerBody.Negative = jsonEscape(p.LowerBody.Negative)
	p.Hand.Positive = jsonEscape(p.Hand.Positive)
	p.Hand.Negative = jsonEscape(p.Hand.Negative)
	p.Upscale.Positive = jsonEscape(p.Upscale.Positive)
	p.Upscale.Negative = jsonEscape(p.Upscale.Negative)
	p.Edit.Positive = jsonEscape(p.Edit.Positive)
	p.Edit.Negative = jsonEscape(p.Edit.Negative)
	p.Edit.Image = jsonEscape(p.Edit.Image)
	p.Paint.Positive = jsonEscape(p.Paint.Positive)
	p.Paint.Negative = jsonEscape(p.Paint.Negative)
	p.Paint.Image = jsonEscape(p.Paint.Image)
	return p
}

func joinValues(groups ...[]string) string {
	var parts []string
	for _, g := range groups {
		for _, v := range g {
			if v != "" {
				parts = append(parts, v)
			}
		}
	}
	return strings.Join(parts, ", ")
}

func joinAll(values []string) string {
	return strings.Join(values, ", ")
}

func joinComma(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + ", " + b
}

// Queue retrieves the current running and pending items from ComfyUI.
func (c *Client) Queue(ctx context.Context) ([]generate.QueueItem, []generate.QueueItem, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/queue", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to reach ComfyUI: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("ComfyUI returned %d", resp.StatusCode)
	}

	var queueResp struct {
		Running []json.RawMessage `json:"queue_running"`
		Pending []json.RawMessage `json:"queue_pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&queueResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode queue response: %w", err)
	}

	running := parseQueueItems(queueResp.Running)
	pending := parseQueueItems(queueResp.Pending)
	return running, pending, nil
}

// parseQueueItems extracts queue items from ComfyUI's queue response.
// Each item is a JSON array: [number, prompt_id, prompt, extra_data, outputs_to_execute].
func parseQueueItems(raw []json.RawMessage) []generate.QueueItem {
	var items []generate.QueueItem
	for _, entry := range raw {
		var arr []json.RawMessage
		if err := json.Unmarshal(entry, &arr); err != nil || len(arr) < 2 {
			continue
		}
		var number int
		var promptID string
		_ = json.Unmarshal(arr[0], &number)
		_ = json.Unmarshal(arr[1], &promptID)
		items = append(items, generate.QueueItem{
			PromptID: promptID,
			Number:   number,
		})
	}
	return items
}

// ClearQueue cancels all pending and running items in ComfyUI's queue.
func (c *Client) ClearQueue(ctx context.Context) error {
	body := `{"clear": true}`
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/queue", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach ComfyUI: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ComfyUI returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ResolveOutputs retrieves the output files for a completed generation.
func (c *Client) ResolveOutputs(ctx context.Context, promptID string) ([]generate.Output, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/history/"+promptID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach ComfyUI: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ComfyUI returned %d", resp.StatusCode)
	}

	var historyResp map[string]struct {
		Outputs map[string]struct {
			Images []struct {
				Filename  string `json:"filename"`
				Subfolder string `json:"subfolder"`
				Type      string `json:"type"`
			} `json:"images"`
		} `json:"outputs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&historyResp); err != nil {
		return nil, fmt.Errorf("failed to decode history response: %w", err)
	}

	entry, ok := historyResp[promptID]
	if !ok {
		return nil, nil // not yet completed or not found
	}

	var outputs []generate.Output
	for _, node := range entry.Outputs {
		for _, img := range node.Images {
			outputs = append(outputs, generate.Output{
				Filename:  img.Filename,
				Subfolder: img.Subfolder,
				Type:      img.Type,
			})
		}
	}
	return outputs, nil
}
