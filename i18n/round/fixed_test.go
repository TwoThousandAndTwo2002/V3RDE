package round

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"v3rde/i18n"
	"v3rde/i18n/rung"
)

// X66: which entries of fixed.json a round uses, and why the others are not.
func TestActiveFixed(t *testing.T) {
	cur := []i18n.Message{{ID: "A.one", Text: "Nothing is offered now."}, {ID: "A.two", Text: "Looking for {name} in the photo…"}}
	langs := i18n.Languages{Source: "en", Languages: []i18n.Language{{Code: "en", Role: "source"}, {Code: "yo", Role: "target"}}}
	f := FixedFile{Texts: map[string]map[string]FixedText{
		"yo": {
			"A.one":  {English: "Nothing is offered now.", Text: "Kò sí nkan."},
			"A.two":  {English: "Looking for {name} in the photo…", Text: "N wá {name} nínú fọ́tò…"},
			"A.gone": {English: "x", Text: "y"},
		},
		"en": {"A.one": {English: "Nothing is offered now.", Text: "no"}},
		"de": {"A.one": {English: "Nothing is offered now.", Text: "Nichts"}},
	}}
	active, warn := ActiveFixed(f, cur, langs)
	if len(active) != 1 || active["yo"]["A.one"] != "Kò sí nkan." || active["yo"]["A.two"] != "N wá {name} nínú fọ́tò…" {
		t.Fatalf("active %v", active)
	}
	if len(warn) != 3 || !strings.Contains(warn[0], "de A.one: de is not a target") || !strings.Contains(warn[1], "en A.one: en is not a target") ||
		!strings.Contains(warn[2], "yo A.gone: no such text") {
		t.Fatalf("warnings %q", warn)
	}
	for name, x := range map[string]FixedText{
		"old English":  {English: "Nothing now.", Text: "Kò sí nkan."},
		"empty":        {English: "Nothing is offered now.", Text: ""},
		"too long":     {English: "Nothing is offered now.", Text: strings.Repeat("a", FixedMaxLen+1)},
		"no {name}":    {English: "Looking for {name} in the photo…", Text: "N wá nínú fọ́tò…"},
		"other {name}": {English: "Looking for {name} in the photo…", Text: "N wá {n} nínú fọ́tò…"},
	} {
		id := "A.one"
		if strings.Contains(name, "name") {
			id = "A.two"
		}
		a, w := ActiveFixed(FixedFile{Texts: map[string]map[string]FixedText{"yo": {id: x}}}, cur, langs)
		if len(a) != 0 || len(w) != 1 {
			t.Errorf("%s: used %v, warnings %q", name, a, w)
		}
	}
	if f, err := ReadFixed(filepath.Join(t.TempDir(), "fixed.json")); err != nil || len(f.Texts) != 0 {
		t.Fatalf("no file: %+v %v", f, err)
	}
	// X66 review: an unknown key is left out; a typo of "text" is an empty text (a warning, not a stop)
	p := filepath.Join(t.TempDir(), "fixed.json")
	write(t, p, []byte(`{"note":"n","texts":{"yo":{"A.one":{"en":"Nothing is offered now.","txt":"Kò sí nkan.","lang":"yo"}}}}`))
	f2, err := ReadFixed(p)
	if err != nil {
		t.Fatal(err)
	}
	if a, w := ActiveFixed(f2, cur, langs); len(a) != 0 || len(w) != 1 || !strings.Contains(w[0], "empty") {
		t.Fatalf("typo: %v %q", a, w)
	}
	write(t, p, []byte(`{"texts":`))
	if _, err := ReadFixed(p); err == nil {
		t.Fatal("not JSON: want an error (the round logs it and goes on)")
	}
}

// X66: ApplyFixed puts the text in the order of the app, off Missing and the review list (this language only).
func TestApplyFixed(t *testing.T) {
	cur := []i18n.Message{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}}
	res := rung.Result{Messages: []rung.Chosen{{ID: "a", Text: "ta"}, {ID: "c", Text: "tc"}}, Missing: []string{"b"}}
	src := map[string]string{"a": "A"}
	rv := []ReviewItem{{Lang: "yo", ID: "b"}, {Lang: "es-419", ID: "b"}, {Lang: "yo", ID: "c"}}
	res.Messages = append(res.Messages, rung.Chosen{ID: "c2", Text: "tb", SameAs: "b", Fixed: true}) // not in cur: left out
	res.Messages[1].SameAs, res.Messages[1].Fixed = "b", true                                         // c reused b; an old mark
	out, rv2 := ApplyFixed("yo", res, src, rv, cur, map[string]string{"b": "person b"})
	if out.Messages[2].SameAs != "" || out.Messages[2].Fixed {
		t.Fatalf("c is its own text again, not fixed: %+v", out.Messages[2])
	}
	if len(out.Messages) != 3 || out.Messages[1] != (rung.Chosen{ID: "b", Text: "person b", Fixed: true}) || len(out.Missing) != 0 || src["b"] != "B" {
		t.Fatalf("result %+v src %v", out, src)
	}
	if len(rv2) != 2 || rv2[0].Lang != "es-419" || rv2[1].ID != "c" {
		t.Fatalf("review %+v", rv2)
	}
}

// X66 (T9 Nov 2, test finding 22): a round with i18n/fixed.json. The fixed text is live, it is not translated and not
// for review; a fixed.json change alone is a round with no model call; an English change makes the fixed text old.
func TestRoundFixedText(t *testing.T) {
	w := newWorld(t)
	conf := filepath.Join(w.repo, "i18n")
	write(t, filepath.Join(conf, "fixed.json"), []byte(`{"note":"","texts":{"es-419":{"`+xID+`":{"en":"Log out","text":"Cerrar sesión","by":"T9","date":"2026-11-02"}}}}`))
	sum, log := w.run(t, false)
	if !strings.Contains(log, "fixed.json: 1 texts a person gave are used as they are") {
		t.Fatalf("log:\n%s", log)
	}
	if live := readLive(t, w, "es-419"); live[xID] != "Cerrar sesión" {
		t.Fatalf("es-419 %s = %q (the judges fail every model text of it)", xID, live[xID])
	}
	if _, ok := readLive(t, w, "pt-BR")[xID]; ok {
		t.Fatal("pt-BR has no fixed text: its failed text has no translation")
	}
	var plan Plan
	if err := i18n.ReadJSON(filepath.Join(w.state, "rounds", sum.Tag+".json"), &plan); err != nil {
		t.Fatal(err)
	}
	for _, x := range plan.Langs["es-419"].Work {
		if x.ID == xID {
			t.Fatal("a fixed text is in the work list")
		}
	}
	rv := reviewOf(t, w, sum.Tag)
	if _, ok := rv["es-419 "+xID]; ok {
		t.Fatal("a fixed text is on the review list")
	}
	if _, ok := rv["pt-BR "+xID]; !ok {
		t.Fatal("the other language's text stays on the review list")
	}
	var lf rung.Result
	if err := i18n.ReadJSON(filepath.Join(w.state, "live", "es-419.json"), &lf); err != nil {
		t.Fatal(err)
	}
	for _, c := range lf.Messages {
		if c.ID == xID && (!c.Fixed || c.Score != nil) {
			t.Fatalf("the live entry %+v", c)
		}
	}

	// only fixed.json changes: a round, no model call, the new text is live
	write(t, filepath.Join(conf, "fixed.json"), []byte(`{"note":"","texts":{"es-419":{"`+xID+`":{"en":"Log out","text":"Salir","by":"T9","date":"2026-11-02"}},`+
		`"pt-BR":{"`+xID+`":{"en":"Log out","text":"Sair","by":"T9","date":"2026-11-02"}}}}`))
	w.commit(t, "4444444444444444444444444444444444444444")
	calls, opens := w.tr.calls, w.opens
	sum2, log2 := w.run(t, false)
	if sum2.Nothing || w.tr.calls != calls || w.opens != opens {
		t.Fatalf("summary %+v, calls %d, opens %d\n%s", sum2, w.tr.calls-calls, w.opens-opens, log2)
	}
	if readLive(t, w, "es-419")[xID] != "Salir" || readLive(t, w, "pt-BR")[xID] != "Sair" {
		t.Fatal("the new fixed texts are not live")
	}
	if s, _ := w.run(t, false); !s.Nothing {
		t.Fatal("the fixed texts are live: the next round has nothing to do")
	}

	// X66 review: a fixed text taken out of fixed.json is translated again (the person's text never stays by itself)
	write(t, filepath.Join(conf, "fixed.json"), []byte(`{"note":"","texts":{"es-419":{"`+xID+`":{"en":"Log out","text":"Salir","by":"T9","date":"2026-11-02"}},`+
		`"pt-BR":{"`+yID+`":{"en":"Collected","text":"Coletado","by":"T9","date":"2026-11-02"}}}}`))
	w.commit(t, "6666666666666666666666666666666666666666")
	w.run(t, false)
	if readLive(t, w, "pt-BR")[yID] != "Coletado" {
		t.Fatal("the pt-BR fixed text is not live")
	}
	write(t, filepath.Join(conf, "fixed.json"), []byte(`{"note":"","texts":{"es-419":{"`+xID+`":{"en":"Log out","text":"Salir","by":"T9","date":"2026-11-02"}}}}`))
	w.commit(t, "7777777777777777777777777777777777777777")
	calls = w.tr.calls
	if s, log := w.run(t, false); s.Nothing || w.tr.calls == calls {
		t.Fatalf("a removed fixed text must be translated again: %+v\n%s", s, log)
	}
	if got := readLive(t, w, "pt-BR")[yID]; got != "F:Collected" {
		t.Fatalf("pt-BR %s = %q, want the model's text again", yID, got)
	}
	var pt rung.Result
	if err := i18n.ReadJSON(filepath.Join(w.state, "live", "pt-BR.json"), &pt); err != nil {
		t.Fatal(err)
	}
	for _, c := range pt.Messages {
		if c.Fixed {
			t.Fatalf("pt-BR has no fixed text now: %+v", c)
		}
	}
	if readLive(t, w, "es-419")[xID] != "Salir" {
		t.Fatal("the es-419 fixed text stays")
	}

	// the English changes: the fixed text is old, so the round translates the text again
	words := filepath.Join(w.repo, "mobile", "src", "words.ts")
	b, err := os.ReadFile(words)
	if err != nil {
		t.Fatal(err)
	}
	write(t, words, []byte(strings.Replace(string(b), `logout: "Log out"`, `logout: "Sign out"`, 1)))
	w.commit(t, "5555555555555555555555555555555555555555")
	sum3, log3 := w.run(t, false)
	if !strings.Contains(log3, `es-419 `+xID+`: the English changed (fixed for "Log out", the app has "Sign out")`) {
		t.Fatalf("log:\n%s", log3)
	}
	if err := i18n.ReadJSON(filepath.Join(w.state, "rounds", sum3.Tag+".json"), &plan); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range plan.Langs["es-419"].Work {
		found = found || x.ID == xID
	}
	if !found {
		t.Fatal("an old fixed text is translated again")
	}
}

// X66 review: the fix round never sends a fixed text to the model, also when a flagged text reaches it through the graph
// (a hand-made pair Log out / Give: Log out fails every judge, so the fix round spreads to Give).
func TestFixedTextNotInTheFixRound(t *testing.T) {
	w := newWorld(t)
	conf := filepath.Join(w.repo, "i18n")
	write(t, filepath.Join(conf, "concepts.json"), []byte(`{"note":"","concepts":{"listing":{"about":"a food listing","terms":["listing","listings"]}},"opens":[],"pairs":[["`+xID+`","ROLE.give"]]}`))
	write(t, filepath.Join(conf, "fixed.json"), []byte(`{"note":"","texts":{"es-419":{"ROLE.give":{"en":"Give","text":"Dar","by":"T9","date":"2026-11-02"}},`+
		`"pt-BR":{"ROLE.give":{"en":"Give","text":"Doar","by":"T9","date":"2026-11-02"}}}}`))
	w.run(t, false)
	if w.tr.texts["Give"] != 0 {
		t.Fatalf("the model got the fixed text %d times", w.tr.texts["Give"])
	}
	if w.tr.texts["Log out"] < 2 {
		t.Fatalf("the flagged text was not in the fix round: %v", w.tr.texts)
	}
	if readLive(t, w, "es-419")["ROLE.give"] != "Dar" || readLive(t, w, "pt-BR")["ROLE.give"] != "Doar" {
		t.Fatal("the fixed texts are not live")
	}
}

