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
)

// The class types this package's templates emit. The reader matches on them,
// so they live beside the templates that write them: a template that renames a
// node silently strips the corresponding section out of every reader downstream
// unless TestMetadataRoundTrip catches it.
const (
	classCheckpointLoader    = "CheckpointLoaderSimple"
	classUNETLoader          = "UNETLoader"
	classLoraLoader          = "LoraLoader"
	classLoraLoaderModelOnly = "LoraLoaderModelOnly"
	classKSampler            = "KSampler"
	classUpscaleModelLoader  = "UpscaleModelLoader"
	classUpscale             = "UltimateSDUpscale"
	classDetailer            = "DetailerForEach"
	classTextEncode          = "CLIPTextEncode"
)

// maxChunk caps a chunk this reader will hold in memory. A workflow graph runs
// to tens of kilobytes; anything larger is not text we wrote.
const maxChunk = 8 << 20

// Metadata is the generation record recovered from a PNG this package wrote.
// Seeds are uint64 because ComfyUI accepts the full unsigned range; anything
// narrower silently collapses distinct seeds onto one value.
type Metadata struct {
	Model     string
	Width     int
	Height    int
	Positive  string
	Negative  string
	Sampler   string
	Scheduler string
	Steps     int
	CFG       float64
	Seed      uint64
	Denoise   float64
	LoRAs     []LoRA
	FileSize  int64
	Detailers []Detailer
	Upscale   Upscale
	Sources   []string
	Inputs    []string
}

// Upscale is the UltimateSDUpscale pass, if the workflow ran one.
type Upscale struct {
	Model     string
	Steps     int
	CFG       float64
	Sampler   string
	Scheduler string
	Denoise   float64
	Seed      uint64
	Factor    float64
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
	for id, node := range nodes {
		if node.ClassType == classTextEncode {
			texts[id] = str(node.Inputs, "text")
		}
	}

	for id, node := range nodes {
		switch node.ClassType {
		case classCheckpointLoader:
			m.Model = str(node.Inputs, "ckpt_name")
		case classUNETLoader:
			m.Model = str(node.Inputs, "unet_name")
		case classKSampler:
			m.Sampler = str(node.Inputs, "sampler_name")
			m.Scheduler = str(node.Inputs, "scheduler")
			m.Steps = integer(node.Inputs, "steps")
			m.CFG = float(node.Inputs, "cfg")
			m.Seed = unsigned(node.Inputs, "seed")
			m.Denoise = float(node.Inputs, "denoise")
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
		case classDetailer:
			m.Detailers = append(m.Detailers, Detailer{
				Name:      strings.TrimSuffix(id, "_detail"),
				Positive:  texts[link(node.Inputs, "positive")],
				Negative:  texts[link(node.Inputs, "negative")],
				Steps:     integer(node.Inputs, "steps"),
				CFG:       float(node.Inputs, "cfg"),
				Sampler:   str(node.Inputs, "sampler_name"),
				Scheduler: str(node.Inputs, "scheduler"),
				Denoise:   float(node.Inputs, "denoise"),
				Seed:      unsigned(node.Inputs, "seed"),
			})
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
	}
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return
	}
	m.Sources = meta.Sources
	m.Inputs = meta.Inputs
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

func integer(inputs map[string]any, key string) int {
	n, err := strconv.Atoi(string(number(inputs, key)))
	if err != nil {
		return 0
	}
	return n
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
