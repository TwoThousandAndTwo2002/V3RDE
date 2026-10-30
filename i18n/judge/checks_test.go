package judge

import (
	"strings"
	"testing"

	"v3rde/i18n"
)

func set() Set {
	msgs := []i18n.Message{{ID: "show", Text: "Show"}, {ID: "hide", Text: "Hide"}, {ID: "a.close", Text: "Close"}, {ID: "b.close", Text: "Close"},
		{ID: "cancel", Text: "Cancel"}, {ID: "retry", Text: "Try again"}, {ID: "retry2", Text: "Try again?"}, {ID: "brand", Text: "V3RDE"}, {ID: "blob", Text: "no blob"}, {ID: "hint", Text: "you@email.com"}, {ID: "one", Text: "item found", Arg: "plural"}, {ID: "many", Text: "items found", Arg: "plural"}}
	ctx := i18n.ContextFile{Contexts: map[string]i18n.Context{}}
	for _, m := range msgs {
		ctx.Contexts[m.ID] = i18n.Context{Kind: "Button", Does: "same"}
	}
	g := i18n.GraphFile{Edges: []i18n.Edge{{From: "show", To: "hide", Kind: "pair"}}}
	for _, m := range msgs {
		g.Edges = append(g.Edges, i18n.Edge{From: m.ID, To: "screen:S", Kind: "on_screen"})
	}
	return Set{Lang: i18n.Language{Code: "hi", Script: "Deva"}, Keep: []string{"V3RDE"}, Msgs: msgs, Graph: g, Ctx: ctx, Shared: map[string]string{},
		Trans: map[string]string{"show": "दिखाएँ", "hide": "दिखाएँ", "a.close": "बंद करें", "b.close": "बंद", "cancel": "बंद करें",
			"retry": "फिर से", "retry2": "फिर से?", "brand": "V3RDE", "blob": "no blob", "hint": "you@email.com", "one": "मिला", "many": "मिला"}}
}

// Each check finds its case and only its case.
func TestChecks(t *testing.T) {
	got := map[string][]string{}
	for _, is := range Checks(set()) {
		got[is.Check] = append(got[is.Check], strings.Join(is.IDs, "+"))
	}
	want := map[string]string{
		"pair_same":           "show+hide",
		"target_inconsistent": "a.close+b.close",
		"source_same":         "a.close+cancel",
		"untranslated":        "blob",
	}
	for c, w := range want {
		if g := strings.Join(got[c], " "); g != w {
			t.Errorf("%s: got %q, want %q", c, g, w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("checks: %v", got)
	}
	f := Flagged(Checks(set()))
	if len(f["show"]) != 1 || f["brand"] != nil || f["retry"] != nil {
		t.Errorf("flagged: %v", f)
	}
}
