package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gate "github.com/TwoThousandAndTwo2002/Icarus/gate"
)

func write(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// open checks the head pin, the head, then the model pin, before it opens
// the model.
func TestOpenChecksThePins(t *testing.T) {
	dir := t.TempDir()
	headPath := write(t, dir, "head.json", `{"input_side":4,"embedding_dim":2,"threshold":0.5,"coef":[1,2]}`)
	modelPath := write(t, dir, "model.tflite", "not a model")
	headSum, _, _ := fileSHA256(headPath)
	modelSum, _, _ := fileSHA256(modelPath)
	if _, _, _, _, err := open(modelPath, headPath, modelSum, pinHead, 1); err == nil ||
		err.Error() != "head sha256 "+headSum[:12]+" is not the pin "+pinHead[:12] {
		t.Fatalf("wrong head pin: %v", err)
	}
	if _, _, _, _, err := open(modelPath, headPath, "abc", headSum, 1); err == nil || !strings.HasSuffix(err.Error(), "is not the pin abc") {
		t.Fatalf("a short model pin must not crash: %v", err)
	}
	bad := write(t, dir, "bad.json", `{"input_side":4,"embedding_dim":3,"coef":[1]}`)
	if _, _, _, _, err := open(modelPath, bad, "", "", 1); err == nil || !strings.Contains(err.Error(), "coefficients") {
		t.Fatalf("a bad head: %v", err)
	}
	// the right pins pass; the open then fails (no litert tag, or not a model)
	if _, _, _, _, err := open(modelPath, headPath, modelSum, headSum, 1); err == nil || strings.Contains(err.Error(), "pin") {
		t.Fatalf("right pins: %v", err)
	}
	if _, _, _, _, err := open(filepath.Join(dir, "none"), headPath, "", "", 1); err == nil {
		t.Fatal("a missing model")
	}
}

func TestShapesMustAgreeWithTheHead(t *testing.T) {
	h := &gate.Head{InputSide: 384, Dim: 512}
	for _, tc := range []struct {
		in, out []int
		ok      bool
	}{
		{[]int{1, 384, 384, 3}, []int{1, 512}, true},
		{[]int{1, 3, 384, 384}, []int{1, 512}, false}, // NCHW
		{[]int{1, 192, 192, 3}, []int{1, 512}, false},
		{[]int{1, 384, 384, 3}, []int{1, 768}, false},
		{[]int{384, 384, 3}, []int{1, 512}, false},
		{[]int{1, 384, 384, 3}, nil, false},
	} {
		if err := agree(tc.in, tc.out, h); (err == nil) != tc.ok {
			t.Errorf("%v %v: %v", tc.in, tc.out, err)
		}
	}
}

func TestShadowNeedsBothPins(t *testing.T) {
	s := openShadow("m.tflite", "h.json", "", "x")
	if s.Model != nil || !strings.Contains(s.LoadError, "needs -shadow-model-sha256 and -shadow-head-sha256") {
		t.Fatalf("%+v", s)
	}
	if s := openShadow(filepath.Join(t.TempDir(), "none"), filepath.Join(t.TempDir(), "none"), "a", "b"); s.LoadError == "" || s.Model != nil {
		t.Fatalf("%+v", s)
	}
}

func TestDefaultsAreThePEGate(t *testing.T) {
	if len(pinModel) != 64 || len(pinHead) != 64 || pinModel == pinHead {
		t.Fatal("pins")
	}
	if short("abc") != "abc" || short(pinModel) != pinModel[:12] {
		t.Fatal("short")
	}
}
