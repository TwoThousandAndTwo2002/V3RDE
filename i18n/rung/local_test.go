package rung

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Local runs the scorer script in this container with the arguments of
// qe/score.sh, in the MetricX directory, and reads its output file.
func TestLocalScorer(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "run_predict.sh")
	// a fake run_predict.py: it records its arguments and its directory, and adds a prediction to each row
	fake := `#!/bin/sh
echo "$PWD $*" > "` + filepath.Join(dir, "args.txt") + `"
while [ $# -gt 0 ]; do case "$1" in --input_file) in="$2"; shift;; --output_file) out="$2"; shift;; esac; shift; done
sed 's/}$/,"prediction":2.5}/' "$in" > "$out"
`
	if err := os.WriteFile(script, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	l := Local{Python: "sh", Script: script, Dir: dir}
	if err := l.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	pairs := []Pair{{Source: "Retake", Hypothesis: "Volver a tomar"}, {Source: "Give", Hypothesis: "Dar"}}
	got, err := l.Score(context.Background(), pairs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 2.5 || got[1] != 2.5 {
		t.Fatalf("scores %v", got)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	for _, want := range []string{dir + " ", "--tokenizer google/mt5-large", "--model_name_or_path google/metricx-24-hybrid-large-v2p6",
		"--max_input_length 1536", "--batch_size 1", "--qe"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("no %q in %s", want, args)
		}
	}
	if err := (Local{Python: "sh", Script: filepath.Join(dir, "none.py"), Dir: dir}).Check(context.Background()); err == nil {
		t.Error("Check passes with no script")
	}
}
