package i18n

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// X49x: the app finds a translation by the id of its text, "<TABLE>.<key>" (mobile/src/lang.ts). So every text
// that the extractor finds in the app must be in a word table that lang.ts registers (a <FILE>_TABLES list), or
// the text would stay English in every language.
func TestEveryAppTextIsInARegisteredTable(t *testing.T) {
	files, err := ReadTree("..", "mobile/src")
	if err != nil {
		t.Fatal(err)
	}
	var lists Lists
	if err := ReadJSON("rules.json", &lists); err != nil {
		t.Fatal(err)
	}
	msgs, _, err := All(files, &lists)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files["lang.ts"], "...WORD_TABLES, ...SIGNIN_TABLES, ...CAPTURE_TABLES, ...LOGIC_TABLES, ...RESCUE_TABLES") {
		t.Fatal("lang.ts TABLES does not join the five table lists")
	}
	list := regexp.MustCompile(`export const (?:WORD|SIGNIN|CAPTURE|LOGIC|RESCUE)_TABLES = \{([^}]*)\}`)
	tables := map[string]bool{}
	for name, src := range files {
		for _, m := range list.FindAllStringSubmatch(src, -1) {
			for _, n := range strings.Split(m[1], ",") {
				if n = strings.TrimSpace(n); n != "" {
					tables[n] = true
				}
			}
			_ = name
		}
	}
	if len(tables) < 20 {
		t.Fatalf("only %d tables found: %v", len(tables), tables)
	}
	for _, m := range msgs {
		if head := strings.SplitN(m.ID, ".", 2)[0]; !tables[head] {
			t.Errorf("%s (%q, %s) is not in a registered word table", m.ID, m.Text, m.Where)
		}
	}
}

// X49x review: mobile/tests/app_texts.json is the id -> English text map of the app as the extractor sees it; the
// app test (mobile/tests/lang.test.mts) gives every id a translation and checks that each string, list item and
// word function of the tables shows it. This test keeps the file the same as the tree (V3RDE_UPDATE=1 writes it).
func TestAppTextsFile(t *testing.T) {
	files, err := ReadTree("..", "mobile/src")
	if err != nil {
		t.Fatal(err)
	}
	var lists Lists
	if err := ReadJSON("rules.json", &lists); err != nil {
		t.Fatal(err)
	}
	msgs, _, err := All(files, &lists)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, x := range msgs {
		m[x.ID] = x.Text
	}
	b, _ := json.MarshalIndent(m, "", " ")
	b = append(b, '\n')
	const path = "../mobile/tests/app_texts.json"
	if os.Getenv("V3RDE_UPDATE") == "1" {
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(b) {
		t.Fatalf("%s is not the same as the app texts (V3RDE_UPDATE=1 go test -run TestAppTextsFile .): %v", path, err)
	}
}
