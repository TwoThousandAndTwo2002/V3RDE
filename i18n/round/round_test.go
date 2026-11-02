package round

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"v3rde/i18n"
	"v3rde/i18n/judge"
	"v3rde/i18n/muse"
	"v3rde/i18n/rung"
)

func TestTag(t *testing.T) {
	at := time.Date(2026, 10, 30, 21, 5, 9, 0, time.FixedZone("x", 3600))
	if got := Tag(at); got != "r20261030T200509Z" {
		t.Fatalf("Tag: %s", got)
	}
}

// The work list: added, missing (in the old English, no live translation),
// changed, reached through the graph; removed texts leave.
func TestWorkList(t *testing.T) {
	src := map[string]string{"a": "A", "b": "B", "c": "C"}
	oldMsgs := []i18n.Message{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "e", Text: "E"}, {ID: "f", Text: "F"}}
	cur := []i18n.Message{{ID: "a", Text: "A"}, {ID: "b", Text: "B2"}, {ID: "d", Text: "D"}, {ID: "e", Text: "E"}, {ID: "f", Text: "F"}}
	g := i18n.GraphFile{Nodes: []i18n.Node{{ID: "a", Type: "text"}, {ID: "b", Type: "text"}, {ID: "e", Type: "text"}, {ID: "screen:S", Type: "screen"}, {ID: "screen:T", Type: "screen"}},
		Edges: []i18n.Edge{{From: "a", To: "screen:S", Kind: "on_screen"}, {From: "b", To: "screen:S", Kind: "on_screen"},
			{From: "e", To: "screen:T", Kind: "on_screen"}, {From: "f", To: "screen:T", Kind: "on_screen"}}}
	im := i18n.ImpactOf(oldMsgs, cur, g, g)
	work, removed := WorkList(src, im, cur)
	var got []string
	for _, w := range work {
		got = append(got, w.ID+":"+w.Reason)
	}
	// e has no live translation (an earlier failure): missing; it starts no walk, so f is not reached
	if s := strings.Join(got, " "); s != "a:reached b:changed d:added e:missing f:missing" {
		t.Fatalf("work: %s", s)
	}
	if work[0].Via == "" {
		t.Errorf("reached with no via")
	}
	if strings.Join(removed, ",") != "c" {
		t.Fatalf("removed: %v", removed)
	}
	// nothing changed: no work
	same := cur[:1]
	if w, r := WorkList(map[string]string{"a": "A"}, i18n.ImpactOf(same, same, g, g), same); len(w)+len(r) != 0 {
		t.Fatalf("no change gave %v %v", w, r)
	}
}

// The merge: a passed text takes the new translation; a failed text keeps the
// live one (its source stays old, so the next round tries it again), or its
// fallback, or none; a removed text leaves; the others stay.
func TestMergeLive(t *testing.T) {
	live := rung.Result{Run: "r1", Lang: "es-419", Messages: []rung.Chosen{{ID: "a", Text: "a1"}, {ID: "b", Text: "b1"}, {ID: "c", Text: "c1"}, {ID: "k", Text: "k1"}, {ID: "gone", Text: "g1"}}}
	src := map[string]string{"a": "A", "b": "B", "c": "C", "k": "K", "gone": "G"}
	cur := []i18n.Message{{ID: "a", Text: "A2"}, {ID: "b", Text: "B2"}, {ID: "c", Text: "C"}, {ID: "k", Text: "K"}, {ID: "n", Text: "N"}}
	fb := rung.Chosen{ID: "c", Text: "c-round1"}
	out := map[string]Outcome{
		"a": {Pass: true, New: rung.Chosen{ID: "a", Text: "a2"}},
		"b": {Reasons: []string{"screen_judge: wrong"}, Tried: []string{"b2", "b3"}},
		"c": {Fallback: &fb, Reasons: []string{"reached"}},
		"n": {Reasons: []string{"concept_judge: bad"}, Tried: []string{"n1"}},
	}
	res, newSrc, review := MergeLive("es-419", live, src, cur, out)
	var got []string
	for _, c := range res.Messages {
		got = append(got, c.ID+"="+c.Text)
	}
	if s := strings.Join(got, " "); s != "a=a2 b=b1 c=c-round1 k=k1" {
		t.Fatalf("live: %s", s)
	}
	if strings.Join(res.Missing, ",") != "n" {
		t.Fatalf("missing: %v", res.Missing)
	}
	if newSrc["a"] != "A2" || newSrc["b"] != "B" || newSrc["c"] != "C" || newSrc["k"] != "K" || newSrc["gone"] != "" || newSrc["n"] != "" {
		t.Fatalf("source: %v", newSrc)
	}
	if len(review) != 3 {
		t.Fatalf("review: %+v", review)
	}
	from := map[string]string{}
	for _, r := range review {
		from[r.ID] = r.From + ":" + r.Kept
	}
	if from["b"] != "live:b1" || from["c"] != "round:c-round1" || from["n"] != "none:" {
		t.Fatalf("review kept: %v", from)
	}
}

// Publish: each file through a temp file and a rename, the old file replaced,
// no temp file left, index.json with the languages and the tag.
func TestPublishAtomic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "es-419.json"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	langs := i18n.Languages{Source: "en", Languages: []i18n.Language{{Code: "en", Role: "source", Dir: "ltr"}, {Code: "es-419", Dir: "ltr"}, {Code: "ar", Dir: "rtl"}}}
	en := []i18n.Message{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}}
	live := map[string]rung.Result{"es-419": {Messages: []rung.Chosen{{ID: "a", Text: "a"}}, Missing: []string{"b"}}}
	if err := Publish(dir, "r1", "c1", langs, en, live); err != nil {
		t.Fatal(err)
	}
	var p Published
	if err := i18n.ReadJSON(filepath.Join(dir, "es-419.json"), &p); err != nil {
		t.Fatal(err)
	}
	if p.Round != "r1" || p.Messages["a"] != "a" || len(p.Messages) != 1 {
		t.Fatalf("es-419: %+v", p)
	}
	var idx Index
	if err := i18n.ReadJSON(filepath.Join(dir, "index.json"), &idx); err != nil {
		t.Fatal(err)
	}
	if idx.Round != "r1" || len(idx.Languages) != 2 || idx.Languages[1].Missing != 1 {
		t.Fatalf("index: %+v", idx)
	}
	ents, _ := os.ReadDir(dir)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if s := strings.Join(names, " "); s != "en.json es-419.json index.json" {
		t.Fatalf("files: %s (no temp file, no ar: it has no live file)", s)
	}
	// a write that cannot finish leaves the old file
	bad := filepath.Join(dir, "sub")
	if err := os.WriteFile(bad, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(filepath.Join(bad, "f.json"), []byte("y")); err == nil {
		t.Fatal("a write under a file must fail")
	}
}

// ---- the whole round, with fakes ----

const xID, yID = "ROLE.logout", "RECEIVE.collected"

// fakeTr answers a batch call: "T:" + the masked text, or "F:" + the masked
// text when the context has the problems of a fix round.
type fakeTr struct {
	mu    sync.Mutex
	calls int
	texts map[string]int // X66 review: the English texts sent, with how many times
}

func (f *fakeTr) Chat(ctx context.Context, sys, user string) (muse.Reply, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	var items []muse.BatchItem
	if err := json.Unmarshal([]byte(strings.TrimPrefix(user, "Texts:\n")), &items); err != nil {
		return muse.Reply{}, err
	}
	out := map[string]string{}
	f.mu.Lock()
	if f.texts == nil {
		f.texts = map[string]int{}
	}
	for _, it := range items {
		f.texts[it.Text]++
	}
	f.mu.Unlock()
	for _, it := range items {
		p := "T:"
		if strings.Contains(it.Context, "Problems found") {
			p = "F:"
		}
		out[it.ID] = p + it.Text
	}
	b, _ := json.Marshal(out)
	return muse.Reply{Text: string(b)}, nil
}

// fakeJudge fails the texts in always, and yID while it is a first try ("T:").
type fakeJudge struct {
	name   string
	always map[string]bool
}

func (j fakeJudge) Name() string { return j.name }
func (j fakeJudge) Ask(ctx context.Context, instr, input string) (string, error) {
	var items []struct{ ID, Translation string }
	if err := json.Unmarshal([]byte(input[strings.Index(input, "["):]), &items); err != nil {
		return "", err
	}
	var issues []judge.Problem
	for _, it := range items {
		if j.always[it.ID] || it.ID == yID && strings.HasPrefix(it.Translation, "T:") {
			issues = append(issues, judge.Problem{ID: it.ID, Problem: "wrong action", Suggestion: "x"})
		}
	}
	v := "pass"
	if len(issues) > 0 {
		v = "fail"
	}
	b, _ := json.Marshal(map[string]any{"verdict": v, "issues": issues})
	return string(b), nil
}

type fakeSc struct {
	calls int
	fail  bool // X49b review: the scorer stops (for example at the memory limit)
}

func (f *fakeSc) Check(ctx context.Context) error { return nil }
func (f *fakeSc) Score(ctx context.Context, p []rung.Pair) ([]float64, error) {
	f.calls++
	if f.fail {
		return nil, errors.New("python stopped (killed)")
	}
	out := make([]float64, len(p))
	for i := range out {
		out[i] = 1.5
	}
	return out, nil
}

type world struct {
	repo, state, out string
	tr               *fakeTr
	sc               *fakeSc
	always           map[string]bool
	opens            int
	clock            time.Time
}

func write(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// newWorld: a checkout with the test app (testdata/app) in mobile/src, its
// i18n files, and a .git HEAD; two Latin-script targets.
func newWorld(t *testing.T) *world {
	w := &world{repo: t.TempDir(), state: filepath.Join(t.TempDir(), "state"), out: filepath.Join(t.TempDir(), "out"),
		tr: &fakeTr{}, sc: &fakeSc{}, always: map[string]bool{xID: true}, clock: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
	filepath.Walk("../testdata/app", func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel("../testdata/app", p)
			write(t, filepath.Join(w.repo, "mobile", "src", rel), b)
		}
		return nil
	})
	conf := filepath.Join(w.repo, "i18n")
	write(t, filepath.Join(conf, "source.json"), []byte(`{"branch":"ui","commit":"c0","dir":"mobile/src"}`))
	write(t, filepath.Join(conf, "rules.json"), []byte(`{"text":[{"file":"words.ts","text":"listing","why":"plural"},{"file":"words.ts","text":"listings","why":"plural"}],"not_text":[]}`))
	write(t, filepath.Join(conf, "concepts.json"), []byte(`{"note":"","concepts":{"listing":{"about":"a food listing","terms":["listing","listings"]},"account":{"about":"the account","terms":["log out"]}},"opens":[],"pairs":[["ROLE.give","ROLE.receive"],["GONE.a","ROLE.give"]]}`))
	write(t, filepath.Join(conf, "context.json"), []byte(`{"note":"","contexts":{}}`))
	write(t, filepath.Join(conf, "languages.json"), []byte(`{"source":"en","languages":[{"code":"en","name":"English","native":"English","script":"Latn","dir":"ltr","role":"source"},`+
		`{"code":"es-419","name":"Spanish (Latin America)","native":"español","script":"Latn","dir":"ltr","role":"first"},`+
		`{"code":"pt-BR","name":"Portuguese (Brazil)","native":"português","script":"Latn","dir":"ltr","role":"target"}],"keep":["V3RDE"]}`))
	write(t, filepath.Join(w.repo, ".git", "HEAD"), []byte("ref: refs/heads/services\n"))
	w.commit(t, "1111111111111111111111111111111111111111")
	return w
}

func (w *world) commit(t *testing.T, c string) {
	write(t, filepath.Join(w.repo, ".git", "refs", "heads", "services"), []byte(c+"\n"))
}

func (w *world) run(t *testing.T, dry bool) (Summary, string) {
	t.Helper()
	w.clock = w.clock.Add(time.Hour)
	var log bytes.Buffer
	d := Deps{Log: &log, Now: func() time.Time { return w.clock }, Open: func() (Models, error) {
		w.opens++
		if dry {
			return Models{}, errors.New("dry-run must not open the models")
		}
		return Models{Translator: w.tr, Scorer: w.sc, Settings: muse.Default(),
			Judges: []judge.Model{fakeJudge{"open", w.always}, fakeJudge{"meta", w.always}}}, nil
	}}
	sum, err := Run(context.Background(), Options{Repo: w.repo, State: w.state, Out: w.out, DryRun: dry, Batch: 12, Workers: 2}, d)
	if err != nil {
		t.Fatalf("round: %v\n%s", err, log.String())
	}
	return sum, log.String()
}

func readLive(t *testing.T, w *world, code string) map[string]string {
	t.Helper()
	var p Published
	if err := i18n.ReadJSON(filepath.Join(w.out, code+".json"), &p); err != nil {
		t.Fatal(err)
	}
	return p.Messages
}

func reviewOf(t *testing.T, w *world, tag string) map[string]ReviewItem {
	t.Helper()
	var f struct {
		Round  string       `json:"round"`
		Commit string       `json:"commit"`
		Texts  []ReviewItem `json:"texts"`
	}
	if err := i18n.ReadJSON(filepath.Join(w.state, "review", tag+".json"), &f); err != nil {
		t.Fatal(err)
	}
	out := map[string]ReviewItem{}
	for _, r := range f.Texts {
		out[r.Lang+" "+r.ID] = r
	}
	return out
}

// First round from an empty state: every text is added; a text flagged twice
// goes to review with no translation; a text fixed in the fix round goes live
// with its fix; MetricX runs once for each round of all languages.
func TestRoundFirstAndChange(t *testing.T) {
	w := newWorld(t)
	sum, log := w.run(t, false)
	if sum.Nothing || len(sum.Failed) != 0 {
		t.Fatalf("summary %+v\n%s", sum, log)
	}
	if !strings.Contains(log, "pair GONE.a ROLE.give: no such text") {
		t.Errorf("a stale hand-made pair is not reported:\n%s", log)
	}
	if w.sc.calls != 2 {
		t.Errorf("scorer calls %d, want 2 (round 1 and the fix round, all languages in one call each)", w.sc.calls)
	}
	var plan Plan
	if err := i18n.ReadJSON(filepath.Join(w.state, "rounds", sum.Tag+".json"), &plan); err != nil {
		t.Fatal(err)
	}
	es := plan.Langs["es-419"]
	if es.Status != "done" || len(es.Work) != plan.Texts || es.Work[0].Reason != "added" || len(plan.Added) != plan.Texts {
		t.Fatalf("plan: %+v", es)
	}
	for _, code := range []string{"es-419", "pt-BR"} {
		live := readLive(t, w, code)
		if _, ok := live[xID]; ok {
			t.Errorf("%s: %s failed twice and is new, so it must have no translation: %q", code, xID, live[xID])
		}
		if live[yID] != "F:Collected" {
			t.Errorf("%s: %s = %q, want the fix", code, yID, live[yID])
		}
		if live["ROLE.give"] != "T:Give" {
			t.Errorf("%s: ROLE.give = %q", code, live["ROLE.give"])
		}
	}
	rv := reviewOf(t, w, sum.Tag)
	if r, ok := rv["es-419 "+xID]; !ok || r.From != "none" || len(r.Tried) != 2 || len(r.Reasons) == 0 {
		t.Fatalf("review of %s: %+v", xID, rv)
	}
	if _, ok := rv["es-419 "+yID]; ok {
		t.Errorf("%s was fixed; it is not for review", yID)
	}
	var idx Index
	if err := i18n.ReadJSON(filepath.Join(w.out, "index.json"), &idx); err != nil || idx.Round != sum.Tag || len(idx.Languages) != 3 {
		t.Fatalf("index: %+v %v", idx, err)
	}

	// the app changes: one text changed, one removed; the judges now also fail the changed text
	words := filepath.Join(w.repo, "mobile", "src", "words.ts")
	b, _ := os.ReadFile(words)
	s := strings.Replace(string(b), `expired: "Expired"`, `expired: "Gone"`, 1)
	s = strings.Replace(s, `title: "Available now", `, ``, 1)
	write(t, words, []byte(s))
	w.commit(t, "2222222222222222222222222222222222222222")
	w.always["RECEIVE.expired"] = true
	calls := w.tr.calls
	sum2, log2 := w.run(t, false)
	if err := i18n.ReadJSON(filepath.Join(w.state, "rounds", sum2.Tag+".json"), &plan); err != nil {
		t.Fatal(err)
	}
	why := map[string]string{}
	for _, x := range plan.Langs["es-419"].Work {
		why[x.ID] = x.Reason
	}
	if why["RECEIVE.expired"] != "changed" || why[xID] != "missing" || len(why) >= plan.Texts {
		t.Fatalf("work of the second round: %v\n%s", why, log2)
	}
	if strings.Join(plan.Removed, ",") != "RECEIVE.title" || w.tr.calls == calls {
		t.Fatalf("removed %v, calls %d", plan.Removed, w.tr.calls-calls)
	}
	live := readLive(t, w, "es-419")
	if _, ok := live["RECEIVE.title"]; ok {
		t.Errorf("a removed text is still live")
	}
	if live["RECEIVE.expired"] != "T:Expired" {
		t.Errorf("RECEIVE.expired failed twice: it must keep the last good translation, got %q", live["RECEIVE.expired"])
	}
	if r := reviewOf(t, w, sum2.Tag)["es-419 RECEIVE.expired"]; r.From != "live" || r.Kept != "T:Expired" || r.English != "Gone" {
		t.Fatalf("review: %+v", r)
	}
	var src map[string]string
	if err := i18n.ReadJSON(filepath.Join(w.state, "live", "es-419.source.json"), &src); err != nil || src["RECEIVE.expired"] != "Expired" {
		t.Fatalf("the kept translation is for the old English, so the next round tries again: %v %v", src["RECEIVE.expired"], err)
	}
	if !strings.Contains(log2, "[7/7] round "+sum2.Tag+" done") {
		t.Errorf("no last progress line:\n%s", log2)
	}
}

// Nothing changed: a quick exit 0 with one line, no model, no new round.
func TestNothingChanged(t *testing.T) {
	w := newWorld(t)
	first, _ := w.run(t, false)
	opens, calls := w.opens, w.tr.calls
	sum, log := w.run(t, false)
	if !sum.Nothing || sum.Tag != first.Tag || w.opens != opens || w.tr.calls != calls {
		t.Fatalf("summary %+v, opens %d, calls %d\n%s", sum, w.opens-opens, w.tr.calls-calls, log)
	}
	if !strings.Contains(log, "nothing changed since round "+first.Tag) {
		t.Fatalf("log:\n%s", log)
	}
	rounds, _ := os.ReadDir(filepath.Join(w.state, "rounds"))
	if len(rounds) != 1 {
		t.Fatalf("rounds: %d", len(rounds))
	}
	// a new commit with the same text is also no round
	w.commit(t, "3333333333333333333333333333333333333333")
	if sum, _ := w.run(t, false); !sum.Nothing {
		t.Fatal("same text, new commit: want nothing changed")
	}
}

// Dry run: the lists, no model, nothing written.
func TestDryRun(t *testing.T) {
	w := newWorld(t)
	sum, log := w.run(t, true)
	if w.opens != 0 || w.tr.calls != 0 || sum.Nothing {
		t.Fatalf("opens %d calls %d %+v", w.opens, w.tr.calls, sum)
	}
	for _, want := range []string{"dry-run es-419", "dry-run pt-BR", "added   " + " " + xID, "nothing written"} {
		if !strings.Contains(log, want) {
			t.Errorf("no %q in:\n%s", want, log)
		}
	}
	for _, d := range []string{w.state, w.out} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("%s was written", d)
		}
	}
}

// First run with committed results in the checkout: they are the start, so
// only the texts that changed since then are translated.
func TestSeed(t *testing.T) {
	w := newWorld(t)
	files, _ := i18n.ReadTree(w.repo, "mobile/src")
	var lists i18n.Lists
	i18n.ReadJSON(filepath.Join(w.repo, "i18n", "rules.json"), &lists)
	msgs, _, _ := i18n.All(files, &lists)
	var cf i18n.ConceptFile
	i18n.ReadJSON(filepath.Join(w.repo, "i18n", "concepts.json"), &cf)
	src := i18n.Source{Branch: "ui", Commit: "c0", Dir: "mobile/src"}
	g, _, err := Graph(src, files, msgs, cf)
	if err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(w.repo, "i18n")
	write(t, filepath.Join(conf, "en.json"), i18n.Marshal(i18n.EnFile{Source: src, Language: "en", Messages: msgs}))
	write(t, filepath.Join(conf, "graph.json"), i18n.Marshal(g))
	seed := rung.Result{Run: "old", Lang: "es-419", Commit: "c0", Model: muse.Model, K: 1, Dedupe: true, Missing: []string{}}
	for _, m := range msgs {
		if m.ID != "ROLE.receive" {
			seed.Messages = append(seed.Messages, rung.Chosen{ID: m.ID, Text: "S:" + m.Text})
		}
	}
	write(t, filepath.Join(conf, "es-419.json"), i18n.Marshal(seed))
	sum, log := w.run(t, true)
	var _ = sum
	if !strings.Contains(log, "seed es-419") || !strings.Contains(log, "dry-run es-419   1 texts (added 0, missing 1") {
		t.Fatalf("log:\n%s", log)
	}
	if !strings.Contains(log, "dry-run pt-BR") || strings.Contains(log, "seed pt-BR") {
		t.Fatalf("pt-BR has no seed:\n%s", log)
	}
}

// X49b review: a scorer failure does not lose the round (scores only rank); a language that failed is tried
// again on the next deploy with no text change; a first round stopped early keeps its languages' work.
func TestReviewFixes(t *testing.T) {
	w := newWorld(t)
	w.sc.fail = true
	sum, log := w.run(t, false)
	if sum.Nothing || len(sum.Failed) != 0 || !strings.Contains(log, "stay unscored") {
		t.Fatalf("scorer failure: %+v\n%s", sum, log)
	}
	if _, err := os.Stat(filepath.Join(w.out, "index.json")); err != nil {
		t.Fatalf("nothing published after a scorer failure: %v", err)
	}
	var live rung.Result
	if err := i18n.ReadJSON(filepath.Join(w.state, "live", "es-419.json"), &live); err != nil || !live.Unscored {
		t.Fatalf("the live file must say unscored: %v %v", err, live.Unscored)
	}
	w.sc.fail = false
	// the last round says es-419 failed: the next run with the same text is a round, not "nothing changed"
	var st StateFile
	if err := i18n.ReadJSON(filepath.Join(w.state, "round.json"), &st); err != nil {
		t.Fatal(err)
	}
	st.Failed = []string{"es-419"}
	if err := writeJSON(filepath.Join(w.state, "round.json"), st); err != nil {
		t.Fatal(err)
	}
	var src map[string]string
	_ = i18n.ReadJSON(filepath.Join(w.state, "live", "es-419.source.json"), &src)
	for id := range src {
		src[id] = "old English"
		break
	}
	_ = writeJSON(filepath.Join(w.state, "live", "es-419.source.json"), src)
	if sum, log := w.run(t, false); sum.Nothing {
		t.Fatalf("a failed language was not tried again:\n%s", log)
	}
}
