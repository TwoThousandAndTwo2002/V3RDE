// Package mask hides the parts of a message that a translation must not change
// (the {name} placeholders and the names in languages.json "keep") behind inline
// tags <x1/>, <x2/>, ... before the text goes to the translator, and puts them
// back after. Restore fails when a tag is lost, doubled or unknown (L4a).
package mask

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Masked is a message with its protected parts replaced by tags.
type Masked struct {
	Text string   // the text with <x1/> ... <xN/>
	Orig []string // Orig[k-1] is the text behind <xk/>
}

var (
	reParam = regexp.MustCompile(`\{[A-Za-z_$][A-Za-z0-9_$]*\}`)
	// a tag as the translator may write it back: <x1/>, <x1 />, < x1/ >
	reTag = regexp.MustCompile(`<\s*x(\d+)\s*/\s*>`)
)

type span struct{ a, b int }

// Mask replaces each {name} and each keep name (as a whole word, case as written)
// with a tag, numbered in the order they come in the text.
func Mask(text string, keep []string) (Masked, error) {
	if reTag.MatchString(text) {
		return Masked{}, fmt.Errorf("the text already has a tag like <x1/>: %q", text)
	}
	var spans []span
	for _, m := range reParam.FindAllStringIndex(text, -1) {
		spans = append(spans, span{m[0], m[1]})
	}
	for _, k := range keep {
		if k == "" {
			continue
		}
		for from := 0; ; {
			i := strings.Index(text[from:], k)
			if i < 0 {
				break
			}
			a, b := from+i, from+i+len(k)
			if wordEdge(text, a, b) && !inside(spans, a, b) {
				spans = append(spans, span{a, b})
			}
			from = b
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].a < spans[j].a })
	var out strings.Builder
	m := Masked{}
	last := 0
	for _, s := range spans {
		out.WriteString(text[last:s.a])
		m.Orig = append(m.Orig, text[s.a:s.b])
		out.WriteString("<x" + strconv.Itoa(len(m.Orig)) + "/>")
		last = s.b
	}
	out.WriteString(text[last:])
	m.Text = out.String()
	return m, nil
}

// wordEdge: no letter or digit just before a or just after b.
func wordEdge(s string, a, b int) bool {
	if a > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:a])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	if b < len(s) {
		r, _ := utf8.DecodeRuneInString(s[b:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func inside(spans []span, a, b int) bool {
	for _, s := range spans {
		if a < s.b && b > s.a {
			return true
		}
	}
	return false
}

// Restore puts the protected parts back into a translation. Each tag must be
// there exactly once, and no other tag and no bare {name} may be there.
func (m Masked) Restore(translated string) (string, error) {
	seen := make([]int, len(m.Orig))
	for _, t := range reTag.FindAllStringSubmatch(translated, -1) {
		k, _ := strconv.Atoi(t[1])
		if k < 1 || k > len(m.Orig) {
			return "", fmt.Errorf("unknown tag <x%d/> (the source has %d)", k, len(m.Orig))
		}
		seen[k-1]++
	}
	var bad []string
	for k, n := range seen {
		if n != 1 {
			bad = append(bad, fmt.Sprintf("<x%d/> %d times", k+1, n))
		}
	}
	if len(bad) > 0 {
		return "", fmt.Errorf("tags not exactly once: %s", strings.Join(bad, ", "))
	}
	if p := reParam.FindString(translated); p != "" {
		return "", fmt.Errorf("a bare placeholder %s is in the translation", p)
	}
	return reTag.ReplaceAllStringFunc(translated, func(t string) string {
		k, _ := strconv.Atoi(reTag.FindStringSubmatch(t)[1])
		return m.Orig[k-1]
	}), nil
}
