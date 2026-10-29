//go:build litert

package gate

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// The tiny model (testdata/tiny.tflite: 8x8x3 float32 in, 4 unit-norm floats
// out) and the output of TensorFlow Lite's Python interpreter on the same
// input (testdata/tiny_ref.json), both made by testdata/make_tiny.py
// (TensorFlow 2.21.0, 2026-10-29). This test runs in every image build: it
// found that a first version of litert.go passed a Go pointer that cgo
// refuses.
func TestLiteRTMatchesThePythonInterpreter(t *testing.T) {
	l, err := OpenLiteRT("testdata/tiny.tflite", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if in, out := l.InputShape(), l.OutputShape(); len(in) != 4 || in[1] != 8 || in[3] != 3 || out[len(out)-1] != 4 {
		t.Fatalf("shapes %v %v", in, out)
	}
	x := make([]float32, 8*8*3)
	for i := range x {
		x[i] = float32((i*7919)%255) / 255.0
	}
	var ref struct {
		Argmax int       `json:"argmax"`
		Y      []float64 `json:"y"`
	}
	b, err := os.ReadFile("testdata/tiny_ref.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &ref); err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ { // the buffers are used again: the second run must match too
		y, err := l.Embed(x)
		if err != nil || len(y) != len(ref.Y) {
			t.Fatalf("run %d: %v %v", run, y, err)
		}
		worst := 0.0
		for i := range y {
			worst = math.Max(worst, math.Abs(float64(y[i])-ref.Y[i]))
		}
		if worst > 1e-5 || math.Abs(L2(y)-1) > 1e-5 {
			t.Fatalf("run %d: max |dy| %.3g, norm %v", run, worst, L2(y))
		}
	}
	if _, err := l.Embed(x[:10]); err == nil {
		t.Fatal("a short input must be an error")
	}
	l.Close()
	if _, err := l.Embed(x); err == nil {
		t.Fatal("Embed after Close must be an error")
	}
	if _, err := OpenLiteRT("testdata/none.tflite", 1); err == nil {
		t.Fatal("a missing model must be an error")
	}
	if m, err := OpenModel("testdata/tiny.tflite", 1); err != nil || m.OutputShape()[1] != 4 {
		t.Fatalf("OpenModel %v", err)
	}
}
