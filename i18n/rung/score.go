package rung

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Docker scores pairs with qe/score.sh (the MetricX-24 image of L1/L2).
type Docker struct {
	Script string // path of qe/score.sh
	Image  string // QE_IMAGE
	Log    io.Writer
}

// Check: the docker daemon answers and the MetricX image is there.
func (d Docker) Check(ctx context.Context) error {
	if out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		return fmt.Errorf("docker does not answer (is Colima running? colima start): %s", bytes.TrimSpace(out))
	}
	if out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", d.Image).CombinedOutput(); err != nil {
		return fmt.Errorf("the MetricX image %s is not there: %s", d.Image, bytes.TrimSpace(out))
	}
	return nil
}

// Score writes the pairs as MetricX input, runs the container once, and reads the scores.
func (d Docker) Score(ctx context.Context, pairs []Pair) ([]float64, error) {
	dir, err := os.MkdirTemp("", "v3rde_score_")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "in.jsonl"), filepath.Join(dir, "out.jsonl")
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, p := range pairs {
		if err := enc.Encode(map[string]string{"source": p.Source, "hypothesis": p.Hypothesis, "reference": ""}); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(in, b.Bytes(), 0o600); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "bash", d.Script, in, out)
	cmd.Env = append(os.Environ(), "QE_IMAGE="+d.Image)
	cmd.Stdout, cmd.Stderr = d.Log, d.Log
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("qe/score.sh: %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadScores(f, pairs)
}

// ReadScores reads MetricX output lines: the same pairs, in the same order, each with a prediction in 0..25.
func ReadScores(r io.Reader, pairs []Pair) ([]float64, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var out []float64
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var row struct {
			Source     string   `json:"source"`
			Hypothesis string   `json:"hypothesis"`
			Prediction *float64 `json:"prediction"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			return nil, err
		}
		k := len(out)
		if k >= len(pairs) || row.Source != pairs[k].Source || row.Hypothesis != pairs[k].Hypothesis {
			return nil, fmt.Errorf("score row %d is not pair %d", k+1, k+1)
		}
		if row.Prediction == nil || *row.Prediction < 0 || *row.Prediction > 25 {
			return nil, fmt.Errorf("score row %d: no prediction in 0..25", k+1)
		}
		out = append(out, *row.Prediction)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) != len(pairs) {
		return nil, fmt.Errorf("%d score rows for %d pairs", len(out), len(pairs))
	}
	return out, nil
}
