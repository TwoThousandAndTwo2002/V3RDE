package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// load checks the pin before it opens the file: a wrong pin, a short pin
// and a missing file are errors (the service then answers UNAVAILABLE).
func TestLoadChecksThePin(t *testing.T) {
	p := filepath.Join(t.TempDir(), "products.sqlite")
	if err := os.WriteFile(p, []byte("not the catalog"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum, err := fileSHA256(p)
	if err != nil || len(sum) != 64 {
		t.Fatalf("sha %q %v", sum, err)
	}
	if _, _, err := load(p, pinCatalog, 0); err == nil ||
		err.Error() != "catalog sha256 "+sum[:12]+" is not the pin "+pinCatalog[:12] {
		t.Fatalf("wrong pin: %v", err)
	}
	if _, _, err := load(p, "abc", 0); err == nil || !strings.HasSuffix(err.Error(), "is not the pin abc") {
		t.Fatalf("a short pin must not panic: %v", err)
	}
	// the right pin passes the check; the open then fails (no sqlite tag, or not a catalog)
	if _, _, err := load(p, sum, 0); err == nil || strings.Contains(err.Error(), "pin") {
		t.Fatalf("right pin: %v", err)
	}
	if _, _, err := load(p, "", 0); err == nil || strings.Contains(err.Error(), "pin") {
		t.Fatalf("no pin: %v", err)
	}
	if _, _, err := load(filepath.Join(t.TempDir(), "none"), pinCatalog, 0); err == nil {
		t.Fatal("a missing file must fail")
	}
}
