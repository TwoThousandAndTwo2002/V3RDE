package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLastDays(t *testing.T) {
	end := time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC)
	if got := strings.Join(lastDays(end, 3), ","); got != "20261030,20261031,20261101" {
		t.Fatal(got)
	}
}

func TestWriteAndPrune(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"20261028", "20261029", "20261030", "20261031"} {
		for _, ext := range []string{".json", ".md"} {
			if err := write(filepath.Join(dir, "report-"+d+ext), []byte(d)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prune(dir, 2); err != nil {
		t.Fatal(err)
	}
	var names []string
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if got := strings.Join(names, ","); got != "keep.txt,report-20261030.json,report-20261030.md,report-20261031.json,report-20261031.md" {
		t.Fatal(got)
	}
	st, _ := os.Stat(filepath.Join(dir, "report-20261031.json"))
	if st.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", st.Mode())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "report-20261031.json")); string(b) != "20261031" {
		t.Fatal(string(b))
	}
}
