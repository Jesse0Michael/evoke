// Package comfyui implements the generate.Generator interface for ComfyUI.
// It converts a resolved Evoke document into ComfyUI-specific prompt data,
// merges it with a workflow config, renders a Go template, and submits the
// resulting JSON to ComfyUI's /prompt endpoint.
package comfyui

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/jesse0michael/evoke/internal/generate"
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

//go:embed templates/*.tmpl
var templates embed.FS

const defaultTemplate = "sdxl"

// promptData is the ComfyUI-specific structure passed to workflow templates.
type promptData struct {
	Checkpoint  string   `json:"checkpoint"`
	Positive    string   `json:"positive"`
	Negative    string   `json:"negative"`
	Loras       []lora   `json:"loras"`
	Sampler     sampler  `json:"sampler"`
	Apparel     prompt   `json:"apparel"`
	Environment prompt   `json:"environment"`
	Upscale     upscale  `json:"upscale"`
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
	Workflow string
	Prompt   promptData
	Time     int64
	Index    int
	Debug    bool
	Name     string
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
	evokeData := renderPromptData(doc)
	applyDefaults(&evokeData)

	payload, err := renderTemplate(evokeData, c.Verbose)
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
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/prompt", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to submit to ComfyUI: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
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
	if c.Verbose {
		result.Payload = body
	}

	return result, nil
}

func renderPromptData(doc *evoke.Composition) promptData {
	var pd promptData

	pd.Positive = joinValues(doc.Character, doc.Personality.Positive,
		doc.Appearance.Positive, doc.Backstory,
		singularSlice(doc.Scenario), doc.Prompt.Positive)
	pd.Negative = joinValues(doc.Appearance.Negative, doc.Prompt.Negative)
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

	// Apply LORA definitions and resolve references.
	resolveLoras(doc, &pd)

	// Apply DETAILER configs.
	applyDetailer(doc, &pd, "face", &pd.Face)
	applyDetailer(doc, &pd, "eye", &pd.Eye)
	applyDetailer(doc, &pd, "upper_body", &pd.UpperBody)
	applyDetailer(doc, &pd, "lower_body", &pd.LowerBody)
	applyDetailer(doc, &pd, "hand", &pd.Hand)

	return pd
}

func applyImageSettings(pd *promptData, stage *evoke.ImageStage) {
	if v, ok := stage.Settings["checkpoint"]; ok {
		pd.Checkpoint = v
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
}

func resolveLoras(doc *evoke.Composition, pd *promptData) {
	// Build a lookup from lora name to its definition settings (skip disabled).
	loraDefs := make(map[string]*evoke.LoraDefinition)
	for i := range doc.Loras {
		if !doc.Loras[i].Disabled {
			loraDefs[doc.Loras[i].Argument] = &doc.Loras[i]
		}
	}

	// Resolve lora references from unnamed IMAGE stage into the main lora chain.
	if base := doc.ImageStageByArgument(""); base != nil && !base.Disabled {
		for _, name := range base.Loras {
			if def, ok := loraDefs[name]; ok {
				pd.Loras = append(pd.Loras, loraFromDefinition(def))
			}
		}
	}

	// Resolve lora references from IMAGE upscale into upscale loras.
	if up := doc.ImageStageByArgument("upscale"); up != nil && !up.Disabled {
		for _, name := range up.Loras {
			if def, ok := loraDefs[name]; ok {
				l := loraFromDefinition(def)
				pd.Upscale.Positive = joinComma(pd.Upscale.Positive, "")
				_ = l // Upscale loras are resolved but the template handles them through the main chain for now.
			}
		}
	}
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

const (
	defaultCheckpoint  = "riMixIllustriousAnima_riMixV2.safetensors"
	defaultSteps       = 40
	defaultCFG         = 4.0
	defaultSamplerName = "euler_ancestral"
	defaultScheduler   = "karras"
	defaultDenoise     = 1.0
	defaultWidth       = 1216
	defaultHeight      = 832
)

func applyDefaults(data *promptData) {
	if data.Checkpoint == "" {
		data.Checkpoint = defaultCheckpoint
	}
	if data.Sampler.Steps == 0 {
		data.Sampler.Steps = defaultSteps
	}
	if data.Sampler.CFG == 0 {
		data.Sampler.CFG = defaultCFG
	}
	if data.Sampler.SamplerName == "" {
		data.Sampler.SamplerName = defaultSamplerName
	}
	if data.Sampler.Scheduler == "" {
		data.Sampler.Scheduler = defaultScheduler
	}
	if data.Sampler.Denoise == 0 {
		data.Sampler.Denoise = defaultDenoise
	}
	if data.Sampler.Width == 0 {
		data.Sampler.Width = defaultWidth
	}
	if data.Sampler.Height == 0 {
		data.Sampler.Height = defaultHeight
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

func renderTemplate(data promptData, debug bool) ([]byte, error) {
	raw, err := templates.ReadFile("templates/" + defaultTemplate + ".tmpl")
	if err != nil {
		return nil, fmt.Errorf("template %q not found: %w", defaultTemplate, err)
	}

	data = escapePromptData(data)

	td := templateData{
		Workflow: defaultTemplate,
		Prompt:   data,
		Time:     time.Now().Unix(),
		Index:    0,
		Debug:    debug,
		Name:     "evoke",
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

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"RandomSeed": func() uint64 { return rand.Uint64() },
		"StripExt":   stripExt,
	}
}

func stripExt(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[:i]
	}
	return s
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

func singularSlice(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
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
