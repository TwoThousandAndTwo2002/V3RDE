package post

import (
	"context"
	"encoding/json"
	"fmt"
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

// Every purpose this service sends has a prompt, and rescue:draft tells its
// two prompts apart by the text sent (with and without the scanner note).
func TestPromptPurposes(t *testing.T) {
	l := Listing{Title: "Beans", NumberOfServing: 4}
	email, _ := rescuePrompt(l, "")
	drop, _ := dropoffPrompt(l, "")
	ld := l
	ld.Details = &ListingDetails{MealCondition: "sealed", Description: "a can"}
	emailNote, cond := rescuePrompt(ld, ScanVision)
	dropNote, _ := dropoffPrompt(ld, ScanVision)
	if cond == "" {
		t.Fatal("no scanner note")
	}
	for _, tc := range []struct{ purpose, sent, id string }{
		{"fanout:text", fmt.Sprintf(FanoutPrompt, `{"title":"x"}`), "fanout.text"},
		{"fanout:check", fmt.Sprintf(ModeratePrompt, `"x"`), "fanout.check"},
		{"rescue:draft", email, "rescue.email"},
		{"rescue:draft", emailNote, "rescue.email"},
		{"rescue:draft", drop, "rescue.dropoff"},
		{"rescue:draft", dropNote, "rescue.dropoff"},
		{"rescue:draft", "something else", analyze.Unknown},
		{"rescue:web_search", fmt.Sprintf(SearchPrompt, "ZIP 80219", "x"), "rescue.web_search"},
		{"touchup:check", VerifyPrompt, "touchup.check"},
		{"touchup:edit", EditPrompts[ModeLight], "touchup.edit.light"},
		{"touchup:edit", EditPrompts[ModeBackground], "touchup.edit.background"},
		{"touchup:edit", "other edit", analyze.Unknown},
		{"voice:map", fmt.Sprintf(VoicePrompt, "[]", `""`, `""`, voiceNoEarlier, ruleNew), "voice.map"},
		{"voice:transcribe", `{"mode":"PUSH_TO_TALK"}`, "voice.transcribe"},
	} {
		if r, _ := Prompts.For(tc.purpose, tc.sent); r.ID != tc.id {
			t.Errorf("%s %.40q: prompt %q, want %q", tc.purpose, tc.sent, r.ID, tc.id)
		}
	}
}

// The ref, input_sha and effort reach the Call and its JSON (the event line)
// for the three kinds of request: Responses API, image edit, transcribe.
func TestPromptTraceInCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.HasSuffix(r.URL.Path, "/responses") {
			_, _ = io.WriteString(w, `{"id":"r1","output":[{"type":"message","content":[{"type":"output_text","text":"{\"ok\":true}"}]}]}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	m := &Muse{Base: srv.URL, Key: "k", Effort: "low"}
	_, call, err := m.AskJSON(context.Background(), "touchup:check", VerifyPrompt, Image{Bytes: []byte{1}, Mime: "image/jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if call.PromptRef.ID != "touchup.check" || call.Version != "1" || call.InputSHA != analyze.ShortSHA(VerifyPrompt) || call.Effort != "low" {
		t.Errorf("muse call %+v %q %q", call.PromptRef, call.InputSHA, call.Effort)
	}
	b, _ := json.Marshal(call)
	for _, f := range []string{`"prompt_id":"touchup.check"`, `"prompt_version":"1"`, `"prompt_sha":"087f12d5f45f61c8"`, `"effort":"low"`, `"input_sha":"`} {
		if !strings.Contains(string(b), f) {
			t.Errorf("call %s has no %s", b, f)
		}
	}
	ed := &ImageEditor{Base: srv.URL, Key: "k", Reasoning: "low"}
	_, call, _ = ed.Edit(context.Background(), []byte{1}, "image/jpeg", EditPrompts[ModeBackground], "")
	if call.PromptRef.ID != "touchup.edit.background" || call.InputSHA != analyze.ShortSHA(EditPrompts[ModeBackground]) || call.Effort != "low" {
		t.Errorf("edit call %+v %q %q", call.PromptRef, call.InputSHA, call.Effort)
	}
	tr := &Transcriber{URL: srv.URL + "/asr", Key: "k"}
	_, call, _ = tr.Transcribe(context.Background(), []byte{1}, []string{"beans"}, []string{"Spanish"})
	_, call2, _ := tr.Transcribe(context.Background(), []byte{1}, []string{"rice"}, []string{"Spanish"})
	if call.PromptRef.ID != "voice.transcribe" || call.InputSHA == "" || call.InputSHA == call2.InputSHA {
		t.Errorf("transcribe calls %+v %q %q", call.PromptRef, call.InputSHA, call2.InputSHA)
	}
}

// Every purpose literal in the code of this package (AskJSON, AskJSONWith,
// Call{Purpose: ...}) has a registered prompt: a new call without a prompt
// fails here.
func TestPromptEveryPurposeInCode(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	re := regexp.MustCompile(`(?:AskJSON(?:With)?\(ctx, |Purpose: )"([a-z_]+:[a-z_:-]+)"`)
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
	if len(seen) < 8 {
		t.Fatalf("found %d purposes: %v", len(seen), seen)
	}
	for p := range seen {
		if !Prompts.Serves(p) {
			t.Errorf("purpose %q has no prompt in prompts.go", p)
		}
	}
}
