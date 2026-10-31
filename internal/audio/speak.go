package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// speechBody builds the /v1/audio/speech request body. It is assembled as a
// map rather than a struct so Plan.Body's settings merge in by name: the
// server's schema types each one, and a struct would have to re-declare every
// field the declaration already knows about.
//
// The same AUDIO settings must reach both transports or the one declaration
// means two different things depending on which command read it, so everything
// Plan.Body maps is sent here exactly as Args sends it to the one-shot runtime.
func speechBody(p *Plan, text string) (map[string]any, []string) {
	body, unsupported := p.Body()
	body["model"] = p.Model
	body["input"] = text
	if p.Instruct != "" {
		body["instruct"] = p.Instruct
	}
	if p.Reference != "" {
		body["ref_audio"] = p.Reference
		if p.ReferenceText != "" {
			body["ref_text"] = p.ReferenceText
		}
	}
	if p.Format != "" {
		body["response_format"] = p.Format
	}
	return body, unsupported
}

// Speak synthesizes text against a resident backend and returns the raw audio
// bytes in Plan.Format, plus the names of any AUDIO settings this transport
// could not carry. Unlike Generate, nothing is written to disk here — the
// caller (a voice-enabled chat, playing and discarding a take) decides that.
func Speak(ctx context.Context, backend *Backend, p *Plan, text string) ([]byte, []string, error) {
	fields, unsupported := speechBody(p, text)
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, unsupported, fmt.Errorf("failed to encode speech request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backend.Endpoint()+"/v1/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, unsupported, fmt.Errorf("failed to build speech request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, unsupported, fmt.Errorf("voice backend request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, unsupported, fmt.Errorf("failed to read speech response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, unsupported, fmt.Errorf("voice backend returned %s: %s", resp.Status, truncate(data, 500))
	}
	return data, unsupported, nil
}

// truncate renders at most max bytes of b as a string, for an error message
// that must not reproduce an entire failed response body.
func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}
