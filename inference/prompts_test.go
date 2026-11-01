package inference

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// TestPromptPins (P1): the registry against the pin history
// (prompts_pins.txt; analyze.CheckPinHistory). A change of a prompt's text
// fails here until it has a NEW row with a new version, so the version in the
// logs changes with the text (docs/sources/P1.md).
func TestPromptPins(t *testing.T) {
	b, err := os.ReadFile("prompts_pins.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range analyze.CheckPinHistory(Prompts.Refs(), string(b)) {
		t.Error(p)
	}
}

// Every purpose this service sends has a prompt (engine.go, twopass.go,
// agent.go, touchupv2.go).
func TestPromptPurposes(t *testing.T) {
	multi := MultiPrompt(goldenBoxes(), false, true)
	for _, tc := range []struct{ purpose, sent, id string }{
		{"muse:whole", analyze.Prompt, "analyze.whole"},
		{"muse:whole:early", analyze.Prompt, "analyze.whole"},
		{"muse:single", multi, "inventory.multi"},
		{"muse:single-2", multi, "inventory.multi"},
		{"muse:retry", multi, "inventory.multi"},
		{"muse:single", multi + compactRule, "inventory.multi.compact"},
		{"muse:names", "", "twopass.names"},
		{"muse:names-retry", "", "twopass.names"},
		{"muse:details", "", "twopass.details"},
		{"muse:details-retry", "", "twopass.details"},
		{"muse:agent:0", AgentPrompt, "agent"},
		{"muse:agent:3", "", "agent"},
		{"sam:food", "food", "sam.concept"},
		{"sam:takeout container", "takeout container", "sam.concept"},
		{"sam:box", "<0f>1<|box;...|>", "sam.box"},
		{"sam:refine", "<0f>1<|point;...|>", "sam.refine"},
		{"sam:box", "box", "sam.concept"}, // a concept named "box"
		{PurposeImageEdit, BiggerPrompt, "touchup.bigger"},
		{PurposeImageEdit, "another edit", analyze.Unknown},
		{"muse:other", "", analyze.Unknown},
	} {
		if r, _ := Prompts.For(tc.purpose, tc.sent); r.ID != tc.id {
			t.Errorf("%s: prompt %q, want %q", tc.purpose, r.ID, tc.id)
		}
	}
}

// The ref, input_sha, effort, API id and cached tokens reach the call and
// the event line.
func TestPromptTraceInEvent(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, `{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"{}"}]}],
			"usage":{"input_tokens":10,"output_tokens":2,"input_tokens_details":{"cached_tokens":4}}}`)
	}))
	defer srv.Close()
	cl := &Client{URL: srv.URL, Key: "k", Effort: "low"}
	c, err := cl.Do(context.Background(), "muse:whole", map[string]any{"model": DefaultMuse,
		"input": UserMessage(TextPart(analyze.Prompt), ImagePart([]byte{1}, "image/jpeg"))})
	if err != nil {
		t.Fatal(err)
	}
	want := analyze.PromptRef{ID: "analyze.whole", Version: "1", SHA: "5b01039918cb2fd0"}
	if c.PromptRef != want || c.InputSHA != analyze.ShortSHA(analyze.Prompt) || c.Effort != "low" {
		t.Fatalf("call %+v %q %q", c.PromptRef, c.InputSHA, c.Effort)
	}
	if analyze.Effort(gotBody) != "low" {
		t.Errorf("effort not sent: %v", gotBody["reasoning"])
	}
	ev := Event{}
	ev.fromInventory(&Inventory{Calls: []*Call{c}})
	b, _ := json.Marshal(ev.Calls[0])
	for _, f := range []string{`"prompt_id":"analyze.whole"`, `"prompt_version":"1"`, `"prompt_sha":"5b01039918cb2fd0"`,
		`"input_sha":"` + c.InputSHA + `"`, `"effort":"low"`, `"api_request_id":"resp_1"`, `"cached_tokens":4`} {
		if !strings.Contains(string(b), f) {
			t.Errorf("event call %s has no %s", b, f)
		}
	}
	// a SAM call sends no effort
	c, _ = cl.Do(context.Background(), "sam:food", map[string]any{"model": DefaultSAM,
		"input": UserMessage(TextPart("food"), ImagePart([]byte{1}, "image/jpeg"))})
	if c.PromptRef.ID != "sam.concept" || c.Effort != "" || c.InputSHA != analyze.ShortSHA("food") {
		t.Errorf("sam call %+v %q %q", c.PromptRef, c.Effort, c.InputSHA)
	}
}

func TestEngineSettingsInEvent(t *testing.T) {
	e := &Engine{API: &Client{Effort: "low"}, Muse: "m", Concept: "food", MaxNamed: 24, Marks: true, Split: 3, Compact: true}
	s := e.Settings()
	b, _ := json.Marshal(s)
	for _, f := range []string{`"effort":"low"`, `"max_named":24`, `"marks":true`, `"coords":true`, `"split":3`, `"compact":true`, `"concepts":["food","",""]`} {
		if !strings.Contains(string(b), f) {
			t.Errorf("settings %s has no %s", b, f)
		}
	}
	if strings.Contains(string(b), `"k"`) || strings.Contains(strings.ToLower(string(b)), "key") {
		t.Errorf("settings has a key: %s", b)
	}
}

// Every purpose literal in the code of this package (API.Do(ctx, "...",
// "muse:"+..., "sam:"+..., samPrompt(..., "...")) has a registered prompt.
func TestPromptEveryPurposeInCode(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	re := regexp.MustCompile(`(?:\.Do\(ctx, |samPrompt\(ctx, jb, prompt, |callEffort\(ctx, inv, |contractCall\(ctx, inv, )"([a-z_]+:[a-z0-9_:-]*)"`)
	seen := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			seen[m[1]] = true
		}
	}
	if len(seen) < 6 {
		t.Fatalf("found %d purposes: %v", len(seen), seen)
	}
	for p := range seen {
		probe := p
		if strings.HasSuffix(p, ":") { // a prefix: "muse:" + purpose, "sam:" + concept
			probe += "x"
		}
		if p == "muse:" { // "muse:"+purpose: the purposes of single, names, details
			for _, q := range []string{"muse:single", "muse:retry", "muse:names", "muse:details-retry"} {
				if !Prompts.Serves(q) {
					t.Errorf("purpose %q has no prompt", q)
				}
			}
			continue
		}
		if !Prompts.Serves(probe) {
			t.Errorf("purpose %q has no prompt in prompts.go", p)
		}
	}
}
