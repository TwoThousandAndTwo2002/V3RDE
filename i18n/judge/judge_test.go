package judge

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"v3rde/i18n"
)

type fakeModel struct {
	name  string
	calls int32
	reply func(instr, input string) (string, error)
}

func (f *fakeModel) Name() string { return f.name }
func (f *fakeModel) Ask(ctx context.Context, instr, input string) (string, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.reply(instr, input)
}

func gset() Set {
	s := set()
	s.Graph.Edges = append(s.Graph.Edges, i18n.Edge{From: "a.close", To: "concept:close", Kind: "uses"}, i18n.Edge{From: "b.close", To: "concept:close", Kind: "uses"},
		i18n.Edge{From: "cancel", To: "concept:close", Kind: "uses"})
	s.Lang.Name = "Hindi"
	return s
}

// One group for each concept and screen with 2 or more texts; a shared text once.
func TestGroups(t *testing.T) {
	s := gset()
	s.Shared["b.close"] = "a.close"
	var got []string
	for _, g := range Groups(s) {
		got = append(got, g.Kind+":"+g.Name+"="+strings.Join(g.IDs, ","))
	}
	if j := strings.Join(got, " "); !strings.Contains(j, "concept:close=a.close,cancel") || strings.Contains(j, "b.close") || !strings.HasPrefix(j, "concept:close") {
		t.Fatalf("groups: %s", j)
	}
}

// The judge answer is read from the first JSON object; unknown ids are dropped;
// fail with no known issue, or no JSON, is an error.
func TestParse(t *testing.T) {
	g := Group{IDs: []string{"a", "b"}}
	v, ps, err := Parse("Here:\n```json\n{\"verdict\":\"FAIL\",\"issues\":[{\"id\":\"a\",\"problem\":\"noun on a button\",\"suggestion\":\"x\"},{\"id\":\"zz\",\"problem\":\"p\"}]}\n```", g)
	if err != nil || v != "fail" || len(ps) != 1 || ps[0].ID != "a" {
		t.Fatalf("%s %v %v", v, ps, err)
	}
	for _, bad := range []string{"no json", `{"verdict":"maybe"}`, `{"verdict":"fail","issues":[{"id":"zz","problem":"p"}]}`} {
		if _, _, err := Parse(bad, g); err == nil {
			t.Errorf("must fail: %s", bad)
		}
	}
}

// Every model gets every group once; the prompt has the language, the English,
// the context and the translation; issues name the model and the group.
func TestJudge(t *testing.T) {
	s := gset()
	groups := Groups(s)
	var seen int32
	m1 := &fakeModel{name: "open", reply: func(instr, input string) (string, error) {
		if strings.Contains(instr, "Hindi (hi)") && strings.Contains(input, `"translation": "बंद करें"`) && strings.Contains(input, `"does": "same"`) {
			atomic.AddInt32(&seen, 1)
		}
		if strings.Contains(input, "Concept: close") {
			return `{"verdict":"fail","issues":[{"id":"cancel","problem":"Cancel uses the word for close.","suggestion":"रद्द करें"}]}`, nil
		}
		return `{"verdict":"pass","issues":[]}`, nil
	}}
	m2 := &fakeModel{name: "meta", reply: func(string, string) (string, error) { return "not json", nil }}
	vs := Judge(context.Background(), s, groups, []Model{m1, m2}, map[string]string{"close": "Closing a sheet."}, 2)
	if len(vs) != 2*len(groups) || int(m1.calls) != len(groups) || int(m2.calls) != len(groups) || int(seen) != len(groups) {
		t.Fatalf("verdicts %d, calls %d/%d, prompts with data %d (groups %d)", len(vs), m1.calls, m2.calls, seen, len(groups))
	}
	is := Issues(vs)
	if len(is) != 1 || is[0].IDs[0] != "cancel" || is[0].Model != "open" || is[0].Group != "close" || !strings.Contains(is[0].Detail, "रद्द करें") {
		t.Fatalf("issues %+v", is)
	}
	for _, v := range vs {
		if v.Model == "meta" && v.Error == "" {
			t.Errorf("a non-JSON answer must be an error: %+v", v)
		}
	}
}
