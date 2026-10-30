package i18n

import (
	"path/filepath"
	"strings"
)

// IsResult is true for a translation result <lang>.json, and false for the
// other files next to it: <lang>.check.json, <lang>.judge.json,
// <lang>.judge.round1.json, <lang>.before-dedupe.json and ledgers. A language
// code has no dot (es-419, zh-Hans), so a result name has exactly one dot.
func IsResult(path string) bool {
	b := filepath.Base(path)
	return strings.HasSuffix(b, ".json") && strings.Count(b, ".") == 1
}
