package judge

import (
	"testing"

	"v3rde/i18n"
)

// Measure: scores (shared texts not counted twice), checks, and judge agreement.
func TestMeasure(t *testing.T) {
	f := func(x float64) *float64 { return &x }
	s := Set{Lang: i18n.Language{Code: "xx"}, Msgs: []i18n.Message{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}},
		Trans: map[string]string{"a": "A", "b": "B", "c": "B"}, Shared: map[string]string{"c": "b"}}
	sc := Scores{"a": f(1), "b": f(6), "c": f(6)}
	rep := &Report{Flagged: map[string][]string{"a": {"x"}, "b": {"y"}}, Errors: 1, Verdicts: []Verdict{
		{Group: Group{Kind: "screen", Name: "S"}, Model: "open", Verdict: "fail", Issues: []Problem{{ID: "a"}, {ID: "b"}}},
		{Group: Group{Kind: "screen", Name: "S"}, Model: "meta", Verdict: "fail", Issues: []Problem{{ID: "a"}}},
		{Group: Group{Kind: "concept", Name: "k"}, Model: "meta", Verdict: "pass"}}}
	a := Measure("arm", s, sc, 7, rep)
	if a.Texts != 3 || a.Missing != 1 || a.Calls != 7 || a.Scored != 2 || a.Median != 3.5 || a.Over5 != 1 ||
		a.Groups != 2 || a.GroupsFailed["open"] != 1 || a.GroupsFailed["meta"] != 1 || a.Flagged != 2 || a.ByBoth != 1 || a.ByOne["open"] != 1 || a.JudgeErrors != 1 {
		t.Fatalf("%+v", a)
	}
	if b := Measure("plain", s, sc, 0, nil); b.Judged || b.Flagged != 0 {
		t.Fatalf("%+v", b)
	}
}
