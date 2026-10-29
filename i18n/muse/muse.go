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
	"net/http"
	"os"
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

// New makes a client with the default settings, a 120 s timeout and 3 retries.
func New(base, key string) *Client {
	return &Client{BaseURL: strings.TrimRight(base, "/"), Key: key, Settings: Default(),
		HTTP: &http.Client{Timeout: 120 * time.Second}, Retries: 3, Sleep: sleepCtx}
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

// Chat sends one request (n = 1) and returns the answer text. It tries again
// after 429, 5xx and network errors (Retry-After, else 1 s, 2 s, 4 s).
func (c *Client) Chat(ctx context.Context, system, user string) (Reply, error) {
	body, err := json.Marshal(request{Settings: c.Settings, Messages: []message{{"system", system}, {"user", user}}})
	if err != nil {
		return Reply{}, err
	}
	var last error
	for try := 0; try <= c.Retries; try++ {
		r, wait, err := c.once(ctx, body)
		if err == nil {
			return r, nil
		}
		last = c.redact(err)
		if !retryable(err) || try == c.Retries {
			break
		}
		if wait == 0 {
			wait = time.Duration(1<<try) * time.Second
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
