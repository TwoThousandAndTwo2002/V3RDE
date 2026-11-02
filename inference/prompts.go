package inference

// P1: the prompts this service sends, by call purpose (server/prompts.go).
// A change of a prompt's text needs a new Version here and a new pin in
// prompts_test.go; the SUP fails otherwise. The versions start at "1" on
// 2026-11-01 for the text as it is on services bbfed58.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// goldenBoxes is the fixed input of the box prompts: two boxes on a 1000 x 800
// frame.
func goldenBoxes() []Instance {
	return []Instance{
		{Left: 100, Top: 80, Right: 300, Bottom: 400, W: 1000, H: 800},
		{Left: 500, Top: 200, Right: 900, Bottom: 760, W: 1000, H: 800},
	}
}

// eachMode renders f for the three box introductions (coordinates only;
// marks and coordinates; marks only), so a change in any of them changes the
// golden text.
func eachMode(f func(marks, coords bool) string) string {
	return strings.Join([]string{f(false, true), f(true, true), f(true, false)}, "\x01")
}

func goldenMulti() string {
	return eachMode(func(m, c bool) string { return MultiPrompt(goldenBoxes(), m, c) })
}

func goldenDetail() string {
	groups := []Item{{Result: analyze.Result{Name: "canned beans"}, Count: 2, Members: []int{1, 2}}}
	return eachMode(func(m, c bool) string { return DetailPrompt(groups, goldenBoxes(), m, c) })
}

// isCompact is the Match of the compact multi prompt.
var isCompact = analyze.EndsWith(compactRule)

// Prompts is the registry of the Meta calls of this service.
var Prompts = analyze.NewPrompts(
	analyze.PromptSpec{ID: "analyze.whole", Version: "1", Purposes: []string{"muse:whole", "muse:whole:"},
		Golden: func() string { return analyze.Prompt }},
	analyze.PromptSpec{ID: "inventory.multi", Version: "1", Purposes: []string{"muse:single", "muse:single-", "muse:retry"},
		Golden: goldenMulti, Match: analyze.Not(isCompact)},
	analyze.PromptSpec{ID: "inventory.multi.compact", Version: "1", Purposes: []string{"muse:single", "muse:single-", "muse:retry"},
		Golden: func() string { return goldenMulti() + compactRule }, Match: isCompact},
	analyze.PromptSpec{ID: "twopass.names", Version: "1", Purposes: []string{"muse:names", "muse:names-"},
		Golden: func() string {
			return eachMode(func(m, c bool) string { return NamePrompt(goldenBoxes(), m, c) })
		}},
	analyze.PromptSpec{ID: "twopass.details", Version: "1", Purposes: []string{"muse:details", "muse:details-"},
		Golden: goldenDetail},
	// muse:agent:N (N >= 1) sends only tool outputs on the same response
	// chain, so it has the prompt of turn 0.
	analyze.PromptSpec{ID: "agent", Version: "1", Purposes: []string{"muse:agent:"},
		Golden: func() string { return AgentPrompt + "\x01" + mustJSON(segmentTool) }},
	// sam:<concept>: the text is the concept (it is in input_sha); the pin is
	// the concept lists of touch-up.
	analyze.PromptSpec{ID: "sam.concept", Version: "1", Purposes: []string{"sam:"},
		Golden: func() string {
			return strings.Join(ContainerConcepts, ",") + "\x01" + strings.Join(SubjectConcepts, ",")
		}},
	// sam:box and sam:refine send prompt tokens; a SAM concept named "box"
	// or "refine" is sam:<concept> (analyze.Prompts.For tries the shorter
	// purpose).
	analyze.PromptSpec{ID: "sam.box", Version: "1", Purposes: []string{"sam:box"},
		Golden: func() string { return SAMBoxFormat }, Match: analyze.StartsWith(SAMObject1)},
	analyze.PromptSpec{ID: "sam.refine", Version: "1", Purposes: []string{"sam:refine"},
		Golden: func() string { return SAMObject1 + "\x01" + SAMPointFormat }, Match: analyze.StartsWith(SAMObject1)},
	// the auto touch-up (X48o): Muse Image makes the photo bigger (EditJSON).
	analyze.PromptSpec{ID: "touchup.bigger", Version: "1", Purposes: []string{PurposeImageEdit},
		Golden: func() string { return BiggerPrompt }, Match: func(s string) bool { return s == BiggerPrompt }},
)

// PurposeImageEdit is the purpose of a Muse Image edit (EditJSON).
const PurposeImageEdit = "image:edit"

// CallLog (P1) keeps every model call made with its context: Client.Do and
// EditJSON add a copy (without the answer body). The touch-up job puts the
// calls on its event line.
type CallLog struct {
	mu    sync.Mutex
	calls []Call
}

type callLogKey struct{}

// WithCallLog is ctx with a new CallLog.
func WithCallLog(ctx context.Context) (context.Context, *CallLog) {
	l := &CallLog{}
	return context.WithValue(ctx, callLogKey{}, l), l
}

func callLogOf(ctx context.Context) *CallLog {
	l, _ := ctx.Value(callLogKey{}).(*CallLog)
	return l
}

func (l *CallLog) add(c Call) {
	c.body = nil
	l.mu.Lock()
	l.calls = append(l.calls, c)
	l.mu.Unlock()
}

// Calls is a copy of the calls so far, in the order they ended.
func (l *CallLog) Calls() []Call {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Call(nil), l.calls...)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
