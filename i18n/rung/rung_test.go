package rung

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"v3rde/i18n"
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

type fakeSc struct{ got []Pair }

func (f *fakeSc) Score(ctx context.Context, p []Pair) ([]float64, error) {
	f.got = p
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
	tr := &fakeTr{reply: func(user string, n int32) (string, error) {
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
	sc := &fakeSc{}
	var led bytes.Buffer
	res, err := Run(context.Background(), cfg(3), msgs, tr, sc, &led)
	if err != nil {
		t.Fatal(err)
	}
	if tr.calls != 9 { // 3 messages x 3 tries; FRAME.brand is copied
		t.Fatalf("calls %d, want 9", tr.calls)
	}
	lines := strings.Split(strings.TrimSpace(led.String()), "\n")
	if len(lines) != 10 {
		t.Fatalf("ledger lines %d, want 10", len(lines))
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
	if first.ID != "VOICE.rename" || first.Try != 1 || first.Settings.TopK != -1 || first.Commit != "c0" {
		t.Errorf("ledger order or fields: %+v", first)
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
