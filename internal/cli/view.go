package cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/term"
)

// ViewCmd launches the interactive image viewer for recent output.
func ViewCmd(args []string, _ bool) int {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	maxItems := fs.Int("n", 100, "max images to load")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if err := runViewer(context.Background(), *maxItems); err != nil {
		fmt.Fprintf(os.Stderr, "evoke view: %v\n", err)
		return 1
	}
	return 0
}

func runViewer(_ context.Context, maxItems int) error {
	outputDir := resolveOutputDir()
	if outputDir == "" {
		return fmt.Errorf("could not resolve output directory (set EVOKE_OUTPUT_DIR)")
	}

	images, err := loadImages(outputDir, maxItems)
	if err != nil {
		return err
	}
	if len(images) == 0 {
		fmt.Println("No image files found.")
		return nil
	}

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("failed to set raw mode: %w", err)
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	idx := 0

	var show func()
	var refreshNewer func()
	var refreshOlder func()

	refreshNewer = func() {
		topPath := ""
		if len(images) > 0 {
			topPath = images[0].path
		}
		_ = term.Restore(fd, oldState)
		fresh, rerr := loadImages(outputDir, maxItems)
		_, _ = term.MakeRaw(fd)
		if rerr != nil || len(fresh) == 0 {
			return
		}
		splitAt := 0
		for i, img := range fresh {
			if img.path == topPath {
				splitAt = i
				break
			}
		}
		if splitAt == 0 {
			return
		}
		images = append(fresh[:splitAt], images...)
		idx = splitAt - 1
		show()
	}

	refreshOlder = func() {
		tailPath := ""
		if len(images) > 0 {
			tailPath = images[len(images)-1].path
		}
		_ = term.Restore(fd, oldState)
		fresh, rerr := loadImages(outputDir, maxItems)
		_, _ = term.MakeRaw(fd)
		if rerr != nil || len(fresh) == 0 {
			return
		}
		splitAt := -1
		for i, img := range fresh {
			if img.path == tailPath {
				splitAt = i
				break
			}
		}
		if splitAt < 0 || splitAt >= len(fresh)-1 {
			return
		}
		images = append(images, fresh[splitAt+1:]...)
		idx = len(images) - len(fresh) + splitAt + 1
		show()
	}

	show = func() {
		fmt.Print("\033[H")

		w, h, _ := term.GetSize(fd)
		if w == 0 {
			w = 120
		}
		if h == 0 {
			h = 40
		}

		metaWidth := 44
		imgCols := w - metaWidth - 3
		if imgCols < 30 {
			imgCols = 30
			metaWidth = w - imgCols - 3
		}
		imgRows := h - 4

		_ = term.Restore(fd, oldState)
		cmd := exec.Command("chafa",
			fmt.Sprintf("--size=%dx%d", imgCols, imgRows),
			images[idx].path,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
		_, _ = term.MakeRaw(fd)

		meta := extractPNGMeta(images[idx].path)
		metaLines := formatMeta(meta, images[idx].path, metaWidth)

		metaCol := imgCols + 2
		for row := 1; row < h-1; row++ {
			fmt.Printf("\033[%d;%dH\033[K", row, metaCol)
		}
		for i, line := range metaLines {
			row := i + 1
			if row >= h-1 {
				break
			}
			fmt.Printf("\033[%d;%dH\033[2m│\033[0m %s", row, metaCol, line)
		}

		// Status bar.
		fmt.Printf("\033[%d;1H\033[K", h-1)
		fmt.Printf("\033[%d;1H\033[K", h)
		label := images[idx].filename
		if images[idx].subfolder != "" {
			label = filepath.Base(images[idx].subfolder) + "/" + images[idx].filename
		}
		debugAvail := len(findDebugImages(outputDir, images[idx].filename)) > 0
		fmt.Printf("\033[%d;1H\033[1m[%d/%d]\033[0m  %s", h-1, idx+1, len(images), label)
		hints := []string{"← → navigate"}
		if debugAvail {
			hints = append(hints, "t debug")
		}
		hints = append(hints, "d delete", "q quit")
		fmt.Printf("\033[%d;1H\033[2m%s\033[0m", h, strings.Join(hints, "  "))
	}

	show()

	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			break
		}
		if n == 1 {
			switch buf[0] {
			case 'q', 'Q', 0x1b, 0x03: // q, Escape, Ctrl-C
				fmt.Print("\033[2J\033[H")
				return nil
			case 'l', 'n', ' ':
				if idx < len(images)-1 {
					idx++
					show()
				} else {
					refreshOlder()
				}
			case 'h', 'p':
				if idx > 0 {
					idx--
					show()
				} else {
					refreshNewer()
				}
			case 'd', 'D':
				_, hh, _ := term.GetSize(fd)
				if hh == 0 {
					hh = 40
				}
				fmt.Printf("\033[%d;1H\033[K\033[1;31m← → cancel  d again to delete\033[0m", hh)
				confirm := make([]byte, 1)
				if cn, cerr := os.Stdin.Read(confirm); cn == 1 && cerr == nil && (confirm[0] == 'd' || confirm[0] == 'D') {
					_ = os.Remove(images[idx].path)
					images = append(images[:idx], images[idx+1:]...)
					if len(images) == 0 {
						fmt.Print("\033[2J\033[H")
						_ = term.Restore(fd, oldState)
						fmt.Println("No images remaining.")
						return nil
					}
					if idx >= len(images) {
						idx = len(images) - 1
					}
				}
				show()
			case 't', 'T':
				debugImgs := findDebugImages(outputDir, images[idx].filename)
				if len(debugImgs) == 0 {
					show()
					break
				}
				showDebugViewer(fd, debugImgs, outputDir)
				show()
			}
		}
		if n == 3 && buf[0] == 0x1b && buf[1] == '[' {
			switch buf[2] {
			case 'A': // up — jump 50 left
				idx -= 50
				if idx < 0 {
					idx = 0
				}
				show()
			case 'B': // down — jump 50 right
				idx += 50
				if idx >= len(images) {
					idx = len(images) - 1
				}
				show()
			case 'C': // right
				if idx < len(images)-1 {
					idx++
					show()
				} else {
					refreshOlder()
				}
			case 'D': // left
				if idx > 0 {
					idx--
					show()
				} else {
					refreshNewer()
				}
			}
		}
	}
	return nil
}

func showDebugViewer(fd int, debugImgs []string, outputDir string) {
	dIdx := 0
	showDebug := func() {
		fmt.Print("\033[H")
		w, h, _ := term.GetSize(fd)
		if w == 0 {
			w = 120
		}
		if h == 0 {
			h = 40
		}

		metaWidth := 44
		imgCols := w - metaWidth - 3
		if imgCols < 30 {
			imgCols = 30
		}
		imgRows := h - 4

		oldState, _ := term.GetState(fd)
		_ = term.Restore(fd, oldState)
		cmd := exec.Command("chafa",
			fmt.Sprintf("--size=%dx%d", imgCols, imgRows),
			debugImgs[dIdx],
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
		_, _ = term.MakeRaw(fd)

		dName := filepath.Base(debugImgs[dIdx])
		meta := extractPNGMeta(debugImgs[dIdx])

		var matched *detailerInfo
		for _, d := range meta.Detailers {
			prefix := strings.TrimSuffix(d.NodeID, "_detail")
			if strings.Contains(dName, "_"+prefix+"_") {
				matched = &d
				break
			}
		}

		var metaLines []string
		if matched != nil {
			metaLines = formatStepMeta(*matched, metaWidth)
		} else if strings.Contains(dName, "_upscale") {
			metaLines = formatUpscaleMeta(meta.Upscale, metaWidth)
		} else {
			metaLines = formatBaseMeta(meta, metaWidth)
		}
		metaCol := imgCols + 2
		for row := 1; row < h-1; row++ {
			fmt.Printf("\033[%d;%dH\033[K", row, metaCol)
		}
		for i, line := range metaLines {
			row := i + 1
			if row >= h-1 {
				break
			}
			fmt.Printf("\033[%d;%dH\033[2m│\033[0m %s", row, metaCol, line)
		}

		fmt.Printf("\033[%d;1H\033[K\033[33m[debug %d/%d]\033[0m  %s", h-1, dIdx+1, len(debugImgs), dName)
		fmt.Printf("\033[%d;1H\033[K\033[2m← → navigate  q/esc back\033[0m", h)
	}
	showDebug()

	buf := make([]byte, 3)
	for {
		dn, derr := os.Stdin.Read(buf)
		if derr != nil {
			break
		}
		if dn == 1 && (buf[0] == 'q' || buf[0] == 'Q' || buf[0] == 0x1b || buf[0] == 't' || buf[0] == 'T') {
			break
		}
		if dn == 1 {
			switch buf[0] {
			case 'l', 'n', ' ':
				if dIdx < len(debugImgs)-1 {
					dIdx++
					showDebug()
				}
			case 'h', 'p':
				if dIdx > 0 {
					dIdx--
					showDebug()
				}
			}
		}
		if dn == 3 && buf[0] == 0x1b && buf[1] == '[' {
			switch buf[2] {
			case 'C':
				if dIdx < len(debugImgs)-1 {
					dIdx++
					showDebug()
				}
			case 'D':
				if dIdx > 0 {
					dIdx--
					showDebug()
				}
			}
		}
	}
}

type viewImage struct {
	path      string
	filename  string
	subfolder string
}

func loadImages(outputDir string, maxItems int) ([]viewImage, error) {
	type fileEntry struct {
		path    string
		modTime int64
	}
	var files []fileEntry

	_ = filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(outputDir, path)
		if strings.Contains(rel, "tmp/") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" {
			files = append(files, fileEntry{path: path, modTime: info.ModTime().UnixMilli()})
		}
		return nil
	})

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime > files[j].modTime
	})

	if maxItems > 0 && len(files) > maxItems {
		files = files[:maxItems]
	}

	var imgs []viewImage
	for _, f := range files {
		rel, _ := filepath.Rel(outputDir, f.path)
		dir := filepath.Dir(rel)
		sub := ""
		if dir != "." {
			sub = dir
		}
		imgs = append(imgs, viewImage{
			path:      f.path,
			filename:  filepath.Base(f.path),
			subfolder: sub,
		})
	}
	return imgs, nil
}

// resolveOutputDir returns the image output directory.
// Checks EVOKE_OUTPUT_DIR first, then falls back to ~/Documents/ComfyUI/output/images/.
func resolveOutputDir() string {
	if dir := os.Getenv("EVOKE_OUTPUT_DIR"); dir != "" {
		return dir
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(userHome, "Documents", "ComfyUI", "output", "images")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	return ""
}

// --- PNG metadata extraction ---

type pngMeta struct {
	Model     string
	Width     int
	Height    int
	Positive  string
	Negative  string
	FacePos   string
	FaceNeg   string
	Sampler   string
	Scheduler string
	Steps     int
	CFG       float64
	Seed      int64
	Denoise   float64
	LoRAs     []loraInfo
	FileSize  int64
	Detailers []detailerInfo
	Upscale   upscaleInfo
	Sources   []string
	Inputs    []string
}

type upscaleInfo struct {
	Model     string
	Steps     int
	CFG       float64
	Sampler   string
	Scheduler string
	Denoise   float64
	Seed      int64
	Factor    float64
}

type detailerInfo struct {
	NodeID    string
	Positive  string
	Negative  string
	Steps     int
	CFG       float64
	Sampler   string
	Scheduler string
	Denoise   float64
	Seed      int64
}

type loraInfo struct {
	Name  string
	Model float64
	Clip  float64
}

func extractPNGMeta(path string) pngMeta {
	var m pngMeta

	info, err := os.Stat(path)
	if err == nil {
		m.FileSize = info.Size()
	}

	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer func() { _ = f.Close() }()

	sig := make([]byte, 8)
	if _, err := f.Read(sig); err != nil {
		return m
	}

	for {
		var length uint32
		if err := binary.Read(f, binary.BigEndian, &length); err != nil {
			break
		}
		ctype := make([]byte, 4)
		if _, err := f.Read(ctype); err != nil {
			break
		}
		data := make([]byte, length)
		if _, err := f.Read(data); err != nil {
			break
		}
		crc := make([]byte, 4)
		_, _ = f.Read(crc)

		ct := string(ctype)
		if ct == "IHDR" && length >= 8 {
			m.Width = int(binary.BigEndian.Uint32(data[0:4]))
			m.Height = int(binary.BigEndian.Uint32(data[4:8]))
		}
		if ct == "tEXt" {
			idx := bytes.IndexByte(data, 0)
			if idx < 0 {
				continue
			}
			key := string(data[:idx])
			val := string(data[idx+1:])
			if key == "prompt" {
				parsePNGPrompt(val, &m)
			}
			if key == "evoke" {
				parseEvokePNGMeta(val, &m)
			}
		}
	}
	return m
}

func parsePNGPrompt(raw string, m *pngMeta) {
	var nodes map[string]struct {
		ClassType string                 `json:"class_type"`
		Inputs    map[string]interface{} `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(raw), &nodes); err != nil {
		return
	}

	for _, node := range nodes {
		switch node.ClassType {
		case "CheckpointLoaderSimple":
			if v, ok := node.Inputs["ckpt_name"].(string); ok {
				m.Model = v
			}
		case "KSampler":
			if v, ok := node.Inputs["sampler_name"].(string); ok {
				m.Sampler = v
			}
			if v, ok := node.Inputs["scheduler"].(string); ok {
				m.Scheduler = v
			}
			if v, ok := node.Inputs["steps"].(float64); ok {
				m.Steps = int(v)
			}
			if v, ok := node.Inputs["cfg"].(float64); ok {
				m.CFG = v
			}
			if v, ok := node.Inputs["seed"].(float64); ok {
				m.Seed = int64(v)
			}
			if v, ok := node.Inputs["denoise"].(float64); ok {
				m.Denoise = v
			}
		case "LoraLoader":
			li := loraInfo{}
			if v, ok := node.Inputs["lora_name"].(string); ok {
				li.Name = v
			}
			if v, ok := node.Inputs["strength_model"].(float64); ok {
				li.Model = v
			}
			if v, ok := node.Inputs["strength_clip"].(float64); ok {
				li.Clip = v
			}
			if li.Name != "" {
				m.LoRAs = append(m.LoRAs, li)
			}
		case "UpscaleModelLoader":
			if v, ok := node.Inputs["model_name"].(string); ok {
				m.Upscale.Model = v
			}
		case "UltimateSDUpscale":
			if v, ok := node.Inputs["sampler_name"].(string); ok {
				m.Upscale.Sampler = v
			}
			if v, ok := node.Inputs["scheduler"].(string); ok {
				m.Upscale.Scheduler = v
			}
			if v, ok := node.Inputs["steps"].(float64); ok {
				m.Upscale.Steps = int(v)
			}
			if v, ok := node.Inputs["cfg"].(float64); ok {
				m.Upscale.CFG = v
			}
			if v, ok := node.Inputs["seed"].(float64); ok {
				m.Upscale.Seed = int64(v)
			}
			if v, ok := node.Inputs["denoise"].(float64); ok {
				m.Upscale.Denoise = v
			}
			if v, ok := node.Inputs["upscale_by"].(float64); ok {
				m.Upscale.Factor = v
			}
		}
	}

	promptTexts := make(map[string]string)
	for id, node := range nodes {
		if node.ClassType == "CLIPTextEncode" {
			if text, ok := node.Inputs["text"].(string); ok {
				promptTexts[id] = text
			}
		}
	}

	m.Positive = promptTexts["prompt_pos"]
	m.Negative = promptTexts["prompt_neg"]
	m.FacePos = promptTexts["face_pos"]
	m.FaceNeg = promptTexts["face_neg"]

	for id, node := range nodes {
		if node.ClassType != "FaceDetailer" {
			continue
		}
		di := detailerInfo{NodeID: id}
		if v, ok := node.Inputs["steps"].(float64); ok {
			di.Steps = int(v)
		}
		if v, ok := node.Inputs["cfg"].(float64); ok {
			di.CFG = v
		}
		if v, ok := node.Inputs["sampler_name"].(string); ok {
			di.Sampler = v
		}
		if v, ok := node.Inputs["scheduler"].(string); ok {
			di.Scheduler = v
		}
		if v, ok := node.Inputs["denoise"].(float64); ok {
			di.Denoise = v
		}
		if v, ok := node.Inputs["seed"].(float64); ok {
			di.Seed = int64(v)
		}
		if ref, ok := node.Inputs["positive"].([]interface{}); ok && len(ref) >= 1 {
			if nodeID, ok := ref[0].(string); ok {
				di.Positive = promptTexts[nodeID]
			}
		}
		if ref, ok := node.Inputs["negative"].([]interface{}); ok && len(ref) >= 1 {
			if nodeID, ok := ref[0].(string); ok {
				di.Negative = promptTexts[nodeID]
			}
		}
		m.Detailers = append(m.Detailers, di)
	}
	sort.Slice(m.Detailers, func(i, j int) bool {
		return m.Detailers[i].NodeID < m.Detailers[j].NodeID
	})
}

func parseEvokePNGMeta(raw string, m *pngMeta) {
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

// --- Metadata formatting ---

func formatMeta(m pngMeta, fullPath string, width int) []string {
	var lines []string

	add := func(label, value string) {
		if value == "" {
			return
		}
		lines = append(lines, fmt.Sprintf("\033[1m%-10s\033[0m %s", label, value))
	}

	if fullPath != "" {
		display := fullPath
		maxDisplay := width - 12
		if maxDisplay > 0 && len(display) > maxDisplay {
			display = "…" + display[len(display)-maxDisplay+1:]
		}
		lines = append(lines, fmt.Sprintf("\033[1m%-10s\033[0m \033]8;;file://%s\033\\%s\033]8;;\033\\", "File", fullPath, display))
	}
	if m.Width > 0 && m.Height > 0 {
		add("Size", fmt.Sprintf("%dx%d", m.Width, m.Height))
	}
	if m.FileSize > 0 {
		add("Disk", formatBytes(m.FileSize))
	}
	if len(m.Inputs) > 0 {
		add("Inputs", strings.Join(m.Inputs, " "))
	}
	if len(m.Sources) > 0 {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("\033[1m%s\033[0m", "Sources"))
		maxW := width - 4
		if maxW < 20 {
			maxW = 20
		}
		for _, src := range m.Sources {
			display := filepath.Base(src)
			if len(src) <= maxW {
				display = src
			}
			lines = append(lines, "  "+display)
		}
	}
	if m.Model != "" {
		add("Model", m.Model)
	}
	if m.Sampler != "" {
		s := m.Sampler
		if m.Scheduler != "" {
			s += " / " + m.Scheduler
		}
		add("Sampler", s)
	}
	if m.Steps > 0 {
		add("Steps", fmt.Sprintf("%d", m.Steps))
	}
	if m.CFG > 0 {
		add("CFG", fmt.Sprintf("%.1f", m.CFG))
	}
	if m.Seed != 0 {
		add("Seed", fmt.Sprintf("%d", m.Seed))
	}
	for _, l := range m.LoRAs {
		add("LoRA", fmt.Sprintf("%s (%.1f/%.1f)", l.Name, l.Model, l.Clip))
	}

	maxW := width - 2
	if maxW < 20 {
		maxW = 20
	}

	var prompts []struct{ label, text string }
	if m.Positive != "" {
		prompts = append(prompts, struct{ label, text string }{"Positive", m.Positive})
	}
	if m.Negative != "" {
		prompts = append(prompts, struct{ label, text string }{"Negative", m.Negative})
	}
	if m.FacePos != "" {
		prompts = append(prompts, struct{ label, text string }{"Face+", m.FacePos})
	}
	if m.FaceNeg != "" {
		prompts = append(prompts, struct{ label, text string }{"Face-", m.FaceNeg})
	}

	for _, p := range prompts {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("\033[1m%s\033[0m", p.label))
		text := p.text
		for len(text) > 0 {
			end := maxW
			if end > len(text) {
				end = len(text)
			}
			lines = append(lines, "  "+text[:end])
			text = text[end:]
		}
	}

	for _, d := range m.Detailers {
		lines = append(lines, "")
		name := strings.TrimSuffix(d.NodeID, "_detail")
		lines = append(lines, fmt.Sprintf("\033[1;33m%s\033[0m", name))
		if d.Sampler != "" {
			s := d.Sampler
			if d.Scheduler != "" {
				s += " / " + d.Scheduler
			}
			add("Sampler", s)
		}
		if d.Steps > 0 {
			add("Steps", fmt.Sprintf("%d", d.Steps))
		}
		if d.CFG > 0 {
			add("CFG", fmt.Sprintf("%.1f", d.CFG))
		}
		if d.Denoise > 0 {
			add("Denoise", fmt.Sprintf("%.2f", d.Denoise))
		}
		if d.Seed != 0 {
			add("Seed", fmt.Sprintf("%d", d.Seed))
		}
		if d.Positive != "" {
			lines = append(lines, "\033[1m+\033[0m")
			text := d.Positive
			for len(text) > 0 {
				end := maxW
				if end > len(text) {
					end = len(text)
				}
				lines = append(lines, "  "+text[:end])
				text = text[end:]
			}
		}
		if d.Negative != "" {
			lines = append(lines, "\033[1m-\033[0m")
			text := d.Negative
			for len(text) > 0 {
				end := maxW
				if end > len(text) {
					end = len(text)
				}
				lines = append(lines, "  "+text[:end])
				text = text[end:]
			}
		}
	}

	return lines
}

func formatStepMeta(d detailerInfo, width int) []string {
	var lines []string

	add := func(label, value string) {
		lines = append(lines, fmt.Sprintf("\033[1m%-10s\033[0m %s", label, value))
	}

	if d.Sampler != "" {
		s := d.Sampler
		if d.Scheduler != "" {
			s += " / " + d.Scheduler
		}
		add("Sampler", s)
	}
	if d.Steps > 0 {
		add("Steps", fmt.Sprintf("%d", d.Steps))
	}
	if d.CFG > 0 {
		add("CFG", fmt.Sprintf("%.1f", d.CFG))
	}
	if d.Denoise > 0 {
		add("Denoise", fmt.Sprintf("%.2f", d.Denoise))
	}
	if d.Seed != 0 {
		add("Seed", fmt.Sprintf("%d", d.Seed))
	}

	maxW := width - 2
	if maxW < 20 {
		maxW = 20
	}
	if d.Positive != "" {
		lines = append(lines, "")
		lines = append(lines, "\033[1mPositive\033[0m")
		text := d.Positive
		for len(text) > 0 {
			end := maxW
			if end > len(text) {
				end = len(text)
			}
			lines = append(lines, "  "+text[:end])
			text = text[end:]
		}
	}
	if d.Negative != "" {
		lines = append(lines, "")
		lines = append(lines, "\033[1mNegative\033[0m")
		text := d.Negative
		for len(text) > 0 {
			end := maxW
			if end > len(text) {
				end = len(text)
			}
			lines = append(lines, "  "+text[:end])
			text = text[end:]
		}
	}

	return lines
}

func formatUpscaleMeta(u upscaleInfo, width int) []string {
	var lines []string

	add := func(label, value string) {
		lines = append(lines, fmt.Sprintf("\033[1m%-10s\033[0m %s", label, value))
	}

	if u.Model != "" {
		add("Model", u.Model)
	}
	if u.Sampler != "" {
		s := u.Sampler
		if u.Scheduler != "" {
			s += " / " + u.Scheduler
		}
		add("Sampler", s)
	}
	if u.Steps > 0 {
		add("Steps", fmt.Sprintf("%d", u.Steps))
	}
	if u.CFG > 0 {
		add("CFG", fmt.Sprintf("%.1f", u.CFG))
	}
	if u.Denoise > 0 {
		add("Denoise", fmt.Sprintf("%.2f", u.Denoise))
	}
	if u.Factor > 0 {
		add("Factor", fmt.Sprintf("%.1fx", u.Factor))
	}
	if u.Seed != 0 {
		add("Seed", fmt.Sprintf("%d", u.Seed))
	}

	return lines
}

func formatBaseMeta(m pngMeta, width int) []string {
	var lines []string

	add := func(label, value string) {
		lines = append(lines, fmt.Sprintf("\033[1m%-10s\033[0m %s", label, value))
	}

	if m.Model != "" {
		add("Model", m.Model)
	}
	if m.Sampler != "" {
		s := m.Sampler
		if m.Scheduler != "" {
			s += " / " + m.Scheduler
		}
		add("Sampler", s)
	}
	if m.Steps > 0 {
		add("Steps", fmt.Sprintf("%d", m.Steps))
	}
	if m.CFG > 0 {
		add("CFG", fmt.Sprintf("%.1f", m.CFG))
	}
	if m.Denoise > 0 {
		add("Denoise", fmt.Sprintf("%.2f", m.Denoise))
	}
	if m.Seed != 0 {
		add("Seed", fmt.Sprintf("%d", m.Seed))
	}
	for _, l := range m.LoRAs {
		add("LoRA", fmt.Sprintf("%s (%.1f/%.1f)", l.Name, l.Model, l.Clip))
	}

	maxW := width - 2
	if maxW < 20 {
		maxW = 20
	}
	if m.Positive != "" {
		lines = append(lines, "")
		lines = append(lines, "\033[1mPositive\033[0m")
		text := m.Positive
		for len(text) > 0 {
			end := maxW
			if end > len(text) {
				end = len(text)
			}
			lines = append(lines, "  "+text[:end])
			text = text[end:]
		}
	}
	if m.Negative != "" {
		lines = append(lines, "")
		lines = append(lines, "\033[1mNegative\033[0m")
		text := m.Negative
		for len(text) > 0 {
			end := maxW
			if end > len(text) {
				end = len(text)
			}
			lines = append(lines, "  "+text[:end])
			text = text[end:]
		}
	}

	return lines
}

func formatBytes(b int64) string {
	const (
		gb = 1024 * 1024 * 1024
		mb = 1024 * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.0f MB", float64(b)/float64(mb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// timestampIndexPattern matches a timestamp_index pair in a filename.
var timestampIndexPattern = regexp.MustCompile(`(\d{10,}_\d+)`)

func findDebugImages(outputDir, filename string) []string {
	matches := timestampIndexPattern.FindStringSubmatch(filename)
	if len(matches) < 2 {
		return nil
	}
	tsIdx := matches[1]

	tmpDir := filepath.Join(outputDir, "tmp")
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil
	}

	var results []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), tsIdx+"_") {
			results = append(results, filepath.Join(tmpDir, e.Name()))
		}
	}
	sort.Strings(results)
	return results
}
