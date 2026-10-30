package i18n

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var appLists = &Lists{Text: []Entry{{File: "words.ts", Text: "listing", Why: "plural"}, {File: "words.ts", Text: "listings", Why: "plural"}}}

func appFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	filepath.Walk("testdata/app", func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel("testdata/app", p)
			files[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	return files
}

// Screens: a words key, a function with an object return type, a words
// object passed as an argument, a component; another screen's component is
// not part of this screen.
func TestScreens(t *testing.T) {
	files := appFiles(t)
	msgs, _, err := All(files, appLists)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := Screens(files, msgs)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sc["Role"], " "); got != "ROLE.give ROLE.logout ROLE.receive words.roleCard" {
		t.Errorf("Role: %s", got)
	}
	got := strings.Join(sc["Give"], " ")
	for _, want := range []string{"RECEIVE.collected", "RECEIVE.expired", "ui.Shutter", "words.postTitle.post", "words.postTitle.listing"} {
		if !strings.Contains(got, want) {
			t.Errorf("Give has no %s: %s", want, got)
		}
	}
	if strings.Contains(got, "ROLE.logout") || strings.Contains(got, "RECEIVE.title") {
		t.Errorf("Give has texts of another screen or an unused key: %s", got)
	}
}

// part_of from the joined placeholder; uses as whole words only.
func TestPartOfUses(t *testing.T) {
	files := appFiles(t)
	msgs, _, _ := All(files, appLists)
	var parts []string
	for _, e := range partOf(msgs) {
		parts = append(parts, e.From+">"+e.To)
	}
	if got := strings.Join(parts, " "); got != "words.postTitle.listing>words.postTitle.post words.postTitle.listings>words.postTitle.post" {
		t.Errorf("part_of: %s", got)
	}
	cs := map[string]Concept{"listing": {Terms: []string{"listing", "listed"}}, "account": {Terms: []string{"log out"}}}
	for text, want := range map[string]string{"Post {n} listings": "", "Already listed": "listing", "Unlisted": "", "Log out": "account", "Log in": "", "{listing}": ""} {
		if got := strings.Join(Uses(text, cs), ","); got != want {
			t.Errorf("Uses(%q) = %q, want %q", text, got, want)
		}
	}
}

// Walk: direct edges first, one hop through a screen or a concept, most
// connected first, the start texts not listed.
func TestWalk(t *testing.T) {
	g := GraphFile{
		Nodes: []Node{{ID: "a", Degree: 3}, {ID: "b", Degree: 1}, {ID: "c", Degree: 2}, {ID: "d", Degree: 5}, {ID: "screen:S"}, {ID: "concept:k"}},
		Edges: []Edge{{"a", "screen:S", "on_screen"}, {"b", "screen:S", "on_screen"}, {"a", "c", "pair"}, {"c", "screen:S", "on_screen"},
			{"a", "concept:k", "uses"}, {"d", "concept:k", "uses"}},
	}
	r := Walk(g, []string{"a"})
	var got []string
	for _, x := range r {
		got = append(got, x.ID+"("+x.Via+")")
	}
	if s := strings.Join(got, " "); s != "d(a uses k uses) c(a pair) b(a on_screen S on_screen)" {
		t.Fatalf("walk: %s", s)
	}
}

// Spread: pairs, joined parts and the same concept; not texts only on the same screen.
func TestSpread(t *testing.T) {
	g := GraphFile{
		Nodes: []Node{{ID: "a", Degree: 3}, {ID: "b", Degree: 1}, {ID: "c", Degree: 2}, {ID: "d", Degree: 5}, {ID: "e"}, {ID: "screen:S"}, {ID: "concept:k"}},
		Edges: []Edge{{"a", "screen:S", "on_screen"}, {"b", "screen:S", "on_screen"}, {"a", "c", "pair"}, {"c", "screen:S", "on_screen"},
			{"a", "concept:k", "uses"}, {"d", "concept:k", "uses"}, {"e", "a", "part_of"}},
	}
	var got []string
	for _, x := range Spread(g, "a") {
		got = append(got, x.ID)
	}
	if s := strings.Join(got, " "); s != "d c e" {
		t.Fatalf("spread: %s", s)
	}
}

// Stale: a text with no context, a changed English text, a context of a gone text.
func TestStale(t *testing.T) {
	msgs := []Message{{ID: "a", Text: "Close"}, {ID: "b", Text: "Retake"}, {ID: "c", Text: "New"}}
	cf := ContextFile{Contexts: map[string]Context{"a": {EnHash: EnHash("Close")}, "b": {EnHash: EnHash("Take again")}, "z": {}}}
	m, ch, g := Stale(msgs, cf)
	if strings.Join(m, ",") != "c" || strings.Join(ch, ",") != "b" || strings.Join(g, ",") != "z" {
		t.Fatalf("missing %v changed %v gone %v", m, ch, g)
	}
}

// ImpactOf: added, removed and changed texts, and the texts they reach.
func TestImpact(t *testing.T) {
	old := []Message{{ID: "a", Text: "Log out"}, {ID: "b", Text: "Continue"}, {ID: "x", Text: "Old"}}
	cur := []Message{{ID: "a", Text: "Sign out"}, {ID: "b", Text: "Continue"}, {ID: "n", Text: "New"}}
	g := GraphFile{Nodes: []Node{{ID: "a"}, {ID: "b"}, {ID: "x"}, {ID: "n"}}, Edges: []Edge{{"a", "b", "pair"}, {"x", "b", "pair"}}}
	im := ImpactOf(old, cur, g, g)
	if strings.Join(im.Added, ",") != "n" || strings.Join(im.Removed, ",") != "x" || strings.Join(im.Changed, ",") != "a" ||
		len(im.Reached) != 1 || im.Reached[0].ID != "b" {
		t.Fatalf("%+v", im)
	}
}

// A literal in a chain that starts with "(" is kept, and every literal is
// accounted for (L4g found two lost texts this way).
func TestEveryLiteral(t *testing.T) {
	r, err := Extract("a.ts", "export function f(x: string) { return (x ? x : \"Request failed\") + \" — your photo is kept.\"; }", &Lists{})
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range r.Messages {
		texts = append(texts, m.Text)
	}
	if got := strings.Join(texts, "|"); got != "{p1} — your photo is kept.|Request failed" {
		t.Fatalf("messages: %s", got)
	}
}

// context.json: a context for every text, written for the current English
// (en_hash), with kind, screen, does, a valid status, and a value for each placeholder.
func TestContexts(t *testing.T) {
	var en EnFile
	var cf ContextFile
	if err := ReadJSON("en.json", &en); err != nil {
		t.Fatal(err)
	}
	if err := ReadJSON("context.json", &cf); err != nil {
		t.Fatal(err)
	}
	missing, changed, gone := Stale(en.Messages, cf)
	if len(missing)+len(changed)+len(gone) > 0 {
		t.Fatalf("stale contexts (go run ./cmd/graph): missing %v, changed English %v, gone %v", missing, changed, gone)
	}
	for _, m := range en.Messages {
		c := cf.Contexts[m.ID]
		if c.Kind == "" || c.Screen == "" || c.Does == "" || (c.Status != "draft" && c.Status != "approved") {
			t.Errorf("%s: incomplete context %+v", m.ID, c)
		}
		for _, p := range m.Params {
			if c.Values[p] == "" {
				t.Errorf("%s: no value for {%s}", m.ID, p)
			}
		}
		for v := range c.Values {
			if !strings.Contains(m.Text, "{"+v+"}") {
				t.Errorf("%s: value %s is not a placeholder", m.ID, v)
			}
		}
	}
}

// The real graph: the logout pair, the joined Post line, every concept used.
func TestGraphFile(t *testing.T) {
	var g GraphFile
	if err := ReadJSON("graph.json", &g); err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, e := range g.Edges {
		has[e.From+">"+e.To+":"+e.Kind] = true
	}
	for _, want := range []string{"ROLE.logout>SIGN_IN.go:pair", "logic.postTitle.listing>logic.postTitle.post:part_of",
		"ROLE.logout>screen:Role:on_screen", "logic.postTitle.post>concept:post:uses"} {
		if !has[want] {
			t.Errorf("graph.json has no edge %s", want)
		}
	}
}
