package comfyui

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The class types this package's templates emit. The reader matches on them,
// so they live beside the templates that write them: a template that renames a
// node silently strips the corresponding section out of every reader downstream
// unless TestMetadataRoundTrip catches it.
const (
	classCheckpointLoader    = "CheckpointLoaderSimple"
	classUNETLoader          = "UNETLoader"
	classCLIPLoader          = "CLIPLoader"
	classVAELoader           = "VAELoader"
	classLoraLoader          = "LoraLoader"
	classLoraLoaderModelOnly = "LoraLoaderModelOnly"
	classKSampler            = "KSampler"
	classUpscaleModelLoader  = "UpscaleModelLoader"
	classUpscale             = "UltimateSDUpscale"
	classDetailer            = "DetailerForEach"
	classDetectorProvider    = "UltralyticsDetectorProvider"
	classBboxSEGS            = "BboxDetectorSEGS"
	classSEGSFilter          = "ImpactSEGSOrderedFilter"
	classTextEncode          = "CLIPTextEncode"
	// The instruction-edit encoder holds its text in "prompt" rather than
	// "text". It is a text node for this reader's purposes all the same: the
	// paint template keys it prompt_pos/prompt_neg so the lookup below is the
	// same one every other template answers.
	classTextEncodeQwenEdit = "TextEncodeQwenImageEditPlus"
)

// The node keys this package's templates give their sampler passes. A graph is
// a map, so the key is the only thing that distinguishes two nodes of the same
// class.
const (
	nodeSampler = "sampler"
	nodeRefine  = "refine"
)

// maxChunk caps a chunk this reader will hold in memory. A workflow graph runs
// to tens of kilobytes; anything larger is not text we wrote.
const maxChunk = 8 << 20

// Metadata is the generation record recovered from a PNG this package wrote.
// Seeds are uint64 because ComfyUI accepts the full unsigned range; anything
// narrower silently collapses distinct seeds onto one value.
type Metadata struct {
	Model string
	// Clip and VAE are set only by the architectures loaded as separate
	// files; a single-checkpoint base carries all three in Model.
	Clip        string
	VAE         string
	WeightDtype string
	Width       int
	Height      int
	Positive    string
	Negative    string
	Sampler     string
	Scheduler   string
	Steps       int
	CFG         float64
	Seed        uint64
	Denoise     float64
	LoRAs       []LoRA
	FileSize    int64
	Detailers   []Detailer
	Refine      Refine
	Upscale     Upscale
	Sources     []string
	Inputs      []string
	Job         Job
}

// Job is what the CLI knew at submission time. The id is minted client-side
// and handed to ComfyUI as the prompt_id, so a saved image names the queue
// entry that produced it.
type Job struct {
	ID      string
	Created time.Time
	Base    string
	Kind    string
}

// Refine is the second sampler pass over the base latent, if the workflow ran
// one. Denoise is the whole of what it did, so a zero Steps means it did not
// run rather than that it ran for free.
type Refine struct {
	Steps     int
	CFG       float64
	Sampler   string
	Scheduler string
	Denoise   float64
	Seed      uint64
}

// Upscale is the UltimateSDUpscale pass, if the workflow ran one.
type Upscale struct {
	Model      string
	Steps      int
	CFG        float64
	Sampler    string
	Scheduler  string
	Denoise    float64
	Seed       uint64
	Factor     float64
	TileWidth  int
	TileHeight int
	Mode       string
	SeamFix    string
}

// Detailer is one DetailerForEach pass. Name is the template's node key with
// the "_detail" suffix trimmed ("face", "hand"), which is also what the debug
// filenames are built from.
type Detailer struct {
	Name      string
	Positive  string
	Negative  string
	Steps     int
	CFG       float64
	Sampler   string
	Scheduler string
	Denoise   float64
	Seed      uint64
	// Detector and the bbox settings come from the nodes upstream of the pass:
	// what it looked for, and how permissively.
	Detector         string
	Threshold        float64
	Dilation         int
	CropFactor       float64
	DropSize         int
	MaxDetections    int
	GuideSize        int
	MaxSize          int
	Feather          int
	NoiseMaskFeather int
}

// LoRA is one loader in the chain. Clip is zero for model-only loaders.
type LoRA struct {
	Name  string
	Model float64
	Clip  float64
}

// ReadPNG extracts generation metadata from a PNG's text chunks. A file that
// carries no recognizable chunk yields a zero Metadata rather than an error —
// the viewer shows whatever is there, including nothing.
func ReadPNG(path string) (Metadata, error) {
	var m Metadata

	if info, err := os.Stat(path); err == nil {
		m.FileSize = info.Size()
	}

	f, err := os.Open(path)
	if err != nil {
		return m, fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	sig := make([]byte, 8)
	if _, err := io.ReadFull(f, sig); err != nil {
		return m, fmt.Errorf("failed to read %s: %w", path, err)
	}

	// Chunks are walked by seeking over their bodies: only the header and the
	// text chunks are wanted, and IDAT is the whole image — megabytes that
	// reading would cost on every file the viewer touches.
	var header [8]byte
	for {
		if _, err := io.ReadFull(f, header[:]); err != nil {
			break
		}
		length := int64(binary.BigEndian.Uint32(header[0:4]))
		ctype := string(header[4:8])

		if (ctype != "IHDR" && ctype != "tEXt") || length > maxChunk {
			// Body plus the 4-byte CRC.
			if _, err := f.Seek(length+4, io.SeekCurrent); err != nil {
				break
			}
			continue
		}

		data := make([]byte, length)
		if _, err := io.ReadFull(f, data); err != nil {
			break
		}
		if _, err := f.Seek(4, io.SeekCurrent); err != nil {
			break
		}

		switch ctype {
		case "IHDR":
			if length >= 8 {
				m.Width = int(binary.BigEndian.Uint32(data[0:4]))
				m.Height = int(binary.BigEndian.Uint32(data[4:8]))
			}
		case "tEXt":
			idx := bytes.IndexByte(data, 0)
			if idx < 0 {
				continue
			}
			switch string(data[:idx]) {
			case "prompt":
				parseWorkflow(string(data[idx+1:]), &m)
			case "evoke":
				parseEvokeChunk(string(data[idx+1:]), &m)
			}
		}
	}
	return m, nil
}

// workflowNode is one entry of the submitted prompt graph.
type workflowNode struct {
	ClassType string         `json:"class_type"`
	Inputs    map[string]any `json:"inputs"`
}

// parseWorkflow reads the ComfyUI prompt graph. It decodes with UseNumber
// because seeds run to 2^64: decoding into the default float64 rounds every
// large seed and saturates any above 2^63 onto a single value, so distinct
// generations read back as the same seed.
func parseWorkflow(raw string, m *Metadata) {
	var nodes map[string]workflowNode
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&nodes); err != nil {
		return
	}

	// Node id -> encoded text, so a detailer can resolve its prompt links.
	texts := make(map[string]string)
	// A detailer names the segs it refined, which name the detector that found
	// them. What a pass looked for sits two nodes upstream of the pass itself,
	// so it is resolved by following those links rather than by node key.
	detectors := make(map[string]string)
	segs := make(map[string]map[string]any)
	filters := make(map[string]map[string]any)
	for id, node := range nodes {
		switch node.ClassType {
		case classTextEncode:
			texts[id] = str(node.Inputs, "text")
		case classTextEncodeQwenEdit:
			texts[id] = str(node.Inputs, "prompt")
		case classDetectorProvider:
			detectors[id] = str(node.Inputs, "model_name")
		case classBboxSEGS:
			segs[id] = node.Inputs
		case classSEGSFilter:
			filters[id] = node.Inputs
		}
	}

	for id, node := range nodes {
		switch node.ClassType {
		case classCheckpointLoader:
			m.Model = str(node.Inputs, "ckpt_name")
		case classUNETLoader:
			m.Model = str(node.Inputs, "unet_name")
			m.WeightDtype = str(node.Inputs, "weight_dtype")
		case classCLIPLoader:
			m.Clip = str(node.Inputs, "clip_name")
		case classVAELoader:
			m.VAE = str(node.Inputs, "vae_name")
		case classKSampler:
			// A template may sample more than once, and the node key is what
			// tells the passes apart — the same way prompt_pos/prompt_neg are
			// resolved below. Every template names its base pass "sampler".
			switch id {
			case nodeSampler:
				m.Sampler = str(node.Inputs, "sampler_name")
				m.Scheduler = str(node.Inputs, "scheduler")
				m.Steps = integer(node.Inputs, "steps")
				m.CFG = float(node.Inputs, "cfg")
				m.Seed = unsigned(node.Inputs, "seed")
				m.Denoise = float(node.Inputs, "denoise")
			case nodeRefine:
				m.Refine.Sampler = str(node.Inputs, "sampler_name")
				m.Refine.Scheduler = str(node.Inputs, "scheduler")
				m.Refine.Steps = integer(node.Inputs, "steps")
				m.Refine.CFG = float(node.Inputs, "cfg")
				m.Refine.Seed = unsigned(node.Inputs, "seed")
				m.Refine.Denoise = float(node.Inputs, "denoise")
			}
		case classLoraLoader, classLoraLoaderModelOnly:
			if name := str(node.Inputs, "lora_name"); name != "" {
				m.LoRAs = append(m.LoRAs, LoRA{
					Name:  name,
					Model: float(node.Inputs, "strength_model"),
					Clip:  float(node.Inputs, "strength_clip"),
				})
			}
		case classUpscaleModelLoader:
			m.Upscale.Model = str(node.Inputs, "model_name")
		case classUpscale:
			m.Upscale.Sampler = str(node.Inputs, "sampler_name")
			m.Upscale.Scheduler = str(node.Inputs, "scheduler")
			m.Upscale.Steps = integer(node.Inputs, "steps")
			m.Upscale.CFG = float(node.Inputs, "cfg")
			m.Upscale.Seed = unsigned(node.Inputs, "seed")
			m.Upscale.Denoise = float(node.Inputs, "denoise")
			m.Upscale.Factor = float(node.Inputs, "upscale_by")
			m.Upscale.TileWidth = integer(node.Inputs, "tile_width")
			m.Upscale.TileHeight = integer(node.Inputs, "tile_height")
			m.Upscale.Mode = str(node.Inputs, "mode_type")
			m.Upscale.SeamFix = str(node.Inputs, "seam_fix_mode")
		case classDetailer:
			d := Detailer{
				Name:             strings.TrimSuffix(id, "_detail"),
				Positive:         texts[link(node.Inputs, "positive")],
				Negative:         texts[link(node.Inputs, "negative")],
				Steps:            integer(node.Inputs, "steps"),
				CFG:              float(node.Inputs, "cfg"),
				Sampler:          str(node.Inputs, "sampler_name"),
				Scheduler:        str(node.Inputs, "scheduler"),
				Denoise:          float(node.Inputs, "denoise"),
				Seed:             unsigned(node.Inputs, "seed"),
				GuideSize:        integer(node.Inputs, "guide_size"),
				MaxSize:          integer(node.Inputs, "max_size"),
				Feather:          integer(node.Inputs, "feather"),
				NoiseMaskFeather: integer(node.Inputs, "noise_mask_feather"),
			}
			// A cap on detections is an optional filter between the detector
			// and the pass, so the chain is walked one hop at a time.
			source := link(node.Inputs, "segs")
			if f, ok := filters[source]; ok {
				d.MaxDetections = integer(f, "take_count")
				source = link(f, "segs")
			}
			if s, ok := segs[source]; ok {
				d.Detector = detectors[link(s, "bbox_detector")]
				d.Threshold = float(s, "threshold")
				d.Dilation = integer(s, "dilation")
				d.CropFactor = float(s, "crop_factor")
				d.DropSize = integer(s, "drop_size")
			}
			m.Detailers = append(m.Detailers, d)
		}
	}

	m.Positive = texts["prompt_pos"]
	m.Negative = texts["prompt_neg"]

	// Map iteration is random; both lists are displayed, so fix an order.
	sort.Slice(m.LoRAs, func(i, j int) bool { return m.LoRAs[i].Name < m.LoRAs[j].Name })
	sort.Slice(m.Detailers, func(i, j int) bool { return m.Detailers[i].Name < m.Detailers[j].Name })
}

// parseEvokeChunk reads the extra_pnginfo block the CLI attaches.
func parseEvokeChunk(raw string, m *Metadata) {
	var meta struct {
		Sources []string `json:"sources"`
		Inputs  []string `json:"inputs"`
		JobID   string   `json:"job_id"`
		Created string   `json:"created"`
		Base    string   `json:"base"`
		Kind    string   `json:"kind"`
	}
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return
	}
	m.Sources = meta.Sources
	m.Inputs = meta.Inputs
	m.Job = Job{ID: meta.JobID, Base: meta.Base, Kind: meta.Kind}
	if t, err := time.Parse(time.RFC3339, meta.Created); err == nil {
		m.Job.Created = t
	}
}

func str(inputs map[string]any, key string) string {
	v, _ := inputs[key].(string)
	return v
}

// link returns the node id a graph input points at, or "" if the input is a
// literal rather than a ["node", slot] reference.
func link(inputs map[string]any, key string) string {
	ref, ok := inputs[key].([]any)
	if !ok || len(ref) == 0 {
		return ""
	}
	id, _ := ref[0].(string)
	return id
}

func number(inputs map[string]any, key string) json.Number {
	v, _ := inputs[key].(json.Number)
	return v
}

// integer reads a whole-number input. ComfyUI rewrites every submitted value to
// the type its node declares before the graph is embedded, so a field rendered
// as 728 reads back as 728.0 wherever the node declares it FLOAT — Impact's
// guide_size and max_size among them. Accepting only the integer form yields a
// silent zero, and the round trip cannot catch it: that test parses the
// rendered template, which is the value before ComfyUI ever sees it.
func integer(inputs map[string]any, key string) int {
	raw := string(number(inputs, key))
	if n, err := strconv.Atoi(raw); err == nil {
		return n
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return int(f)
}

func unsigned(inputs map[string]any, key string) uint64 {
	n, err := strconv.ParseUint(string(number(inputs, key)), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func float(inputs map[string]any, key string) float64 {
	n, err := number(inputs, key).Float64()
	if err != nil {
		return 0
	}
	return n
}
