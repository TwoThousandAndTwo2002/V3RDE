package inference

// A small client for Meta Model API POST /v1/responses: text + image input,
// function tools, and the function_call_output loop (dev.meta.ai/docs
// /tool-calling, /sam/segmenting, READ 2026-10-26). One client serves both
// Muse Spark and SAM 3.1; the model id picks which. The key is read from a
// file by the command and is never logged.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

const (
	DefaultUpstream = analyze.DefaultUpstream // https://api.meta.ai/v1/responses
	DefaultMuse     = analyze.DefaultModel    // muse-spark-1.3
	DefaultSAM      = "sam-3.1"
)

// Client is one API key and one endpoint.
type Client struct {
	URL    string
	Key    string
	HTTP   *http.Client
	Effort string // reasoning effort for Muse; "" = API default
}

// Call is one round trip: what was sent, what came back, and the cost.
type Call struct {
	Model     string        `json:"model"`
	Purpose   string        `json:"purpose"`
	Status    int           `json:"status"`
	MS        int64         `json:"ms"`
	AtMS      int64         `json:"at_ms"`                // X48a: start, ms after the request start (-1: unknown)
	WaitMS    int64         `json:"wait_ms,omitempty"`    // X48a: a Muse call's wait for the food gate
	RequestID string        `json:"request_id,omitempty"` // the API's id
	Usage     analyze.Usage `json:"usage"`
	// CachedTokens: input_tokens_details.cached_tokens (billed at the cached rate).
	CachedTokens int     `json:"cached_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd"`
	// Cached: a probe SAMCache answer; not paid again (production pays it).
	Cached  bool   `json:"cached,omitempty"`
	Error   string `json:"error,omitempty"`
	elapsed time.Duration
	body    map[string]any
}

// Text is the model's text output (output[].content[].text).
func (c *Call) Text() string { return analyze.ResponseText(c.body) }

// FunctionCall is one tool call the model asked for.
type FunctionCall struct {
	CallID    string
	Name      string
	Arguments string
}

// FunctionCalls lists output items of type function_call.
func (c *Call) FunctionCalls() []FunctionCall {
	var out []FunctionCall
	items, _ := c.body["output"].([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m["type"] != "function_call" {
			continue
		}
		fc := FunctionCall{}
		fc.CallID, _ = m["call_id"].(string)
		fc.Name, _ = m["name"].(string)
		fc.Arguments, _ = m["arguments"].(string)
		out = append(out, fc)
	}
	return out
}

// ID is the response id, for previous_response_id.
func (c *Call) ID() string { s, _ := c.body["id"].(string); return s }

// ImagePart is an input_image content part from bytes.
func ImagePart(img []byte, mime string) map[string]any {
	return map[string]any{"type": "input_image",
		"image_url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img)}
}

// TextPart is an input_text content part.
func TextPart(s string) map[string]any { return map[string]any{"type": "input_text", "text": s} }

// UserMessage wraps content parts as the single user message.
func UserMessage(parts ...map[string]any) []any {
	content := make([]any, len(parts))
	for i, p := range parts {
		content[i] = p
	}
	return []any{map[string]any{"type": "message", "role": "user", "content": content}}
}

// Do sends one body. Non-2xx is an error with the first 300 bytes of the
// answer; the Call is returned either way for the event log.
func (cl *Client) Do(ctx context.Context, purpose string, body map[string]any) (*Call, error) {
	c := &Call{Purpose: purpose, AtMS: sinceStart(ctx)}
	c.Model, _ = body["model"].(string)
	tw := time.Now()
	if err := waitGate(ctx, purpose); err != nil { // X48a: a Muse call waits for the gate
		c.WaitMS, c.Error = time.Since(tw).Milliseconds(), err.Error()
		return c, fmt.Errorf("%s: %w", purpose, err)
	}
	if w := time.Since(tw); w >= time.Millisecond {
		c.WaitMS = w.Milliseconds()
		c.AtMS = sinceStart(ctx)
	}
	if cl.Effort != "" && c.Model != DefaultSAM {
		if _, set := body["reasoning"]; !set {
			body["reasoning"] = map[string]any{"effort": cl.Effort}
		}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return c, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cl.URL, bytes.NewReader(b))
	if err != nil {
		return c, err
	}
	req.Header.Set("Authorization", "Bearer "+cl.Key)
	req.Header.Set("Content-Type", "application/json")
	hc := cl.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 90 * time.Second, Transport: MetaTransport}
	}
	t0 := time.Now()
	resp, err := hc.Do(req)
	defer func() { c.elapsed = time.Since(t0); c.MS = c.elapsed.Milliseconds() }()
	if err != nil {
		c.Error = err.Error()
		return c, fmt.Errorf("%s: %w", purpose, err)
	}
	defer resp.Body.Close()
	c.Status = resp.StatusCode
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		c.Error = err.Error()
		return c, fmt.Errorf("%s read: %w", purpose, err)
	}
	if resp.StatusCode/100 != 2 {
		msg := string(raw)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		c.Error = fmt.Sprintf("HTTP %d %s", resp.StatusCode, msg)
		return c, fmt.Errorf("%s: %s", purpose, c.Error)
	}
	if err := json.Unmarshal(raw, &c.body); err != nil {
		c.Error = "bad JSON"
		return c, fmt.Errorf("%s: bad JSON: %w", purpose, err)
	}
	c.RequestID, _ = c.body["id"].(string)
	um, _ := c.body["usage"].(map[string]any)
	if v, ok := um["input_tokens"].(float64); ok {
		c.Usage.InputTokens = int(v)
	}
	if v, ok := um["output_tokens"].(float64); ok {
		c.Usage.OutputTokens = int(v)
	}
	if d, ok := um["input_tokens_details"].(map[string]any); ok {
		if v, ok := d["cached_tokens"].(float64); ok {
			c.CachedTokens = int(v)
		}
	}
	if d, ok := um["output_tokens_details"].(map[string]any); ok {
		if v, ok := d["reasoning_tokens"].(float64); ok {
			c.Usage.ReasoningTokens = int(v)
		}
	}
	c.CostUSD = c.Cost()
	return c, nil
}
