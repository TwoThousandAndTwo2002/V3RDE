package analyze

import (
	"encoding/json"
	"strings"
	"testing"
)

func testPrompts() *Prompts {
	return NewPrompts(
		PromptSpec{ID: "whole", Version: "1", Purposes: []string{"muse:whole", "muse:whole:"}, Golden: func() string { return "W" }},
		PromptSpec{ID: "any", Version: "2", Purposes: []string{"muse:"}, Golden: func() string { return "A" }},
		PromptSpec{ID: "email", Version: "1", Purposes: []string{"draft"}, Golden: func() string { return "E" }, Match: StartsWith("Email %s more")},
		PromptSpec{ID: "drop", Version: "3", Purposes: []string{"draft"}, Golden: func() string { return "D" }, Match: StartsWith("Drop %s")},
		PromptSpec{ID: "plain", Version: "1", Purposes: []string{"multi"}, Golden: func() string { return "P" }, Match: Not(EndsWith("!"))},
		PromptSpec{ID: "loud", Version: "1", Purposes: []string{"multi"}, Golden: func() string { return "L" }, Match: EndsWith("!")},
		PromptSpec{ID: "concept", Version: "1", Purposes: []string{"sam:"}, Golden: func() string { return "C" }},
		PromptSpec{ID: "box", Version: "1", Purposes: []string{"sam:box"}, Golden: func() string { return "B" }, Match: StartsWith("<0f>")},
	)
}

func TestPromptsFor(t *testing.T) {
	p := testPrompts()
	for _, tc := range []struct{ purpose, sent, id string }{
		{"muse:whole", "", "whole"},
		{"muse:whole:early", "", "whole"}, // the longest prefix wins over "muse:"
		{"muse:names", "", "any"},
		{"draft", "Email x more", "email"},
		{"draft", "Drop x", "drop"},
		{"draft", "Other", Unknown}, // two Match specs, none true, no plain spec
		{"multi", "boxes", "plain"},
		{"multi", "boxes!", "loud"},
		{"sam:food", "food", "concept"},
		{"sam:box", "<0f>1<|box|>", "box"},
		{"sam:box", "box", "concept"}, // a concept named "box": the shorter purpose
		{"sam", "food", Unknown},
		{"muse", "", Unknown}, // "muse:" is a prefix, not the purpose "muse"
	} {
		got, ok := p.For(tc.purpose, tc.sent)
		if got.ID != tc.id || ok != (tc.id != Unknown) {
			t.Errorf("For(%q, %q) = %q %v, want %q", tc.purpose, tc.sent, got.ID, ok, tc.id)
		}
	}
	if r, _ := p.For("draft", "Drop x"); r.Version != "3" || r.SHA != ShortSHA("D") || len(r.SHA) != 16 {
		t.Errorf("ref %+v", r)
	}
	if !p.Serves("draft") || !p.Serves("muse:x") || p.Serves("other:x") || p.Serves("muse") {
		t.Error("Serves")
	}
	var nilP *Prompts
	if r, ok := nilP.For("muse:whole", ""); ok || r.ID != Unknown {
		t.Errorf("nil registry: %+v %v", r, ok)
	}
}

func TestPromptsDuplicateIDPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("no panic for a duplicate id")
		}
	}()
	NewPrompts(PromptSpec{ID: "a", Golden: func() string { return "" }}, PromptSpec{ID: "a", Golden: func() string { return "" }})
}

func TestPromptsRefsSorted(t *testing.T) {
	var ids []string
	for _, r := range testPrompts().Refs() {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, ","); got != "any,box,concept,drop,email,loud,plain,whole" {
		t.Errorf("Refs = %s", got)
	}
}

func TestTextOfAndInputSHA(t *testing.T) {
	body := map[string]any{"model": "m", "input": []any{map[string]any{"type": "message", "role": "user",
		"content": []any{map[string]any{"type": "input_text", "text": "one"},
			map[string]any{"type": "input_image", "image_url": "data:x"},
			map[string]any{"type": "input_text", "text": "two"}}}}}
	if got := TextOf(body); got != "one\x00two" {
		t.Errorf("TextOf = %q", got)
	}
	if InputSHA(TextOf(body)) != ShortSHA("one\x00two") || InputSHA("") != "" {
		t.Error("InputSHA")
	}
	// a tool output turn has no text
	if TextOf(map[string]any{"input": []any{map[string]any{"type": "function_call_output", "output": "{}"}}}) != "" {
		t.Error("tool output has no text")
	}
	if Effort(map[string]any{"reasoning": map[string]any{"effort": "low"}}) != "low" || Effort(map[string]any{}) != "" {
		t.Error("Effort")
	}
}

func TestPromptRefJSON(t *testing.T) {
	b, _ := json.Marshal(struct {
		Purpose string `json:"purpose"`
		PromptRef
	}{"p", PromptRef{ID: "i", Version: "1", SHA: "s"}})
	if string(b) != `{"purpose":"p","prompt_id":"i","prompt_version":"1","prompt_sha":"s"}` {
		t.Errorf("JSON = %s", b)
	}
}

func TestCheckPinHistory(t *testing.T) {
	refs := []PromptRef{{ID: "a", Version: "2", SHA: "s2"}, {ID: "b", Version: "1", SHA: "t1"}}
	ok := "# history\na 1 s1  # first\nb 1 t1\na 2 s2\n"
	if bad := CheckPinHistory(refs, ok); len(bad) != 0 {
		t.Fatalf("ok history: %v", bad)
	}
	for _, tc := range []struct{ name, hist, want string }{
		{"the old row edited to the new sha", "a 1 s2\nb 1 t1\n", "a is version 2"},
		{"one version, two shas", "a 1 s1\nb 1 t1\na 1 s9\na 2 s2\n", "two shas"},
		{"no row", "a 1 s1\na 2 s2\n", "b has no pin row"},
		{"newest is not last", "a 2 s2\nb 1 t1\na 1 s1\n", "a is version 2"},
		{"not registered", ok + "c 1 u1\n", "c, which is not registered"},
		{"bad row", ok + "a 3\n", "want \"id version sha\""},
	} {
		bad := CheckPinHistory(refs, tc.hist)
		if !strings.Contains(strings.Join(bad, "|"), tc.want) {
			t.Errorf("%s: %v", tc.name, bad)
		}
	}
}
