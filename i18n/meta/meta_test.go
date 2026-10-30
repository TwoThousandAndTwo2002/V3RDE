package meta

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const key = "meta-test-key-xyz"

// The request is the Responses API body of the stack (model, one user message
// with input_text, reasoning effort); the answer text comes from output[].content[].text;
// 429 is tried again; the key never shows in an error.
func TestAsk(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(401)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(b, &body)
		if body["model"] != Model || body["reasoning"].(map[string]any)["effort"] != "low" {
			t.Errorf("body %s", b)
		}
		in := body["input"].([]any)[0].(map[string]any)
		txt := in["content"].([]any)[0].(map[string]any)["text"].(string)
		if in["role"] != "user" || !strings.Contains(txt, "RULES") || !strings.Contains(txt, "DATA") {
			t.Errorf("input %v", in)
		}
		if n == 1 {
			w.WriteHeader(429)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": " {\"verdict\":\"pass\"} "}}}}})
	}))
	defer srv.Close()
	c := New(srv.URL, key)
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	got, err := c.Ask(context.Background(), "RULES", "DATA")
	if err != nil || got != `{"verdict":"pass"}` || calls != 2 {
		t.Fatalf("%q %v calls %d", got, err, calls)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		io.WriteString(w, "bad key "+key)
	}))
	defer bad.Close()
	c2 := New(bad.URL, key)
	if _, err := c2.Ask(context.Background(), "a", "b"); err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("error %v", err)
	}
}

// The key file must have mode 0600.
func TestLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "meta_model_api_key")
	os.WriteFile(p, []byte(key+"\n"), 0o644)
	if _, err := Load(p); err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("0644: %v", err)
	}
	os.Chmod(p, 0o600)
	if c, err := Load(p); err != nil || c.Key != key {
		t.Fatalf("0600: %v", err)
	}
}
