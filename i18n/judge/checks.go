// Package judge checks translations as groups (L4j): checks with no model
// (pairs, inconsistency in target, inconsistency in source, untranslated), and
// judges that read a whole concept or a whole screen of one language.
package judge

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"v3rde/i18n"
)

// Issue is one finding about one or more texts.
type Issue struct {
	Check  string   `json:"check"`  // pair_same, target_inconsistent, source_same, untranslated, or a judge name
	IDs    []string `json:"ids"`    // the texts it is about
	Detail string   `json:"detail"` // what is wrong, in ASD-STE100
	Group  string   `json:"group,omitempty"`
	Model  string   `json:"model,omitempty"`
}

// Set is one language's translations with what the graph knows about them.
type Set struct {
	Lang   i18n.Language
	Keep   []string
	Msgs   []i18n.Message
	Graph  i18n.GraphFile
	Ctx    i18n.ContextFile
	Trans  map[string]string // id -> translation
	Shared map[string]string // id -> the id whose translation it reuses (same text and context)
}

// norm: lower case, letters and digits only, for comparing two texts.
func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Checks runs the checks that need no model. Shared texts (same English and
// context, one translation) are not compared with each other.
func Checks(s Set) []Issue {
	en := map[string]string{}
	plural := map[string]bool{} // a part that plural() joins: one form may serve both
	for _, m := range s.Msgs {
		en[m.ID] = m.Text
		plural[m.ID] = m.Arg == "plural"
	}
	k := i18n.NewKnowledge(s.Msgs, s.Graph, s.Ctx)
	var out []Issue
	pairGroups := map[string]bool{} // a pair_same is not reported again as source_same
	// 1. a pair or an opposite must not get one translation
	for _, e := range s.Graph.Edges {
		if e.Kind != "pair" {
			continue
		}
		a, b := s.Trans[e.From], s.Trans[e.To]
		if a != "" && norm(a) == norm(b) && norm(en[e.From]) != norm(en[e.To]) {
			ids := []string{e.From, e.To}
			sort.Strings(ids)
			pairGroups[strings.Join(ids, ",")] = true
			out = append(out, Issue{Check: "pair_same", IDs: []string{e.From, e.To},
				Detail: "The two texts are a pair or opposites in English (" + q(en[e.From]) + ", " + q(en[e.To]) + "), but both translations are " + q(a) + "."})
		}
	}
	// 2. inconsistency in target: one English text and one context, two translations
	byKey := map[string][]string{}
	for _, m := range s.Msgs {
		if s.Trans[m.ID] != "" {
			byKey[k.Key(m.ID)] = append(byKey[k.Key(m.ID)], m.ID)
		}
	}
	for _, ids := range sortedGroups(byKey) {
		seen := map[string]bool{}
		for _, id := range ids {
			seen[norm(s.Trans[id])] = true
		}
		if len(seen) > 1 {
			out = append(out, Issue{Check: "target_inconsistent", IDs: ids,
				Detail: "The same English text " + q(en[ids[0]]) + " with the same context has different translations."})
		}
	}
	// 3. inconsistency in source: on one screen, different English, one translation
	screens := map[string][]string{}
	for _, e := range s.Graph.Edges {
		if e.Kind == "on_screen" && s.Trans[e.From] != "" {
			screens[e.To] = append(screens[e.To], e.From)
		}
	}
	keep := map[string]bool{}
	for _, w := range s.Keep {
		keep[norm(w)] = true
	}
	reported := map[string]bool{}
	for _, scr := range sortedKeys(screens) {
		by := map[string][]string{}
		for _, id := range screens[scr] {
			t := norm(s.Trans[id])
			if t == "" || keep[t] {
				continue
			}
			by[t] = append(by[t], id)
		}
		for _, ids := range sortedGroups(by) {
			eng := map[string]bool{}
			allPlural := true
			for _, id := range ids {
				eng[norm(en[id])] = true
				allPlural = allPlural && plural[id]
			}
			if allPlural {
				continue // "item found" / "items found": many languages have one form
			}
			key := strings.Join(ids, ",")
			if len(eng) > 1 && !reported[key] && !pairGroups[key] {
				reported[key] = true
				var es []string
				for _, id := range ids {
					es = append(es, q(en[id]))
				}
				out = append(out, Issue{Check: "source_same", IDs: ids, Group: strings.TrimPrefix(scr, "screen:"),
					Detail: "Different English texts on one screen (" + strings.Join(es, ", ") + ") have the same translation " + q(s.Trans[ids[0]]) + "."})
			}
		}
	}
	// 4. untranslated: a language in another script that kept the English text
	if s.Lang.Script != "Latn" {
		for _, m := range s.Msgs {
			t := s.Trans[m.ID]
			if t != "" && norm(t) == norm(m.Text) && !keep[norm(m.Text)] && hasLetters(reEmail.ReplaceAllString(m.Text, "")) {
				out = append(out, Issue{Check: "untranslated", IDs: []string{m.ID},
					Detail: "The translation is the English text " + q(m.Text) + "."})
			}
		}
	}
	return out
}

func hasLetters(s string) bool {
	n := 0
	for _, r := range i18n.StripParams(s) {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n >= 2
}

var reEmail = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.]+`)

func q(s string) string { return "\"" + s + "\"" }

func sortedKeys[V any](m map[string]V) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// sortedGroups: the groups with more than one id, in a fixed order.
func sortedGroups(m map[string][]string) [][]string {
	var out [][]string
	for _, k := range sortedKeys(m) {
		if len(m[k]) > 1 {
			ids := append([]string(nil), m[k]...)
			sort.Strings(ids)
			out = append(out, ids)
		}
	}
	return out
}

// Flagged: for each id, the details of the issues about it.
func Flagged(issues []Issue) map[string][]string {
	f := map[string][]string{}
	for _, is := range issues {
		for _, id := range is.IDs {
			f[id] = append(f[id], is.Check+": "+is.Detail)
		}
	}
	return f
}
