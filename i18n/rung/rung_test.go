package rung

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"v3rde/i18n"
	"v3rde/i18n/mask"
	"v3rde/i18n/muse"
)

type fakeTr struct {
	calls int32
	reply func(user string, n int32) (string, error)
}

func (f *fakeTr) Chat(ctx context.Context, sys, user string) (muse.Reply, error) {
	n := atomic.AddInt32(&f.calls, 1)
	txt, err := f.reply(user, n)
	return muse.Reply{Text: txt, ReasoningTokens: 10}, err
}

type fakeSc struct {
	got      []Pair
	checkErr error
	ledger   *bytes.Buffer // the ledger as it was when Score was called
	seen     int           // try lines in the ledger at that time
}

func (f *fakeSc) Check(ctx context.Context) error { return f.checkErr }

func (f *fakeSc) Score(ctx context.Context, p []Pair) ([]float64, error) {
	f.got = p
	if f.ledger != nil {
		f.seen = strings.Count(f.ledger.String(), `"kind":"try"`)
	}
	out := make([]float64, len(p))
	for i, x := range p {
		out[i] = float64(len([]rune(x.Hypothesis))) / 10 // shorter is "better" in this fake
	}
	return out, nil
}

var msgs = []i18n.Message{
	{ID: "VOICE.rename", Text: "Item {n}: “{from}” to “{to}”"},
	{ID: "FRAME.brand", Text: "V3RDE"},
	{ID: "CAPTURE.retake", Text: "Retake"},
	{ID: "RECEIVE.gone", Text: "This food is gone"},
}

func cfg(k int) Config {
	return Config{Run: "t1", Lang: i18n.Language{Code: "es-419", Name: "Spanish (Latin America)"}, Keep: []string{"V3RDE", "WhatsApp"},
		K: k, Workers: 3, Settings: muse.Default(), Commit: "c0"}
}

// k tries per message; a lost tag and no answer are recorded and not kept;
// a message of only keep names is copied with no call; the lowest score wins.
func TestRung(t *testing.T) {
	// n counts the tries of each message: the shared call count depends on the
	// order of the workers (the test failed about 1 time in 200 with it)
	var mu sync.Mutex
	per := map[string]int32{}
	tr := &fakeTr{reply: func(user string, _ int32) (string, error) {
		id := strings.SplitN(strings.TrimPrefix(user, "Message id: "), "\n", 2)[0]
		mu.Lock()
		per[id]++
		n := per[id]
		mu.Unlock()
		switch {
		case strings.Contains(user, "VOICE.rename"):
			if n%2 == 0 {
				return "Artículo <x1/>: de “<x2/>”", nil // lost <x3/>
			}
			return "Artículo <x1/>: de “<x2/>” a “<x3/>”", nil
		case strings.Contains(user, "CAPTURE.retake"):
			if strings.Count(user, "") > 0 && n%3 == 0 {
				return "Tomar de nuevo la foto", nil
			}
			return "Repetir", nil
		default:
			return "", muse.ErrNoAnswer
		}
	}}
	var led bytes.Buffer
	sc := &fakeSc{ledger: &led}
	res, err := Run(context.Background(), cfg(3), msgs, tr, sc, &led)
	if err != nil {
		t.Fatal(err)
	}
	if tr.calls != 9 { // 3 messages x 3 tries; FRAME.brand is copied
		t.Fatalf("calls %d, want 9", tr.calls)
	}
	lines := strings.Split(strings.TrimSpace(led.String()), "\n")
	tries := strings.Count(led.String(), `"kind":"try"`)
	if tries != 10 || sc.seen != 10 {
		t.Fatalf("try lines %d (%d before the scoring), want 10 (10)", tries, sc.seen)
	}
	got := map[string]Chosen{}
	for _, c := range res.Messages {
		got[c.ID] = c
	}
	if got["FRAME.brand"].Text != "V3RDE" || got["FRAME.brand"].Score != nil {
		t.Errorf("brand: %+v", got["FRAME.brand"])
	}
	if got["VOICE.rename"].Text != "Artículo {n}: de “{from}” a “{to}”" {
		t.Errorf("rename: %+v", got["VOICE.rename"])
	}
	if got["CAPTURE.retake"].Text != "Repetir" || got["CAPTURE.retake"].Candidates != 3 {
		t.Errorf("retake: %+v", got["CAPTURE.retake"])
	}
	if strings.Join(res.Missing, ",") != "RECEIVE.gone" {
		t.Errorf("missing %v", res.Missing)
	}
	var first Entry
	json.Unmarshal([]byte(lines[0]), &first)
	if first.Kind != "try" || first.Settings.TopK != -1 || first.Commit != "c0" {
		t.Errorf("ledger fields: %+v", first)
	}
	if n := strings.Count(led.String(), `"kind":"score"`); n != len(sc.got) || n < 3 { // one per scored answer
		t.Errorf("score lines %d, scored pairs %d", n, len(sc.got))
	}
	if strings.Contains(led.String(), `"error":"tags: tags not exactly once`) == false {
		t.Error("the lost tag is not in the ledger")
	}
	for _, p := range sc.got {
		if strings.Contains(p.Hypothesis, "<x") {
			t.Errorf("the scorer got a masked text: %q", p.Hypothesis)
		}
	}
}

// Score rows must be the same pairs in the same order, each in 0..25.
func TestReadScores(t *testing.T) {
	p := []Pair{{"a", "b"}, {"c", "d"}}
	ok := `{"source":"a","hypothesis":"b","prediction":1.5}` + "\n" + `{"source":"c","hypothesis":"d","prediction":20}` + "\n"
	if s, err := ReadScores(strings.NewReader(ok), p); err != nil || s[0] != 1.5 || s[1] != 20 {
		t.Fatalf("%v %v", s, err)
	}
	for _, bad := range []string{
		`{"source":"c","hypothesis":"d","prediction":1}`,
		`{"source":"a","hypothesis":"b","prediction":30}` + "\n" + `{"source":"c","hypothesis":"d","prediction":1}`,
		`{"source":"a","hypothesis":"b","prediction":1}`,
	} {
		if _, err := ReadScores(strings.NewReader(bad), p); err == nil {
			t.Errorf("must fail: %s", bad)
		}
	}
}

// A resume run fills the missing ids; the order is the en.json order; another
// language or model is refused.
func TestMerge(t *testing.T) {
	one, two := 1.0, 2.0
	old := Result{Run: "r1", Lang: "es-419", Commit: "c0", Model: "m", K: 1,
		Messages: []Chosen{{ID: "FRAME.brand", Text: "V3RDE"}, {ID: "RECEIVE.gone", Text: "Ya no está", Score: &one}},
		Missing:  []string{"VOICE.rename", "CAPTURE.retake"}}
	add := Result{Run: "r2", Lang: "es-419", Commit: "c0", Model: "m", K: 1,
		Messages: []Chosen{{ID: "VOICE.rename", Text: "Artículo {n}: de “{from}” a “{to}”", Score: &two}},
		Missing:  []string{"CAPTURE.retake"}}
	got, err := Merge(old, add, msgs)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range got.Messages {
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, ",") != "VOICE.rename,FRAME.brand,RECEIVE.gone" || strings.Join(got.Missing, ",") != "CAPTURE.retake" ||
		got.Run != "r2" || strings.Join(got.Previous, ",") != "r1" {
		t.Fatalf("%v %v %s %v", ids, got.Missing, got.Run, got.Previous)
	}
	add.Model = "other"
	if _, err := Merge(old, add, msgs); err == nil {
		t.Fatal("another model must be refused")
	}
}

// Each English text is translated once; the other ids reuse it (same_as).
func TestDedupe(t *testing.T) {
	ms := []i18n.Message{{ID: "ROLE.close", Text: "Close"}, {ID: "RECEIVE.close", Text: "Close"}, {ID: "CAPTURE.retake", Text: "Retake"}, {ID: "CAPTURE.closeFlow", Text: "Close"}}
	tr := &fakeTr{reply: func(user string, n int32) (string, error) {
		if strings.Contains(user, "Close") {
			return "Cerrar", nil
		}
		return "Repetir", nil
	}}
	var led bytes.Buffer
	res, err := Run(context.Background(), cfg(1), ms, tr, &fakeSc{}, &led)
	if err != nil {
		t.Fatal(err)
	}
	if tr.calls != 2 || !res.Dedupe || len(res.Messages) != 4 {
		t.Fatalf("calls %d dedupe %v messages %d", tr.calls, res.Dedupe, len(res.Messages))
	}
	for _, c := range res.Messages {
		want := map[string]string{"ROLE.close": "", "RECEIVE.close": "ROLE.close", "CAPTURE.closeFlow": "ROLE.close", "CAPTURE.retake": ""}[c.ID]
		if c.SameAs != want || (strings.HasSuffix(c.ID, "lose") || c.ID == "CAPTURE.closeFlow") && c.Text != "Cerrar" {
			t.Errorf("%+v", c)
		}
	}
	if !strings.Contains(led.String(), `"also":["RECEIVE.close","CAPTURE.closeFlow"]`) {
		t.Errorf("ledger has no also list: %s", led.String())
	}
}

// When the scorer cannot run, no request goes to the model.
func TestCheckFirst(t *testing.T) {
	tr := &fakeTr{reply: func(string, int32) (string, error) { return "x", nil }}
	_, err := Run(context.Background(), cfg(1), msgs, tr, &fakeSc{checkErr: errors.New("docker does not answer")}, &bytes.Buffer{})
	if err == nil || tr.calls != 0 || !strings.Contains(err.Error(), "no request was sent") {
		t.Fatalf("%v, %d calls", err, tr.calls)
	}
}

// Complete needs every message of en.json (also a new one), the same commit,
// model, k and prompt, and dedupe.
func TestComplete(t *testing.T) {
	ms := []i18n.Message{{ID: "a"}}
	r := Result{Commit: "c", Model: "m", K: 1, Dedupe: true, Prompt: muse.PromptContext, Messages: []Chosen{{ID: "a"}}, Missing: []string{}}
	if !Complete(r, ms, "c", "m", 1, muse.PromptContext) {
		t.Fatal("complete")
	}
	for _, bad := range []Result{
		{Commit: "c", Model: "m", K: 1, Dedupe: false, Prompt: muse.PromptContext, Messages: r.Messages},
		{Commit: "c", Model: "m", K: 1, Dedupe: true, Prompt: muse.PromptContext, Messages: r.Messages, Missing: []string{"b"}},
		{Commit: "d", Model: "m", K: 1, Dedupe: true, Prompt: muse.PromptContext, Messages: r.Messages},
		{Commit: "c", Model: "m", K: 1, Dedupe: true, Messages: r.Messages}, // plain prompt
	} {
		if Complete(bad, ms, "c", "m", 1, muse.PromptContext) {
			t.Errorf("not complete: %+v", bad)
		}
	}
	if Complete(r, append(ms, i18n.Message{ID: "new"}), "c", "m", 1, muse.PromptContext) {
		t.Error("a text that is new in en.json makes the result not complete")
	}
	if got := Todo(r, append(ms, i18n.Message{ID: "new"})); len(got) != 1 || got[0].ID != "new" {
		t.Errorf("todo %v", got)
	}
}

// Collapse keeps the lowest-scored translation of a shared text for every id.
func TestCollapse(t *testing.T) {
	a, b := 1.28, 0.99
	ms := []i18n.Message{{ID: "x.no_blob", Text: "no blob"}, {ID: "y.no_blob", Text: "no blob"}, {ID: "z", Text: "Retake"}}
	r := Result{Messages: []Chosen{{ID: "x.no_blob", Text: "无 blob", Score: &a}, {ID: "y.no_blob", Text: "无斑点", Score: &b}, {ID: "z", Text: "重拍"}}}
	got := Collapse(r, ms)
	if !got.Dedupe || len(got.Messages) != 3 || len(got.Missing) != 0 {
		t.Fatalf("%+v", got)
	}
	x, y := got.Messages[0], got.Messages[1]
	if x.Text != "无斑点" || x.SameAs != "y.no_blob" || y.Text != "无斑点" || y.SameAs != "" || got.Messages[2].Text != "重拍" {
		t.Fatalf("%+v %+v", x, y)
	}
}

// With a context, the dedupe key is the text and its context: the same English
// with two contexts gets two requests; each prompt has its own context block.
func TestContextKey(t *testing.T) {
	ms := []i18n.Message{{ID: "a.close", Text: "Close"}, {ID: "b.close", Text: "Close"}, {ID: "c.close", Text: "Close"}}
	ctxs := map[string]string{"a.close": "camera", "b.close": "sheet", "c.close": "sheet"}
	var users []string
	var mu sync.Mutex
	tr := &fakeTr{reply: func(user string, n int32) (string, error) {
		mu.Lock()
		users = append(users, user)
		mu.Unlock()
		return "Cerrar", nil
	}}
	c := cfg(1)
	c.Prompt = muse.PromptContext
	c.Key = func(m i18n.Message) string { return m.Text + "|" + ctxs[m.ID] }
	c.About = func(id string, mk mask.Masked) string { return "What it does: closes the " + ctxs[id] }
	res, err := Run(context.Background(), c, ms, tr, &fakeSc{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if tr.calls != 2 || res.Prompt != muse.PromptContext {
		t.Fatalf("calls %d prompt %s", tr.calls, res.Prompt)
	}
	joined := strings.Join(users, "\n")
	if !strings.Contains(joined, "Context:\nWhat it does: closes the camera") || !strings.Contains(joined, "closes the sheet") {
		t.Fatalf("prompts: %s", joined)
	}
	if res.Messages[2].SameAs != "b.close" {
		t.Errorf("c.close must reuse b.close: %+v", res.Messages[2])
	}
}

// With no scorer the run translates and keeps the first try (unscored); then
// ScoreAll scores two results in one call, writes score lines and keeps the
// lowest-scored try.
func TestBatchScore(t *testing.T) {
	dir := t.TempDir()
	ms := []i18n.Message{{ID: "CAPTURE.retake", Text: "Retake"}, {ID: "RECEIVE.gone", Text: "This food is gone"}}
	var paths []string
	for _, lang := range []string{"es-419", "pt-BR"} {
		tr := &fakeTr{reply: func(user string, n int32) (string, error) {
			if strings.Contains(user, "Retake") {
				return map[int32]string{1: "Volver a tomar la foto otra vez", 2: "Repetir"}[(n-1)%2+1], nil
			}
			return "Ya no está", nil
		}}
		c := cfg(2)
		c.Workers = 1 // fixed order of the answers
		c.Lang.Code = lang
		d := filepath.Join(dir, lang)
		os.MkdirAll(d, 0o755)
		lf, _ := os.Create(filepath.Join(d, lang+".ledger.jsonl"))
		res, err := Run(context.Background(), c, ms, tr, nil, lf)
		lf.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !res.Unscored || res.Messages[0].Score != nil {
			t.Fatalf("an unscored run: %+v", res)
		}
		b, _ := json.Marshal(res)
		p := filepath.Join(d, lang+".json")
		os.WriteFile(p, b, 0o644)
		paths = append(paths, p)
	}
	sc := &countSc{}
	n, err := ScoreAll(context.Background(), paths, ms, sc)
	if err != nil {
		t.Fatal(err)
	}
	if sc.calls != 1 || n != 8 {
		t.Fatalf("scorer calls %d, pairs %d; want 1 call, 8 pairs", sc.calls, n)
	}
	for _, p := range paths {
		var r Result
		if err := i18n.ReadJSON(p, &r); err != nil {
			t.Fatal(err)
		}
		if r.Unscored || r.Messages[0].Text != "Repetir" || r.Messages[0].Score == nil {
			t.Errorf("%s: %+v", p, r.Messages[0])
		}
		led, _ := os.ReadFile(filepath.Join(filepath.Dir(p), r.Lang+".ledger.jsonl"))
		if c := strings.Count(string(led), `"kind":"score"`); c != 4 {
			t.Errorf("%s: %d score lines, want 4", p, c)
		}
	}
	if n, err := ScoreAll(context.Background(), paths, ms, sc); err != nil || n != 0 || sc.calls != 1 {
		t.Fatalf("a second run scores nothing again: %d pairs, %d calls, %v", n, sc.calls, err)
	}
}

type countSc struct{ calls int }

func (c *countSc) Check(ctx context.Context) error { return nil }
func (c *countSc) Score(ctx context.Context, p []Pair) ([]float64, error) {
	c.calls++
	out := make([]float64, len(p))
	for i, x := range p {
		out[i] = float64(len([]rune(x.Hypothesis))) / 10
	}
	return out, nil
}

// A fix try (L4j) is kept before a try without a fix, also when its score is
// worse, and the result marks it revised.
func TestFixKept(t *testing.T) {
	one, three := 1.0, 3.0
	es := []Entry{{ID: "a", Try: 1, Text: "Refazer", Score: &one}, {ID: "a", Try: 1, Text: "Tirar outra", Score: &three, Fix: "noun on a button"},
		{ID: "b", Try: 1, Text: "Ok", Score: &one}}
	best, _ := choose(es)
	if best["a"].Text != "Tirar outra" || !best["a"].Revised || best["b"].Text != "Ok" || best["b"].Revised {
		t.Fatalf("%+v %+v", best["a"], best["b"])
	}
}

// A spread try keeps its spread_from after a rescore from the ledger (cmd/score).
func TestSpreadKept(t *testing.T) {
	one, two := 1.0, 2.0
	res := Result{Run: "r1-spread", Previous: []string{"r0", "r1"}}
	tries := []Entry{{Run: "r0", ID: "a", Try: 1, Text: "Anúncio", Score: &one},
		{Run: "r1-spread", ID: "a", Try: 1, Text: "Publicação", Score: &two, Fix: "A related text was corrected", SpreadFrom: "b"},
		{Run: "old", ID: "c", Try: 1, Text: "x", Score: &one}}
	out := Rescore(res, []i18n.Message{{ID: "a"}, {ID: "c"}}, tries)
	if len(out.Messages) != 1 || out.Messages[0].Text != "Publicação" || out.Messages[0].SpreadFrom != "b" || !out.Messages[0].Revised ||
		len(out.Missing) != 1 || out.Missing[0] != "c" {
		t.Fatalf("%+v", out)
	}
}

// Batch: one call for the group; a text that the answer leaves out and a text
// with a lost tag get one call each; keep names are copied; the ledger shows the group.
func TestBatchRun(t *testing.T) {
	ms := []i18n.Message{
		{ID: "VOICE.rename", Text: "Item {n}: “{from}” to “{to}”"},
		{ID: "FRAME.brand", Text: "V3RDE"},
		{ID: "CAPTURE.retake", Text: "Retake"},
		{ID: "RECEIVE.gone", Text: "This food is gone"},
		{ID: "RECEIVE.gone2", Text: "This food is gone"}, // same text: reuses RECEIVE.gone
	}
	var mu sync.Mutex
	var batches, singles int
	tr := &fakeTr{reply: func(user string, _ int32) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(user, "Texts:\n") {
			batches++
			return "{\"VOICE.rename\": \"Artículo <x1/>: de “<x2/>”\", \"RECEIVE.gone\": \"Esta comida ya no está\"}", nil // lost <x3/>; no retake
		}
		singles++
		switch {
		case strings.Contains(user, "VOICE.rename"):
			return "Artículo <x1/>: de “<x2/>” a “<x3/>”", nil
		case strings.Contains(user, "CAPTURE.retake"):
			return "Repetir", nil
		}
		return "", muse.ErrNoAnswer
	}}
	c := cfg(1)
	c.Prompt = muse.PromptBatch
	c.Batch = func(in []i18n.Message) [][]i18n.Message { return [][]i18n.Message{in} }
	var led bytes.Buffer
	res, err := Run(context.Background(), c, ms, tr, nil, &led)
	if err != nil {
		t.Fatal(err)
	}
	if batches != 1 || singles != 2 {
		t.Fatalf("batch calls %d, single calls %d; want 1, 2", batches, singles)
	}
	got := map[string]Chosen{}
	for _, x := range res.Messages {
		got[x.ID] = x
	}
	if got["VOICE.rename"].Text != "Artículo {n}: de “{from}” a “{to}”" || got["CAPTURE.retake"].Text != "Repetir" ||
		got["RECEIVE.gone"].Text != "Esta comida ya no está" || got["RECEIVE.gone2"].SameAs != "RECEIVE.gone" ||
		got["FRAME.brand"].Text != "V3RDE" || len(res.Missing) != 0 || res.Prompt != muse.PromptBatch {
		t.Fatalf("%+v missing %v", res.Messages, res.Missing)
	}
	if n := strings.Count(led.String(), `"batch":"VOICE.rename+3"`); n != 3 {
		t.Fatalf("batch entries in the ledger: %d, want 3", n)
	}
}

// Calls and TextScores: one call for each single try and for each batch group; copies and other runs are not counted.
func TestCalls(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.ledger.jsonl")
	lines := `{"kind":"try","run":"r1","id":"a","try":1,"batch":"a+2"}
{"kind":"try","run":"r1","id":"b","try":1,"batch":"a+2"}
{"kind":"try","run":"r1","id":"c","try":1}
{"kind":"try","run":"r1","id":"d","try":0,"copied":true}
{"kind":"score","run":"r1","id":"c","try":1,"score":1.5}
{"kind":"try","run":"r2","id":"a","try":1,"batch":"a+1"}
{"kind":"try","run":"old","id":"a","try":1}
`
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := Calls(p, Result{Run: "r2", Previous: []string{"r1"}})
	if err != nil || n != 3 {
		t.Fatalf("calls %d (%v), want 3", n, err)
	}
	if err := os.WriteFile(p, []byte(`{"kind":"try","run":"r1","id":"c","try":1,"text":"Hola"}
{"kind":"score","run":"r1","id":"c","try":1,"score":2.5}
{"kind":"try","run":"r1","id":"c","try":2,"text":"Hola"}
{"kind":"score","run":"r1","id":"c","try":2,"score":1.5}
{"kind":"try","run":"r1","id":"d","try":1,"text":"x","error":"tags"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ts, err := TextScores(p)
	if err != nil || len(ts) != 1 || ts["c\x00Hola"] != 1.5 {
		t.Fatalf("text scores %v (%v)", ts, err)
	}
}
