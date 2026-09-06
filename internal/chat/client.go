package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxErrorBody bounds how much of an error response body is read into an error
// message, so a misbehaving backend cannot balloon memory or logs.
const maxErrorBody = 8 << 10

// Client is a minimal OpenAI-compatible chat-completions client. It speaks the
// structured messages API and streams responses; the same client serves a
// server Evoke launched and one it connected to, since only ownership differs.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient creates a client for an OpenAI-compatible endpoint. baseURL should
// include any version prefix (e.g. http://127.0.0.1:8080/v1). apiKey may be
// empty for local backends. It has no request timeout; callers bound requests
// with the context so streaming responses are not cut off arbitrarily.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{},
	}
}

// CompletionRequest is a single chat-completion request built by the session
// from the retained transcript.
type CompletionRequest struct {
	Model    string
	Messages []Message
	Sampling Sampling
}

// Usage reports the backend's actual token accounting for a response. Zero
// values mean the backend did not report usage.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// Stream sends a chat-completion request and invokes onDelta for each text
// chunk as it arrives. It returns the fully assembled response and the backend's
// token usage only on success. On any error (including context cancellation) it
// returns the error and the caller must not treat the partial text as complete.
func (c *Client) Stream(ctx context.Context, cr CompletionRequest, onDelta func(string)) (string, Usage, error) {
	resp, err := c.do(ctx, cr, true)
	if err != nil {
		return "", Usage{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var full strings.Builder
	var usage Usage
	var finish string
	var reasoned bool
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", Usage{}, fmt.Errorf("malformed stream event: %w", err)
		}
		if chunk.Usage != nil {
			usage = chunk.Usage.toUsage()
		}
		for _, ch := range chunk.Choices {
			if ch.FinishReason != nil {
				finish = *ch.FinishReason
			}
			// A reasoning model streams its chain of thought on a separate
			// channel. It is not the reply, but knowing it arrived is what makes
			// an answerless response explainable.
			if ch.Delta.Reasoning != "" {
				reasoned = true
			}
			if ch.Delta.Content == "" {
				continue
			}
			full.WriteString(ch.Delta.Content)
			if onDelta != nil {
				onDelta(ch.Delta.Content)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		// Prefer the context cause so cancellation reports cleanly.
		if cause := context.Cause(ctx); cause != nil {
			return "", Usage{}, cause
		}
		return "", Usage{}, fmt.Errorf("stream read failed: %w", err)
	}
	if full.Len() == 0 {
		return "", Usage{}, emptyReplyError(finish, reasoned)
	}
	return full.String(), usage, nil
}

// Complete sends a non-streaming chat-completion request and returns the full
// response and token usage. It is the fallback when streaming is not desired.
func (c *Client) Complete(ctx context.Context, cr CompletionRequest) (string, Usage, error) {
	resp, err := c.do(ctx, cr, false)
	if err != nil {
		return "", Usage{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed completionResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", Usage{}, fmt.Errorf("failed to decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", Usage{}, fmt.Errorf("backend returned no choices")
	}
	choice := parsed.Choices[0]
	if choice.Message.Content == "" {
		finish := ""
		if choice.FinishReason != nil {
			finish = *choice.FinishReason
		}
		return "", Usage{}, emptyReplyError(finish, choice.Message.Reasoning != "")
	}
	return choice.Message.Content, parsed.Usage.toUsage(), nil
}

// emptyReplyError explains a response that carried no content. Reasoning models
// emit their chain of thought on a separate channel, so a short output budget
// can be spent entirely before any answer is produced — which would otherwise
// reach the user as a silent blank turn.
func emptyReplyError(finishReason string, reasoned bool) error {
	switch {
	case reasoned && finishReason == "length":
		return fmt.Errorf("backend produced only reasoning and hit the output limit before answering; " +
			"raise max_output_tokens on the CHAT declaration, or run the backend with thinking disabled")
	case finishReason == "length":
		return fmt.Errorf("backend hit the output limit before producing any content; " +
			"raise max_output_tokens on the CHAT declaration")
	default:
		return fmt.Errorf("backend returned an empty reply (finish reason %q)", finishReason)
	}
}

// do encodes and sends a request, returning the response for a 2xx status or a
// bounded, secret-free error otherwise.
func (c *Client) do(ctx context.Context, cr CompletionRequest, stream bool) (*http.Response, error) {
	body, err := json.Marshal(toWireRequest(cr, stream))
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	c.auth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return nil, cause
		}
		return nil, fmt.Errorf("request to backend failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("backend returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return resp, nil
}

func (c *Client) auth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type wireRequest struct {
	Model         string         `json:"model,omitempty"`
	Messages      []wireMessage  `json:"messages"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	TopP          *float64       `json:"top_p,omitempty"`
	MaxTokens     int            `json:"max_tokens,omitempty"`
	Seed          *int           `json:"seed,omitempty"`
	Stop          []string       `json:"stop,omitempty"`
	// The two runtimes spell the repetition penalty differently — llama.cpp
	// reads repeat_penalty, mlx_lm reads repetition_penalty — and each ignores
	// unknown body keys, so the one value is sent under both names rather than
	// making the transport aware of which backend it is talking to.
	RepeatPenalty     *float64 `json:"repeat_penalty,omitempty"`
	RepetitionPenalty *float64 `json:"repetition_penalty,omitempty"`
	// ChatTemplateKwargs are arguments for the backend's chat-template
	// rendering. It is how a reasoning model's thinking is toggled per request.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
}

// streamOptions asks an OpenAI-compatible backend to emit a final usage chunk
// while streaming, so token accounting is available even in streamed responses.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

func toWireRequest(cr CompletionRequest, stream bool) wireRequest {
	msgs := make([]wireMessage, len(cr.Messages))
	for i, m := range cr.Messages {
		msgs[i] = wireMessage{Role: string(m.Role), Content: m.Content}
	}
	req := wireRequest{
		Model:             cr.Model,
		Messages:          msgs,
		Stream:            stream,
		Temperature:       cr.Sampling.Temperature,
		TopP:              cr.Sampling.TopP,
		MaxTokens:         cr.Sampling.MaxOutputTokens,
		Seed:              cr.Sampling.Seed,
		RepeatPenalty:     cr.Sampling.RepeatPenalty,
		RepetitionPenalty: cr.Sampling.RepeatPenalty,
		Stop:              cr.Sampling.Stop,
	}
	if stream {
		req.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	// enable_thinking is the convention Qwen-family templates read, and the key
	// mlx_lm and llama.cpp both forward to the tokenizer. A backend whose
	// template ignores it is unaffected.
	if cr.Sampling.Thinking != nil {
		req.ChatTemplateKwargs = map[string]any{"enable_thinking": *cr.Sampling.Thinking}
	}
	return req
}

type usageJSON struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (u usageJSON) toUsage() Usage {
	return Usage(u)
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
			// Reasoning carries a reasoning model's chain of thought, which is
			// not part of the reply.
			Reasoning string `json:"reasoning"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usageJSON `json:"usage"`
}

type completionResponse struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning"`
		} `json:"message"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage usageJSON `json:"usage"`
}
