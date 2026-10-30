package i18n

import "testing"

func TestIsResult(t *testing.T) {
	for p, want := range map[string]bool{
		"runs-ctx/es-419/es-419.json":              true,
		"zh-Hans.json":                             true,
		"runs-ctx/es-419/es-419.check.json":        false,
		"runs-ctx/es-419/es-419.judge.json":        false,
		"runs-ctx/es-419/es-419.judge.round1.json": false,
		"runs/es-419/es-419.before-dedupe.json":    false,
		"runs-ctx/es-419/es-419.ledger.jsonl":      false,
	} {
		if got := IsResult(p); got != want {
			t.Errorf("IsResult(%q) = %v, want %v", p, got, want)
		}
	}
}
