// Package gate is the V3RDE food gate: a vision encoder and a logistic
// head on CPU, before any cloud call. A photo that is not food costs $0.
//
// V3RDE #30 (Oct 29 2026): this package is written new for V3RDE. The route,
// the answer, the events line, the preprocess (bit for bit the same as
// Pillow) and the rules are kept the same as the gate service of the app
// before the Hackathon, with the Hackathon parts (the head's "preprocess"
// block and the shadow gate, entry 3). The default model is PE-Core-S16-384
// (Meta, Apache-2.0), the only gate since entry 7.
//
// The rules:
//   - a photo passes only when p > threshold (strictly);
//   - the embedding must have unit norm (|v| in [0.99, 1.01]), else the
//     answer is UNAVAILABLE: the gate fails closed;
//   - UNAVAILABLE is checked first, so "never ran" cannot read as NOT_FOOD;
//   - p is a stable sigmoid, added up in float64.
package gate

import (
	"encoding/json"
	"fmt"
	"math"
)

// The verdicts.
const (
	Food        = "FOOD"
	NotFood     = "NOT_FOOD"
	Unavailable = "UNAVAILABLE"
)

// The unit-norm band of a good embedding.
const normLow, normHigh = 0.99, 1.01

// Head is the head JSON: what it was fitted on, the threshold and the
// coefficients.
type Head struct {
	Encoder   string    `json:"encoder"`
	Precision string    `json:"precision"`
	InputSide int       `json:"input_side"`
	Dim       int       `json:"embedding_dim"`
	Threshold float64   `json:"threshold"`
	Intercept float64   `json:"intercept"`
	Coef      []float64 `json:"coef"`
	// Preprocess is the preprocess the head was fitted on (entry 3); a head
	// without it is the first gate's: Pillow bilinear, values in [0, 1].
	Preprocess *PreprocessSpec `json:"preprocess,omitempty"`
}

// Spec is the preprocess of the head.
func (h *Head) Spec() PreprocessSpec {
	if h.Preprocess != nil {
		return *h.Preprocess
	}
	return PreprocessSpec{Geometry: GeometryCropFirst, Resample: ResampleBilinear, Std: [3]float32{1, 1, 1}, Layout: "NHWC"}
}

// ParseHead reads a head and refuses one that does not agree with itself:
// the coefficient count must be the declared dimension, the sizes must be
// above 0 and the preprocess must be one this gate can do exactly.
func ParseHead(b []byte) (*Head, error) {
	var h Head
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, err
	}
	if len(h.Coef) != h.Dim {
		return nil, fmt.Errorf("head has %d coefficients but declares %d dims", len(h.Coef), h.Dim)
	}
	if h.InputSide <= 0 || h.Dim <= 0 {
		return nil, fmt.Errorf("head declares input_side %d, dim %d", h.InputSide, h.Dim)
	}
	if h.Preprocess != nil {
		if err := h.Preprocess.Validate(); err != nil {
			return nil, err
		}
	}
	return &h, nil
}

// L2 is the length of v, added up in float64.
func L2(v []float32) float64 {
	sum := 0.0
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}

// Probability is sigmoid(intercept + coef . v), in the form that does not
// overflow for a large |z| on either side.
func (h *Head) Probability(v []float32) (float64, error) {
	if len(v) != h.Dim {
		return 0, fmt.Errorf("embedding is %d, head expects %d", len(v), h.Dim)
	}
	z := h.Intercept
	for i, x := range v {
		z += h.Coef[i] * float64(x)
	}
	if z >= 0 {
		return 1 / (1 + math.Exp(-z)), nil
	}
	e := math.Exp(z)
	return e / (1 + e), nil
}

// Verdict scores an embedding: the label, p, the norm and why. p is -1 and
// why is set only for UNAVAILABLE.
func (h *Head) Verdict(v []float32) (label string, p, norm float64, why string) {
	norm = L2(v)
	if norm < normLow || norm > normHigh {
		return Unavailable, -1, norm, fmt.Sprintf("embedding not unit norm: %.5f", norm)
	}
	p, err := h.Probability(v)
	if err != nil {
		return Unavailable, -1, norm, err.Error()
	}
	if p > h.Threshold {
		return Food, p, norm, ""
	}
	return NotFood, p, norm, ""
}
