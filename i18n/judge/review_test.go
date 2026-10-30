package judge

import (
	"encoding/json"
	"testing"

	"v3rde/i18n"
)

// BuildReview: fixed, spread, review, flagged and ok, and the before text only when it changed.
func TestReview(t *testing.T) {
	var en i18n.EnFile
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		en.Messages = append(en.Messages, i18n.Message{ID: id, Text: "T " + id})
	}
	g := i18n.GraphFile{Edges: []i18n.Edge{{From: "a", To: "concept:k", Kind: "uses"}, {From: "b", To: "concept:k", Kind: "uses"}}}
	res := func(s string) Result {
		var r Result
		if err := json.Unmarshal([]byte(s), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	before := res(`{"messages":[{"id":"a","text":"A0"},{"id":"b","text":"B0"},{"id":"c","text":"C0"},{"id":"d","text":"D0"},{"id":"e","text":"E0"}]}`)
	final := res(`{"messages":[{"id":"a","text":"A1","revised":true},{"id":"b","text":"B1","revised":true,"spread_from":"a"},
		{"id":"c","text":"C1","revised":true},{"id":"d","text":"D0"},{"id":"e","text":"E0"}]}`)
	r1 := Report{Checks: []Issue{{Check: "pair_same", IDs: []string{"a", "c"}, Detail: "same"}}}
	now := Report{Verdicts: []Verdict{{Group: Group{Kind: "screen", Name: "S"}, Model: "m", Verdict: "fail",
		Issues: []Problem{{ID: "c", Problem: "still wrong"}, {ID: "d", Problem: "new"}}}}}
	rv := BuildReview(en, g, i18n.ContextFile{}, []Lang{{Language: i18n.Language{Code: "xx"}, Final: final, Before: &before, Round1: &r1, Now: &now}})
	L := rv.ByLang["xx"]
	want := map[string]string{"a": "fixed", "b": "spread", "c": "review", "d": "flagged", "e": "ok"}
	for id, st := range want {
		if L[id].Status != st {
			t.Errorf("%s: %s, want %s", id, L[id].Status, st)
		}
	}
	if L["a"].Before != "A0" || L["e"].Before != "" || L["b"].SpreadFrom != "a" || len(rv.Concept["k"]) != 2 ||
		rv.Langs[0].Round1 != 2 || rv.Langs[0].Now != 2 || rv.Langs[0].GroupsFail["m"] != 1 || L["c"].Now[0].By != "m" {
		t.Fatalf("%+v %+v", L, rv.Langs[0])
	}
}
