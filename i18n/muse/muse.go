// Package muse calls Muse Spark Open on Baseten (OpenAI-compatible chat
// completions) with fixed settings, and makes the translation prompt (L4b).
package muse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Model is the one model name that the hackathon key works with.
const Model = "meta-models/muse-spark-open-1026"

// BaseURL is Baseten's OpenAI-compatible endpoint.
const BaseURL = "https://inference.baseten.co/v1"

// Settings are the request settings. Defaults: Meta's recommended sampling
// (temperature 1.0, top_p 1.0, top_k off; Baseten's own top_k default is 50,
// -1 turns it off) and reasoning_effort low (L4b D2).
type Settings struct {
	Model           string  `json:"model"`
	Temperature     float64 `json:"temperature"`
	TopP            float64 `json:"top_p"`
	TopK            int     `json:"top_k"`
	ReasoningEffort string  `json:"reasoning_effort"`
	MaxTokens       int     `json:"max_tokens"`
}

// Default returns the fixed settings.
func Default() Settings {
	return Settings{Model: Model, Temperature: 1.0, TopP: 1.0, TopK: -1, ReasoningEffort: "low", MaxTokens: 4096}
}

// Client sends chat requests.
type Client struct {
	BaseURL  string
	Key      string // never printed: errors show it as "***"
	Settings Settings
	HTTP     *http.Client
	Retries  int                                        // extra tries for 429, 5xx and network errors
	Sleep    func(context.Context, time.Duration) error // for tests
	Pace     *Pacer                                     // steady rate under the RPM limit (nil: none)
	Jitter   func() time.Duration                       // added to each backoff wait
}

// KeyFile is where the Baseten key is kept: $V3RDE_CFG/baseten_api_key, else
// ~/.config/v3rde/baseten_api_key, next to the Meta key of the stack (a file
// with mode 0600, made by tools/lif_baseten_key.sh). "" when there is no home.
func KeyFile() string {
	if d := os.Getenv("V3RDE_CFG"); d != "" {
		return filepath.Join(d, "baseten_api_key")
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".config", "v3rde", "baseten_api_key")
}

// Load makes a client with the key from keyFile (mode 0600, not empty), or, when
// there is no such file, from BASETEN_API_KEY. BASETEN_BASE_URL, if set, is the endpoint.
func Load(keyFile string) (*Client, error) {
	key, err := readKey(keyFile)
	if err != nil {
		return nil, err
	}
	if key == "" {
		key = os.Getenv("BASETEN_API_KEY")
	}
	if key == "" {
		return nil, fmt.Errorf("no Baseten key: run tools/lif_baseten_key.sh once (it asks for the key and writes %s, mode 0600)", keyFile)
	}
	base := os.Getenv("BASETEN_BASE_URL")
	if base == "" {
		base = BaseURL
	}
	return New(base, key), nil
}

// readKey returns the key in path, or "" when there is no file.
func readKey(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if fi.Mode().Perm() != 0o600 {
		return "", fmt.Errorf("%s must be mode 0600 (it is %04o): chmod 600 %s", path, fi.Mode().Perm(), path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(b))
	if key == "" || strings.ContainsAny(key, " \t\r\n") {
		return "", fmt.Errorf("%s must hold the key only, on one line", path)
	}
	return key, nil
}

// FromEnv makes a client from BASETEN_API_KEY (and BASETEN_BASE_URL, if set).
func FromEnv() (*Client, error) {
	key := os.Getenv("BASETEN_API_KEY")
	if key == "" {
		return nil, errors.New("BASETEN_API_KEY is not set")
	}
	base := os.Getenv("BASETEN_BASE_URL")
	if base == "" {
		base = BaseURL
	}
	return New(base, key), nil
}

// New makes a client with the default settings, a 120 s timeout, 6 retries and a pacer.
func New(base, key string) *Client {
	return &Client{BaseURL: strings.TrimRight(base, "/"), Key: key, Settings: Default(),
		HTTP: &http.Client{Timeout: 120 * time.Second}, Retries: 6, Sleep: sleepCtx, Pace: NewPacer(),
		Jitter: func() time.Duration { return time.Duration(rand.Int64N(int64(time.Second))) }}
}

// Backoff is the wait before try+1 when the answer gives no Retry-After:
// 1 s, 2 s, 4 s ... up to 32 s (6 waits: 63 s, more than one minute of limit).
func Backoff(try int) time.Duration {
	if try > 5 {
		try = 5
	}
	return time.Duration(1<<try) * time.Second
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Reply is one answer.
type Reply struct {
	Text             string
	FinishReason     string
	ReasoningTokens  int
	CompletionTokens int
	PromptTokens     int
}

// ErrNoAnswer: the model gave no text (for example, the reasoning used all the tokens).
var ErrNoAnswer = errors.New("the model gave no answer text")

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Settings
	Messages []message `json:"messages"`
}

type response struct {
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		Details          struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

// HTTPError is a non-200 answer.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("baseten: HTTP %d: %s", e.Status, e.Body) }

func retryable(err error) bool {
	var h *HTTPError
	if errors.As(err, &h) {
		return h.Status == 429 || h.Status >= 500
	}
	return !errors.Is(err, ErrNoAnswer) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// Chat sends one request (n = 1) and returns the answer text. Each try waits for
// its slot of the pacer. It tries again after 429, 5xx and network errors
// (Retry-After, else Backoff and a jitter).
func (c *Client) Chat(ctx context.Context, system, user string) (Reply, error) {
	body, err := json.Marshal(request{Settings: c.Settings, Messages: []message{{"system", system}, {"user", user}}})
	if err != nil {
		return Reply{}, err
	}
	var last error
	for try := 0; try <= c.Retries; try++ {
		if c.Pace != nil {
			if err := c.Pace.Wait(ctx); err != nil {
				return Reply{}, err
			}
		}
		r, wait, err := c.once(ctx, body)
		if err == nil {
			return r, nil
		}
		last = c.redact(err)
		if !retryable(err) || try == c.Retries {
			break
		}
		if wait == 0 {
			wait = Backoff(try)
			if c.Jitter != nil {
				wait += c.Jitter()
			}
		}
		if e := c.Sleep(ctx, wait); e != nil {
			return Reply{}, e
		}
	}
	return Reply{}, last
}

func (c *Client) redact(err error) error {
	if c.Key == "" || !strings.Contains(err.Error(), c.Key) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), c.Key, "***"))
}

func (c *Client) once(ctx context.Context, body []byte) (Reply, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Reply{}, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Key)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Reply{}, 0, err
	}
	defer res.Body.Close()
	if c.Pace != nil {
		c.Pace.fromHeader(res.Header.Get("x-ratelimit-limit-requests"))
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return Reply{}, 0, err
	}
	if res.StatusCode != 200 {
		var wait time.Duration
		if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s >= 0 && s <= 120 {
			wait = time.Duration(s) * time.Second
		}
		b := string(raw)
		if len(b) > 300 {
			b = b[:300]
		}
		return Reply{}, wait, &HTTPError{Status: res.StatusCode, Body: b}
	}
	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return Reply{}, 0, fmt.Errorf("baseten: bad JSON: %v", err)
	}
	if len(r.Choices) != 1 {
		return Reply{}, 0, fmt.Errorf("baseten: %d choices, want 1", len(r.Choices))
	}
	ch := r.Choices[0]
	out := Reply{FinishReason: ch.FinishReason, ReasoningTokens: r.Usage.Details.ReasoningTokens,
		CompletionTokens: r.Usage.CompletionTokens, PromptTokens: r.Usage.PromptTokens}
	if ch.Message.Content == nil || strings.TrimSpace(*ch.Message.Content) == "" || ch.FinishReason == "length" {
		return out, 0, fmt.Errorf("%w (finish_reason %q, %d reasoning tokens)", ErrNoAnswer, ch.FinishReason, out.ReasoningTokens)
	}
	out.Text = strings.TrimSpace(*ch.Message.Content)
	return out, 0, nil
}
