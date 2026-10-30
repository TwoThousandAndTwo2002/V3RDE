// Package meta calls Muse Spark 1.3 through the Meta Model API (the Responses
// API at https://api.meta.ai/v1/responses), the model the services stack
// uses. It is the second judge (L4j): another model than the translator, so the
// translator does not judge its own output.
package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"v3rde/i18n/muse"
)

// URL and Model of the Meta Model API, as in the services stack.
const (
	URL   = "https://api.meta.ai/v1/responses"
	Model = "muse-spark-1.3"
)

// Client sends one prompt and returns the text of the answer.
type Client struct {
	URL, Key, Model, Effort string
	HTTP                    *http.Client
	Retries                 int
	Sleep                   func(context.Context, time.Duration) error
}

// KeyFile is $V3RDE_CFG/meta_model_api_key, else ~/.config/v3rde/meta_model_api_key:
// the key file of the stack (mode 0600).
func KeyFile() string {
	if d := os.Getenv("V3RDE_CFG"); d != "" {
		return filepath.Join(d, "meta_model_api_key")
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".config", "v3rde", "meta_model_api_key")
}

// Load reads the key (the file must have mode 0600 and hold only the key).
func Load(keyFile string) (*Client, error) {
	fi, err := os.Stat(keyFile)
	if err != nil {
		return nil, fmt.Errorf("no Meta Model API key at %s (the stack's meta_model_api_key, mode 0600)", keyFile)
	}
	if fi.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("%s must be mode 0600 (it is %04o)", keyFile, fi.Mode().Perm())
	}
	b, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(string(b))
	if key == "" || strings.ContainsAny(key, " \t\r\n") {
		return nil, fmt.Errorf("%s must hold the key only, on one line", keyFile)
	}
	url := os.Getenv("META_MODEL_API_URL") // a fake, for a test
	if url == "" {
		url = URL
	}
	return New(url, key), nil
}

// New makes a client: Muse Spark 1.3, reasoning effort low, 120 s timeout, 6 retries.
func New(url, key string) *Client {
	return &Client{URL: url, Key: key, Model: Model, Effort: "low", HTTP: &http.Client{Timeout: 120 * time.Second}, Retries: 6,
		Sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}}
}

// Name is the model name.
func (c *Client) Name() string { return c.Model }

// HTTPError is an answer that is not 2xx.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("meta: HTTP %d: %s", e.Status, e.Body) }

// Ask sends the instructions and the input as one user message and returns the
// text of the answer. 429, 5xx and network errors are tried again (Retry-After,
// else 1 s ... 32 s).
func (c *Client) Ask(ctx context.Context, instructions, input string) (string, error) {
	body := map[string]any{
		"model": c.Model,
		"input": []any{map[string]any{"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": instructions + "\n\n" + input}}}},
	}
	if c.Effort != "" {
		body["reasoning"] = map[string]any{"effort": c.Effort}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	var last error
	for try := 0; try <= c.Retries; try++ {
		text, wait, err := c.once(ctx, payload)
		if err == nil {
			return text, nil
		}
		last = c.redact(err)
		var h *HTTPError
		retry := !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
		if errors.As(err, &h) {
			retry = h.Status == 429 || h.Status >= 500
		}
		if !retry || try == c.Retries {
			break
		}
		if wait == 0 {
			wait = muse.Backoff(try)
		}
		if e := c.Sleep(ctx, wait); e != nil {
			return "", e
		}
	}
	return "", last
}

func (c *Client) redact(err error) error {
	if c.Key == "" || !strings.Contains(err.Error(), c.Key) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), c.Key, "***"))
}

func (c *Client) once(ctx context.Context, payload []byte) (string, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(payload))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return "", 0, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var wait time.Duration
		if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s >= 0 && s <= 120 {
			wait = time.Duration(s) * time.Second
		}
		b := string(raw)
		if len(b) > 300 {
			b = b[:300]
		}
		return "", wait, &HTTPError{Status: res.StatusCode, Body: b}
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", 0, fmt.Errorf("meta: bad JSON: %v", err)
	}
	text := strings.TrimSpace(Text(resp))
	if text == "" {
		return "", 0, errors.New("meta: the answer has no text")
	}
	return text, 0, nil
}

// Text is output_text, else every output[].content[].text joined (as the services stack reads it).
func Text(resp map[string]any) string {
	if s, _ := resp["output_text"].(string); s != "" {
		return s
	}
	var parts []string
	items, _ := resp["output"].([]any)
	for _, it := range items {
		item, _ := it.(map[string]any)
		content, _ := item["content"].([]any)
		for _, c := range content {
			part, _ := c.(map[string]any)
			if t, ok := part["text"].(string); ok {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "\n")
}
