package post

// P1: the prompts this service sends, by call purpose (server/prompts.go).
// A change of a prompt's text needs a new Version here and a new pin in
// prompts_test.go; the SUP fails otherwise. The versions start at "1" on
// 2026-11-01 for the text as it is on services bbfed58.

import (
	"encoding/json"
	"sort"
	"strings"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

func mustJSON(v any) string {
	b, err := json.Marshal(v) // map keys sorted
	if err != nil {
		panic(err)
	}
	return string(b)
}

func constant(s string) func() string { return func() string { return s } }

// Prompts is the registry of the Meta calls of this service.
var Prompts = analyze.NewPrompts(
	analyze.PromptSpec{ID: "fanout.text", Version: "1", Purposes: []string{"fanout:text"}, Golden: constant(FanoutPrompt)},
	analyze.PromptSpec{ID: "fanout.check", Version: "1", Purposes: []string{"fanout:check"}, Golden: constant(ModeratePrompt)},
	// rescue:draft is one of two prompts (R3): the text sent tells which. The
	// scanner note (RescueHint, hintWords) is in both goldens.
	analyze.PromptSpec{ID: "rescue.email", Version: "1", Purposes: []string{"rescue:draft"},
		Golden: goldenRescue(RescuePrompt), Match: analyze.StartsWith(RescuePrompt)},
	analyze.PromptSpec{ID: "rescue.dropoff", Version: "1", Purposes: []string{"rescue:draft"},
		Golden: goldenRescue(DropoffPrompt), Match: analyze.StartsWith(DropoffPrompt)},
	analyze.PromptSpec{ID: "rescue.web_search", Version: "1", Purposes: []string{"rescue:web_search"}, Golden: constant(SearchPrompt)},
	analyze.PromptSpec{ID: "touchup.check", Version: "1", Purposes: []string{"touchup:check"}, Golden: constant(VerifyPrompt)},
	analyze.PromptSpec{ID: "touchup.edit.light", Version: "1", Purposes: []string{"touchup:edit"},
		Golden: constant(EditPrompts[ModeLight]), Match: func(s string) bool { return s == EditPrompts[ModeLight] }},
	analyze.PromptSpec{ID: "touchup.edit.background", Version: "1", Purposes: []string{"touchup:edit"},
		Golden: constant(EditPrompts[ModeBackground]), Match: func(s string) bool { return s == EditPrompts[ModeBackground] }},
	analyze.PromptSpec{ID: "voice.map", Version: "1", Purposes: []string{"voice:map"},
		Golden: constant(VoicePrompt + "\x01" + ruleEdit + "\x01" + ruleNew)},
	// voice:transcribe has no prompt text: the fixed request fields are the
	// pin; the keywords and languages are in input_sha.
	analyze.PromptSpec{ID: "voice.transcribe", Version: "1", Purposes: []string{"voice:transcribe"},
		Golden: func() string { return mustJSON(transcribeFields) }},
)

// transcribeFields are the fixed fields of a transcribe request (Transcribe
// sends them; the pin is their JSON).
var transcribeFields = map[string]string{"mode": "PUSH_TO_TALK", "audioEncoding": "WAV"}

// goldenRescue is tmpl rendered for a fixed listing: without the scanner
// note, and with the note of each meal_condition (hintWords) and a
// description, so the note's words are pinned too.
func goldenRescue(tmpl string) func() string {
	return func() string {
		l := Listing{Title: "Canned beans", NumberOfServing: 4}
		out := []string{promptText(tmpl, l, "")}
		conds := make([]string, 0, len(hintWords))
		for k := range hintWords {
			conds = append(conds, k)
		}
		sort.Strings(conds)
		for _, k := range conds {
			ld := l
			ld.Details = &ListingDetails{MealCondition: k, Description: "two cans of beans"}
			out = append(out, promptText(tmpl, ld, ScanVision))
		}
		return strings.Join(out, "\x01")
	}
}

func promptText(tmpl string, l Listing, source string) string {
	p, _ := promptFor(tmpl, l, source)
	return p
}

// trace sets the P1 fields of call: the prompt of sent, its sha, and effort.
func trace(call *Call, sent, effort string) {
	call.PromptRef, _ = Prompts.For(call.Purpose, sent)
	call.InputSHA, call.Effort = analyze.InputSHA(sent), effort
}
