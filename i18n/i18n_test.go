package i18n

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func strs(toks []Token) []string {
	var out []string
	for _, t := range toks {
		if t.Kind == Str {
			out = append(out, t.Text)
			for _, p := range t.Parts {
				out = append(out, strs(p)...)
			}
		}
	}
	return out
}

// The lexer skips comments and regex literals, keeps divisions, decodes
// escapes and reads nested templates and JSX closing tags.
func TestLexer(t *testing.T) {
	toks, err := Lex("lex.tsx", read(t, "testdata/lex.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(strs(toks), "|")
	want := "./x|say “hi”\n|it's|a {p1} z|in {c}|no|Open "
	if got != want {
		t.Fatalf("strings:\n got %q\nwant %q", got, want)
	}
	regex := 0
	for _, tk := range toks {
		if tk.Kind == Regex {
			regex++
		}
	}
	if regex != 1 {
		t.Fatalf("regex literals: %d, want 1 (a division is not a regex)", regex)
	}
}

// JSX text fails: the app keeps its text in TypeScript strings.
func TestJSXTextFails(t *testing.T) {
	_, err := Lex("a.tsx", "const a = <Text>Send it now</Text>;\n")
	if err == nil || !strings.Contains(err.Error(), "JSX text") {
		t.Fatalf("want a JSX text error, got %v", err)
	}
	if _, err := Lex("a.tsx", "const a = <Text>{WORDS.send}</Text>;\n"); err != nil {
		t.Fatalf("a JSX expression is not text: %v", err)
	}
}

// Messages: words objects, concatenations with placeholders (also a name
// "from"), ternaries, plural arguments, typed consts, and the code rules.
func TestMessages(t *testing.T) {
	r, err := Extract("words.tsx", read(t, "testdata/words.ts"), &Lists{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Message{}
	for _, m := range r.Messages {
		got[m.ID] = m
	}
	want := map[string]string{
		"WORDS.title":              "Your listing",
		"WORDS.hint":               "you@email.com",
		"WORDS.heard":              "You said: “{t}”",
		"WORDS.rename":             "Item {n}: {from} to {to}",
		"WORDS.nested.deep":        "Deep text",
		"FIXED.401":                "Please log in again",
		"words.servings.1_serving": "1 serving",
		"words.servings.servings":  "{n} servings",
		"words.grab":               "Hi! I grabbed \"{title}\" for {servings}. I am {email}.",
		"words.title.item_found":   "item found",
		"words.title.items_found":  "items found",
		"words.code":               "The scanner is busy",
		"words.Pill":               "Take photo",
	}
	for id, text := range want {
		if got[id].Text != text {
			t.Errorf("%s: got %q, want %q", id, got[id].Text, text)
		}
	}
	if len(got) != len(want) {
		var ids []string
		for id := range got {
			ids = append(ids, id+"="+got[id].Text)
		}
		t.Errorf("got %d messages, want %d: %v", len(got), len(want), ids)
	}
	if a := got["words.title.item_found"].Arg; a != "plural" {
		t.Errorf("plural argument: Arg %q", a)
	}
	if p := strings.Join(got["WORDS.rename"].Params, ","); p != "n,from,to" {
		t.Errorf("params %q", p)
	}
	rules := map[string]string{}
	for _, s := range r.Skipped {
		rules[s.Text] = s.Rule
	}
	for text, rule := range map[string]string{"busy": "compared", "/api/listings": "code word", "POST": "code prop method",
		"application/json": "code word", "pill": "code prop testID", "center": "code prop alignItems", "#FFFFFF": "color",
		"{h}:{two}:{two_2}": "no letters"} {
		if rules[text] != rule {
			t.Errorf("skipped %q: rule %q, want %q", text, rules[text], rule)
		}
	}
}

// A rules.json entry that matches no literal is an error (no stale entries).
func TestStaleRule(t *testing.T) {
	files := map[string]string{"a.ts": `export const A = { x: "Hello" };`}
	if _, _, err := All(files, &Lists{NotText: []Entry{{File: "a.ts", Text: "Gone", Why: "old"}}}); err == nil {
		t.Fatal("a stale rule must fail")
	}
	if _, _, err := All(files, &Lists{NotText: []Entry{{File: "b.ts", Why: "old"}}}); err == nil {
		t.Fatal("a rule for a missing file must fail")
	}
}

// en.json and skipped.json are what the extractor makes from the ui commit
// in source.json: every literal of the app is in one of them.
func TestFilesUpToDate(t *testing.T) {
	en, sk, gr, n, err := Build("..", ".")
	if err != nil {
		t.Fatalf("%v (the apply script fetches the ui branch first)", err)
	}
	if !bytes.Equal(en, []byte(read(t, "en.json"))) {
		t.Error("en.json is not up to date: go run ./cmd/extract")
	}
	if !bytes.Equal(sk, []byte(read(t, "skipped.json"))) {
		t.Error("skipped.json is not up to date: go run ./cmd/extract")
	}
	if !bytes.Equal(gr, []byte(read(t, "graph.json"))) {
		t.Error("graph.json is not up to date: go run ./cmd/extract")
	}
	if n < 150 {
		t.Errorf("only %d messages", n)
	}
}

var reTag = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z][a-z]{3})?(-([A-Z]{2}|[0-9]{3}))?$`)

// languages.json: English source, Spanish (Latin America) first, the agreed
// set, and each native name written in its own script.
func TestLanguages(t *testing.T) {
	var l Languages
	if err := ReadJSON("languages.json", &l); err != nil {
		t.Fatal(err)
	}
	var codes []string
	scripts := map[string]*unicode.RangeTable{"Latn": unicode.Latin, "Hans": unicode.Han, "Deva": unicode.Devanagari, "Arab": unicode.Arabic}
	for _, g := range l.Languages {
		codes = append(codes, g.Code+":"+g.Role)
		if !reTag.MatchString(g.Code) {
			t.Errorf("%s is not a BCP 47 tag of the form we use", g.Code)
		}
		tab := scripts[g.Script]
		if tab == nil {
			t.Errorf("%s: script %s not known", g.Code, g.Script)
			continue
		}
		for _, r := range g.Native {
			if unicode.IsLetter(r) && !unicode.Is(tab, r) {
				t.Errorf("%s: %q in %q is not %s", g.Code, r, g.Native, g.Script)
			}
		}
		if (g.Dir == "rtl") != (g.Script == "Arab") || (g.Dir != "rtl" && g.Dir != "ltr") {
			t.Errorf("%s: dir %s", g.Code, g.Dir)
		}
	}
	if got := strings.Join(codes, " "); got != "en:source es-419:first pt-BR:target zh-Hans:target hi:target ar:target" || l.Source != "en" {
		t.Errorf("languages: %s", got)
	}
	en := read(t, "en.json")
	for _, k := range l.Keep {
		if !strings.Contains(en, k) {
			t.Errorf("keep %q is not in en.json", k)
		}
	}
}

// en.json: unique ids, params in text order, a file:line for each message.
func TestEnFile(t *testing.T) {
	var f EnFile
	if err := ReadJSON("en.json", &f); err != nil {
		t.Fatal(err)
	}
	if f.Language != "en" || f.Source.Branch != "ui" || len(f.Source.Commit) != 40 {
		t.Fatalf("header: %+v", f.Source)
	}
	ids := map[string]bool{}
	for _, m := range f.Messages {
		if ids[m.ID] || m.ID == "" || strings.TrimSpace(m.Text) == "" || !strings.Contains(m.Where, ":") {
			t.Errorf("bad message %+v", m)
		}
		ids[m.ID] = true
		var ps []string
		seen := map[string]bool{}
		for _, x := range reParam.FindAllStringSubmatch(m.Text, -1) {
			if !seen[x[1]] {
				seen[x[1]] = true
				ps = append(ps, x[1])
			}
		}
		if strings.Join(ps, ",") != strings.Join(m.Params, ",") {
			t.Errorf("%s: params %v, text %q", m.ID, m.Params, m.Text)
		}
	}
}
