package round

// X66 (T9 Nov 2, test finding 22): a translation that a person gives. The
// round makes every translation with the model and sends hard texts to the
// review list; it had no place for a person's text. i18n/fixed.json keeps
// them by hand: language -> text id -> the English it translates and the
// text. A round uses a fixed text as it is, never translates that text
// again, and takes it off the review list. When the English of the app
// changes, the fixed text is old: the round does not use it (a warning) and
// translates the text again, until a person writes it again.

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"unicode/utf8"

	"v3rde/i18n"
	"v3rde/i18n/rung"
)

// FixedFile is i18n/fixed.json.
type FixedFile struct {
	Note  string                          `json:"note"`
	Texts map[string]map[string]FixedText `json:"texts"` // language code -> text id -> the fixed text
}

// FixedText is one translation a person gives.
type FixedText struct {
	English string `json:"en"`   // the English text it translates (as the app has it)
	Text    string `json:"text"` // the translation, with the same {name} values
	By      string `json:"by"`   // who gave it
	Date    string `json:"date"` // when (YYYY-MM-DD)
}

// FixedMaxLen is the longest fixed text (bytes), as a long app text.
const FixedMaxLen = 600

var placeholderRe = regexp.MustCompile(`\{[A-Za-z_][A-Za-z0-9_]*\}`)

func placeholders(s string) []string {
	p := placeholderRe.FindAllString(s, -1)
	sort.Strings(p)
	return p
}

// ReadFixed reads path; a file that is not there is no fixed text.
// X66 review: unknown keys are left out (not an error), so a note in an entry
// does not stop the round; a file that is not JSON is an error the round logs.
func ReadFixed(path string) (FixedFile, error) {
	var f FixedFile
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return FixedFile{}, err
	}
	return f, nil
}

// ActiveFixed is the fixed texts a round uses: language -> id -> text. A
// fixed text is used only for a target language of languages.json, a text id
// of the app now, the English of the app now, a text that is not empty and
// not too long, and the same {name} values as the English. Every other entry
// is a warning (it is not used).
func ActiveFixed(f FixedFile, cur []i18n.Message, langs i18n.Languages) (map[string]map[string]string, []string) {
	en := map[string]string{}
	for _, m := range cur {
		en[m.ID] = m.Text
	}
	target := map[string]bool{}
	for _, l := range langs.Languages {
		if l.Role != "source" {
			target[l.Code] = true
		}
	}
	active := map[string]map[string]string{}
	var warn []string
	codes := make([]string, 0, len(f.Texts))
	for c := range f.Texts {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, code := range codes {
		ids := make([]string, 0, len(f.Texts[code]))
		for id := range f.Texts[code] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			x := f.Texts[code][id]
			now, known := en[id]
			switch {
			case !target[code]:
				warn = append(warn, fmt.Sprintf("%s %s: %s is not a target language", code, id, code))
			case !known:
				warn = append(warn, fmt.Sprintf("%s %s: no such text in the app now", code, id))
			case x.English != now:
				warn = append(warn, fmt.Sprintf("%s %s: the English changed (fixed for %q, the app has %q): translated again", code, id, x.English, now))
			case x.Text == "" || len(x.Text) > FixedMaxLen || !utf8.ValidString(x.Text):
				warn = append(warn, fmt.Sprintf("%s %s: the fixed text is empty, too long or not UTF-8", code, id))
			case !slices.Equal(placeholders(x.Text), placeholders(now)):
				warn = append(warn, fmt.Sprintf("%s %s: the {name} values %v are not those of the English %v", code, id, placeholders(x.Text), placeholders(now)))
			default:
				if active[code] == nil {
					active[code] = map[string]string{}
				}
				active[code][id] = x.Text
			}
		}
	}
	return active, warn
}

// countFixed is the number of fixed texts in use.
func countFixed(f map[string]map[string]string) int {
	n := 0
	for _, m := range f {
		n += len(m)
	}
	return n
}

// withoutFixed is work with no text that has a fixed text.
func withoutFixed(work []Why, fix map[string]string) []Why {
	if len(fix) == 0 {
		return work
	}
	out := work[:0:0]
	for _, w := range work {
		if _, ok := fix[w.ID]; !ok {
			out = append(out, w)
		}
	}
	return out
}

// fixedPending counts the fixed texts that the live file does not have yet.
func fixedPending(live rung.Result, fix map[string]string) int {
	have := map[string]string{}
	for _, c := range live.Messages {
		have[c.ID] = c.Text
	}
	fixed := map[string]bool{}
	for _, c := range live.Messages {
		fixed[c.ID] = c.Fixed
	}
	n := 0
	for id, t := range fix {
		if have[id] != t || !fixed[id] { // X66 review: also a model text that happens to be the same words
			n++
		}
	}
	return n
}

// unfixed lists the texts of the live file that are marked fixed but have no
// fixed text in use now (removed from fixed.json, or old): the round
// translates them again (X66 review: a person's text never stays by itself).
func unfixed(live rung.Result, fix map[string]string) []string {
	var ids []string
	for _, c := range live.Messages {
		if _, ok := fix[c.ID]; c.Fixed && !ok {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// ApplyFixed puts the fixed texts of one language into its live file (in the
// order of cur), takes them off Missing and the review list, and keeps their
// English in the source file (so the next round sees them as up to date).
// X66 review: a text that is not fixed now loses the mark (a stale fixed
// text kept after a failed try), and a text that reused a fixed id's
// translation (SameAs) is its own text again, so the checks see it alone.
func ApplyFixed(lang string, res rung.Result, src map[string]string, review []ReviewItem, cur []i18n.Message, fix map[string]string) (rung.Result, []ReviewItem) {
	have := map[string]rung.Chosen{}
	for _, c := range res.Messages {
		have[c.ID] = c
	}
	res.Messages, res.Missing = nil, []string{}
	for _, m := range cur {
		c, ok := have[m.ID]
		if t, fixed := fix[m.ID]; fixed {
			c, ok = rung.Chosen{ID: m.ID, Text: t, Fixed: true}, true
			if src != nil {
				src[m.ID] = m.Text
			}
		} else if ok {
			c.Fixed = false
			if _, toFixed := fix[c.SameAs]; toFixed {
				c.SameAs = ""
			}
		}
		if !ok {
			res.Missing = append(res.Missing, m.ID)
			continue
		}
		res.Messages = append(res.Messages, c)
	}
	out := review[:0:0]
	for _, r := range review {
		if _, fixed := fix[r.ID]; r.Lang == lang && fixed {
			continue
		}
		out = append(out, r)
	}
	return res, out
}
