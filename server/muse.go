package analyze

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultUpstream and DefaultModel: https://dev.meta.ai/docs/image-understanding.
const (
	DefaultUpstream = "https://api.meta.ai/v1/responses"
	DefaultModel    = "muse-spark-1.3"
)

// maxReply is the largest Meta reply the server reads.
const maxReply = 4 << 20

// Usage is the token count the API returns, kept for the metrics line.
type Usage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	ReasoningTokens int `json:"reasoning_tokens"`
}

// Upstream calls the Meta Model API. Key is read one time at the start and
// never logged. URL and Model are flags, so a test can use a fake.
type Upstream struct {
	URL    string
	Key    string
	Model  string
	Effort string // reasoning.effort; "" = the API default
	HTTP   *http.Client
}

// Answer is one round trip to the API, raw and parsed.
type Answer struct {
	Text      string
	Usage     Usage
	Status    int
	Elapsed   time.Duration
	Parsed    map[string]any
	Result    Result
	Valid     bool
	ParseErr  error
	RequestID string // the API's own id, for a support ticket
}

// Request is the /v1/responses body: one user message with the prompt and
// the photo as a data: URL.
func (u *Upstream) Request(image []byte, mime string) map[string]any {
	body := map[string]any{
		"model": u.Model,
		"input": []any{map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": Prompt},
				map[string]any{"type": "input_image",
					"image_url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image)},
			},
		}},
	}
	if u.Effort != "" {
		body["reasoning"] = map[string]any{"effort": u.Effort}
	}
	return body
}

// ResponseText is the model's text: output_text when the API gives it, else
// every output[].content[].text joined with new lines.
func ResponseText(resp map[string]any) string {
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

// usageOf reads the usage block; a missing number stays 0.
func usageOf(resp map[string]any) Usage {
	num := func(m map[string]any, k string) int {
		f, _ := m[k].(float64)
		return int(f)
	}
	u, _ := resp["usage"].(map[string]any)
	details, _ := u["output_tokens_details"].(map[string]any)
	return Usage{InputTokens: num(u, "input_tokens"), OutputTokens: num(u, "output_tokens"),
		ReasoningTokens: num(details, "reasoning_tokens")}
}

// Analyze sends one photo and returns the checked answer. An API status
// that is not 2xx, a reply that is not JSON and a reply that fails the
// contract are errors (the handler answers 502 with the request id).
func (u *Upstream) Analyze(ctx context.Context, image []byte, mime string) (Answer, error) {
	var a Answer
	payload, err := json.Marshal(u.Request(image, mime))
	if err != nil {
		return a, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.URL, bytes.NewReader(payload))
	if err != nil {
		return a, err
	}
	req.Header.Set("Authorization", "Bearer "+u.Key)
	req.Header.Set("Content-Type", "application/json")
	client := u.HTTP
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		a.Elapsed = time.Since(start)
		return a, fmt.Errorf("upstream: %w", err)
	}
	defer resp.Body.Close()
	a.Status = resp.StatusCode
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	a.Elapsed = time.Since(start)
	if err != nil {
		return a, fmt.Errorf("upstream read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := raw
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return a, fmt.Errorf("upstream: HTTP %d %s", resp.StatusCode, msg)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return a, fmt.Errorf("upstream: bad JSON: %w", err)
	}
	a.RequestID, _ = body["id"].(string)
	a.Usage = usageOf(body)
	a.Text = ResponseText(body)
	if a.Parsed, a.ParseErr = ExtractJSON(a.Text); a.ParseErr != nil {
		return a, fmt.Errorf("contract: %w", a.ParseErr)
	}
	if a.Result, a.ParseErr = Validate(a.Parsed); a.ParseErr != nil {
		return a, a.ParseErr
	}
	a.Valid = true
	return a, nil
}
