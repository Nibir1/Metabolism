package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nibir1/metabolism/ports"
)

func TestOllamaRequestAndResponse(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"message":{"content":"{\"ok\":true}"},"prompt_eval_count":120,"eval_count":7}`))
	}))
	defer srv.Close()

	o := Ollama{BaseURL: srv.URL + "/", Model: "qwen3:8b"}
	resp, err := o.Complete(context.Background(), ports.ModelRequest{System: "sys", User: "hi", JSON: true, MaxTokens: 50, DisableThinking: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != `{"ok":true}` || resp.InputTokens != 120 || resp.OutputTokens != 7 {
		t.Fatalf("response %+v", resp)
	}
	if got["model"] != "qwen3:8b" || got["format"] != "json" || got["stream"] != false || got["think"] != false {
		t.Fatalf("request %+v", got)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
		t.Fatalf("messages %+v", msgs)
	}
	if o.Name() != "ollama/qwen3:8b" {
		t.Fatal(o.Name())
	}
}

func TestOllamaReportsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":"model 'nope' not found"}`))
	}))
	defer srv.Close()
	_, err := Ollama{BaseURL: srv.URL, Model: "nope"}.Complete(context.Background(), ports.ModelRequest{User: "x"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenAICompatibleRequestAndKeyHandling(t *testing.T) {
	const key = "sk-test-SECRET-1234567890"
	var auth string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "wrong path", 404)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello","reasoning_content":"thinking"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`))
	}))
	defer srv.Close()

	d := DeepSeek("", key)
	d.BaseURL = srv.URL
	resp, err := d.Complete(context.Background(), ports.ModelRequest{User: "hi", JSON: true, DisableThinking: true})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer "+key || resp.Text != "hello" || resp.InputTokens != 10 || resp.FinishReason != "stop" || !resp.Reasoned {
		t.Fatalf("auth %q, resp %+v", auth, resp)
	}
	if got["thinking"].(map[string]any)["type"] != "disabled" {
		t.Fatalf("thinking not disabled: %+v", got)
	}
	if got["model"] != "deepseek-flash" || got["response_format"].(map[string]any)["type"] != "json_object" {
		t.Fatalf("request %+v", got)
	}
}

// The key must never surface in an error, whatever the server sends back.
func TestErrorsNeverCarryTheKey(t *testing.T) {
	const key = "sk-test-SECRET-1234567890"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"Authentication Fails, invalid key"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	d := DeepSeek("deepseek-v4-pro", key)
	d.BaseURL = srv.URL
	_, err := d.Complete(context.Background(), ports.ModelRequest{User: "hi"})
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 401 {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("the error carries the key: %v", err)
	}
	if _, err := DeepSeek("", "").Complete(context.Background(), ports.ModelRequest{User: "x"}); err == nil {
		t.Fatal("a call without a key was attempted")
	}
}

func TestIsLoopback(t *testing.T) {
	for raw, want := range map[string]bool{
		"http://127.0.0.1:11434":    true,
		"http://localhost:11434":    true,
		"http://[::1]:11434":        true,
		"http://192.168.1.20:11434": false,
		"https://api.deepseek.com":  false,
		"127.0.0.1:11434":           false,
		"ftp://localhost":           false,
	} {
		if got := IsLoopback(raw); got != want {
			t.Errorf("IsLoopback(%q) = %v, want %v", raw, got, want)
		}
	}
}
