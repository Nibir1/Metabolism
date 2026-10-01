// Package llm adapts chat models to ports.Model: Ollama (local) and any
// OpenAI-compatible endpoint (DeepSeek).
//
// It lives under adapters/remote/ because it speaks HTTP, and that is the only
// place in Metabolism allowed to (internal/privacyguard). Whether a model is
// used at all, and whether a remote one may be, is decided in cmd/metabolism.
//
// API keys are read by the caller and passed in; they are never logged,
// never put in an error, and never written anywhere.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/ports"
)

// DefaultTimeout bounds one completion. Reflection over a batch can be slow on
// a local model.
const DefaultTimeout = 5 * time.Minute

// maxBody bounds a response body, so a misbehaving endpoint cannot exhaust
// memory.
const maxBody = 8 << 20

// ---------------------------------------------------------------- Ollama

// Ollama talks to an Ollama server's /api/chat.
type Ollama struct {
	BaseURL string // e.g. http://127.0.0.1:11434
	Model   string // e.g. qwen3:8b
	HTTP    *http.Client
}

// DefaultOllamaURL is where Ollama listens by default.
const DefaultOllamaURL = "http://127.0.0.1:11434"

// Name implements ports.Model.
func (o Ollama) Name() string { return "ollama/" + o.Model }

// Complete implements ports.Model.
func (o Ollama) Complete(ctx context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	body := map[string]any{
		"model":    o.Model,
		"stream":   false,
		"messages": messages(req),
		"options":  map[string]any{"temperature": req.Temperature},
	}
	if req.JSON {
		body["format"] = "json"
	}
	if req.DisableThinking {
		body["think"] = false
	}
	if req.MaxTokens > 0 {
		body["options"].(map[string]any)["num_predict"] = req.MaxTokens
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int    `json:"prompt_eval_count"`
		EvalCount       int    `json:"eval_count"`
		DoneReason      string `json:"done_reason"`
		Error           string `json:"error"`
	}
	if err := post(ctx, client(o.HTTP), strings.TrimRight(o.BaseURL, "/")+"/api/chat", nil, body, &out); err != nil {
		return ports.ModelResponse{}, fmt.Errorf("%s: %w", o.Name(), err)
	}
	if out.Error != "" {
		return ports.ModelResponse{}, fmt.Errorf("%s: %s", o.Name(), out.Error)
	}
	return ports.ModelResponse{Text: out.Message.Content, FinishReason: out.DoneReason,
		InputTokens: out.PromptEvalCount, OutputTokens: out.EvalCount}, nil
}

// ---------------------------------------------------------------- OpenAI-compatible

// OpenAI talks to an OpenAI-compatible /chat/completions endpoint.
type OpenAI struct {
	Provider string // name prefix, e.g. "deepseek"
	BaseURL  string // e.g. https://api.deepseek.com
	Model    string // e.g. deepseek-flash
	APIKey   string
	HTTP     *http.Client
}

// DeepSeek returns an OpenAI-compatible adapter for DeepSeek's API.
func DeepSeek(model, apiKey string) OpenAI {
	if model == "" {
		model = "deepseek-flash"
	}
	return OpenAI{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: model, APIKey: apiKey}
}

// Name implements ports.Model.
func (o OpenAI) Name() string { return o.Provider + "/" + o.Model }

// Complete implements ports.Model.
func (o OpenAI) Complete(ctx context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	if o.APIKey == "" {
		return ports.ModelResponse{}, fmt.Errorf("%s: no API key", o.Name())
	}
	body := map[string]any{
		"model":       o.Model,
		"messages":    messages(req),
		"temperature": req.Temperature,
		"stream":      false,
	}
	if req.JSON {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	if req.DisableThinking {
		// DeepSeek: https://api-docs.deepseek.com/guides/thinking_mode/
		body["thinking"] = map[string]string{"type": "disabled"}
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	headers := map[string]string{"Authorization": "Bearer " + o.APIKey}
	if err := post(ctx, client(o.HTTP), strings.TrimRight(o.BaseURL, "/")+"/chat/completions", headers, body, &out); err != nil {
		return ports.ModelResponse{}, fmt.Errorf("%s: %w", o.Name(), err)
	}
	if len(out.Choices) == 0 {
		return ports.ModelResponse{}, fmt.Errorf("%s: response had no choices", o.Name())
	}
	c := out.Choices[0]
	return ports.ModelResponse{Text: c.Message.Content, FinishReason: c.FinishReason,
		Reasoned:    c.Message.ReasoningContent != "",
		InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens}, nil
}

// ---------------------------------------------------------------- shared

func messages(req ports.ModelRequest) []map[string]string {
	var m []map[string]string
	if req.System != "" {
		m = append(m, map[string]string{"role": "system", "content": req.System})
	}
	return append(m, map[string]string{"role": "user", "content": req.User})
}

func client(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: DefaultTimeout}
}

// StatusError is a non-2xx response. Body is a bounded excerpt, which comes
// from the server and never contains the request's key.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

func post(ctx context.Context, c *http.Client, endpoint string, headers map[string]string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		// A transport error can quote the URL; it never quotes headers.
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		excerpt := strings.TrimSpace(string(data))
		if len(excerpt) > 300 {
			excerpt = excerpt[:300] + "…"
		}
		return &StatusError{Status: resp.StatusCode, Body: excerpt}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// IsLoopback reports whether raw is an http(s) URL whose host is this machine.
// cmd/metabolism uses it to decide whether an Ollama URL counts as local.
func IsLoopback(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ErrNotLocal is returned when a model URL that must be local is not.
var ErrNotLocal = errors.New("model endpoint is not on this machine")
