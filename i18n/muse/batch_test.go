package muse

import (
	"encoding/json"
	"strings"
	"testing"
)

// The batch prompt lists every text as JSON; the parser reads a fenced answer.
func TestBatchPrompt(t *testing.T) {
	sys, user, err := PromptBatchWith("Spanish (Latin America)", "es-419", []BatchItem{{ID: "a", Context: "Kind: Button.", Text: "Retake"}, {ID: "b", Text: "Item <x1/>"}})
	if err != nil || !strings.Contains(sys, "one JSON object") || !strings.Contains(sys, "es-419") || !strings.HasPrefix(user, "Texts:\n") {
		t.Fatalf("%v %q %q", err, sys, user)
	}
	var items []BatchItem
	if err := json.Unmarshal([]byte(strings.TrimPrefix(user, "Texts:\n")), &items); err != nil || len(items) != 2 || items[1].Text != "Item <x1/>" {
		t.Fatalf("%v %+v", err, items)
	}
	m, err := ParseBatch("```json\n{\"a\": \"Repetir\", \"b\": \"Artículo <x1/>\"}\n```")
	if err != nil || m["a"] != "Repetir" || m["b"] != "Artículo <x1/>" {
		t.Fatalf("%v %v", err, m)
	}
	if _, err := ParseBatch("Repetir"); err == nil {
		t.Fatal("no JSON: want an error")
	}
}
