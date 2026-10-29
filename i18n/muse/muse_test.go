package muse

import (
	"context"
	"encoding/json"
	"errors"
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

const key = "test-key-not-real-123"

func fake(t *testing.T, h func(w http.ResponseWriter, body map[string]any)) (*Client, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(401)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(b, &body); err != nil {
			t.Error(err)
		}
		h(w, body)
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL+"/v1", key)
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	c.Pace = nil
	return c, &calls
}

func answer(w http.ResponseWriter, content any, finish string) {
	json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content, "reasoning_content": "thinking"}, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 150, "completion_tokens": 200, "completion_tokens_details": map[string]any{"reasoning_tokens": 190}},
	})
}

// The request has the exact model, Meta's sampling, top_k -1, effort low,
// and nothing that Baseten rejects or ignores (n, logprobs, seed).
func TestRequestSettings(t *testing.T) {
	c, _ := fake(t, func(w http.ResponseWriter, b map[string]any) {
		want := map[string]any{"model": Model, "temperature": 1.0, "top_p": 1.0, "top_k": -1.0, "reasoning_effort": "low", "max_tokens": 4096.0}
		for k, v := range want {
			if b[k] != v {
				t.Errorf("%s = %v, want %v", k, b[k], v)
			}
		}
		for _, k := range []string{"n", "logprobs", "top_logprobs", "prompt_logprobs", "seed", "response_format"} {
			if _, ok := b[k]; ok {
				t.Errorf("%s must not be sent", k)
			}
		}
		msgs := b["messages"].([]any)
		if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
			t.Errorf("messages %v", msgs)
		}
		answer(w, " ¿Vienes? ", "stop")
	})
	r, err := c.Chat(context.Background(), "sys", "user")
	if err != nil || r.Text != "¿Vienes?" || r.ReasoningTokens != 190 {
		t.Fatalf("%+v %v", r, err)
	}
}

// 429 and 500 are tried again; 400 is not.
func TestRetries(t *testing.T) {
	var n int32
	c, calls := fake(t, func(w http.ResponseWriter, b map[string]any) {
		switch atomic.AddInt32(&n, 1) {
		case 1:
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(429)
		case 2:
			w.WriteHeader(500)
		default:
			answer(w, "Hola", "stop")
		}
	})
	if r, err := c.Chat(context.Background(), "s", "u"); err != nil || r.Text != "Hola" || *calls != 3 {
		t.Fatalf("%+v %v calls %d", r, err, *calls)
	}
	c2, calls2 := fake(t, func(w http.ResponseWriter, b map[string]any) { w.WriteHeader(400) })
	var h *HTTPError
	if _, err := c2.Chat(context.Background(), "s", "u"); !errors.As(err, &h) || h.Status != 400 || *calls2 != 1 {
		t.Fatalf("400: %v, %d calls", err, *calls2)
	}
}

// No answer text (null content, or the reasoning used all the tokens) is
// ErrNoAnswer, not an empty translation, and it is not tried again here.
func TestNoAnswer(t *testing.T) {
	for _, c := range []struct {
		content any
		finish  string
	}{{nil, "length"}, {"", "stop"}, {"partial", "length"}} {
		cl, calls := fake(t, func(w http.ResponseWriter, b map[string]any) { answer(w, c.content, c.finish) })
		if _, err := cl.Chat(context.Background(), "s", "u"); !errors.Is(err, ErrNoAnswer) || *calls != 1 {
			t.Errorf("%v/%s: %v, %d calls", c.content, c.finish, err, *calls)
		}
	}
}

// The key is never in an error message.
func TestKeyNotInErrors(t *testing.T) {
	c, _ := fake(t, func(w http.ResponseWriter, b map[string]any) {
		w.WriteHeader(500)
		io.WriteString(w, `{"error":"bad key `+key+`"}`)
	})
	c.Retries = 0
	_, err := c.Chat(context.Background(), "s", "u")
	if err == nil || strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "***") {
		t.Fatalf("error %v", err)
	}
	t.Setenv("BASETEN_API_KEY", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("no key must fail")
	}
}

// The prompt names the language and the tag rule, and gives the id as context.
func TestPrompt(t *testing.T) {
	s, u := Prompt("Spanish (Latin America)", "es-419", "CAPTURE.retake", "Retake")
	for _, w := range []string{"Spanish (Latin America) (es-419)", "<x1/>", "translation only"} {
		if !strings.Contains(s, w) {
			t.Errorf("system has no %q", w)
		}
	}
	if u != "Message id: CAPTURE.retake\nText:\nRetake" {
		t.Errorf("user %q", u)
	}
}

// The key comes from the key file (mode 0600 only), else from BASETEN_API_KEY;
// with neither, the error names the setup script and not a key.
func TestKeyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "baseten_api_key")
	t.Setenv("BASETEN_API_KEY", "")
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "tools/lif_baseten_key.sh") {
		t.Fatalf("no file, no env: %v", err)
	}
	t.Setenv("BASETEN_API_KEY", "env-key")
	if c, err := Load(p); err != nil || c.Key != "env-key" {
		t.Fatalf("env: %v", err)
	}
	os.WriteFile(p, []byte(key+"\n"), 0o600)
	if c, err := Load(p); err != nil || c.Key != key {
		t.Fatalf("file: %v", err)
	}
	os.Chmod(p, 0o644)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "0600") || strings.Contains(err.Error(), key) {
		t.Fatalf("0644 must fail without the key in the error: %v", err)
	}
	os.WriteFile(p, []byte("two words"), 0o600)
	os.Chmod(p, 0o600)
	if _, err := Load(p); err == nil {
		t.Fatal("a file with a space must fail")
	}
	t.Setenv("V3RDE_CFG", dir)
	if KeyFile() != p {
		t.Fatalf("KeyFile %s", KeyFile())
	}
}

// After 429 with no Retry-After the waits are 1, 2, 4, 8, 16, 32 s (plus the
// jitter): 6 retries, 7 tries, more than one minute in all.
func TestBackoff(t *testing.T) {
	c, calls := fake(t, func(w http.ResponseWriter, b map[string]any) { w.WriteHeader(429) })
	var waits []time.Duration
	c.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	c.Jitter = func() time.Duration { return 0 }
	if _, err := c.Chat(context.Background(), "s", "u"); err == nil || *calls != 7 {
		t.Fatalf("%v, %d calls", err, *calls)
	}
	want := []time.Duration{1, 2, 4, 8, 16, 32}
	var sum time.Duration
	for i, w := range want {
		if waits[i] != w*time.Second {
			t.Fatalf("waits %v", waits)
		}
		sum += waits[i]
	}
	if len(waits) != 6 || sum <= time.Minute {
		t.Fatalf("waits %v", waits)
	}
}

// The pacer takes the limit from x-ratelimit-limit-requests and spaces the
// requests evenly; SetRPM fixes it.
func TestPacer(t *testing.T) {
	c, _ := fake(t, func(w http.ResponseWriter, b map[string]any) {
		w.Header().Set("x-ratelimit-limit-requests", "30")
		answer(w, "Hola", "stop")
	})
	now := time.Unix(1000, 0)
	var waits []time.Duration
	p := NewPacer()
	p.Now = func() time.Time { return now }
	p.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	c.Pace = p
	// call 1 has no limit yet; call 2 takes the slot that call 1 left; then 2 s apart
	for i := 0; i < 4; i++ {
		if _, err := c.Chat(context.Background(), "s", "u"); err != nil {
			t.Fatal(err)
		}
	}
	if p.RPM() != 30 || len(waits) != 2 || waits[0] != 2*time.Second || waits[1] != 4*time.Second {
		t.Fatalf("rpm %d waits %v", p.RPM(), waits)
	}
	p.SetRPM(15)
	p.fromHeader("120")
	if p.RPM() != 15 {
		t.Fatalf("a fixed limit must stay: %d", p.RPM())
	}
}
