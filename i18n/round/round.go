// Package round runs one translation round after an app update (cmd/round): it
// reads the app text at the deployed commit, rebuilds the text graph, compares
// it with the last round, and translates, for each language, only the texts that
// were added or changed and the texts that the graph says they reach. The batch
// method with one fix round (L4k), MetricX-24 QE scores, the checks and both
// judges decide what goes live. A text that fails twice keeps its last good
// translation and goes to a review list for a person.
package round

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"v3rde/i18n"
	"v3rde/i18n/rung"
)

// Tag is the round tag: r<YYYYMMDDTHHMMSSZ> (UTC).
func Tag(t time.Time) string { return "r" + t.UTC().Format("20060102T150405Z") }

// Why says why one text of one language is in the round.
type Why struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`        // added, missing, changed or reached
	Via    string `json:"via,omitempty"` // reached: the start text and the edges
}

// WorkList returns the texts of one language to translate in this round, in the
// order of cur, and the ids that leave its live file.
//
// im is the change of the app text since the last round (i18n.ImpactOf). src
// maps each id of the language's live file to the English text its translation
// was made for (live/<lang>.source.json). An id of cur that src does not have
// is "added" when the app added it, else "missing" (an earlier failure, or a
// new language). An id whose English is not src's is "changed". The texts that
// the graph walk of the app change reaches (im.Reached) are "reached"; a
// missing text starts no walk, because its English did not change.
func WorkList(src map[string]string, im i18n.Impact, cur []i18n.Message) (work []Why, removed []string) {
	added := map[string]bool{}
	for _, id := range im.Added {
		added[id] = true
	}
	reached := map[string]i18n.Reached{}
	for _, r := range im.Reached {
		reached[r.ID] = r
	}
	inCur := map[string]bool{}
	for _, m := range cur {
		inCur[m.ID] = true
		old, ok := src[m.ID]
		switch {
		case !ok && added[m.ID]:
			work = append(work, Why{ID: m.ID, Reason: "added"})
		case !ok:
			work = append(work, Why{ID: m.ID, Reason: "missing"})
		case old != m.Text:
			work = append(work, Why{ID: m.ID, Reason: "changed"})
		default:
			if r, ok := reached[m.ID]; ok {
				work = append(work, Why{ID: m.ID, Reason: "reached", Via: r.Via})
			}
		}
	}
	for id := range src {
		if !inCur[id] {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	return work, removed
}

// Outcome is the end of one text in a round.
type Outcome struct {
	Pass     bool
	New      rung.Chosen  // Pass: the translation that goes live
	Fallback *rung.Chosen // !Pass: a translation of this round that passed before the fix round (nil: keep the live one)
	Reasons  []string     // !Pass: why it failed (the checks, the judges, MetricX)
	Tried    []string     // !Pass: the translations that failed
}

// ReviewItem is one text for a person (review/<tag>.json).
type ReviewItem struct {
	Lang    string   `json:"lang"`
	ID      string   `json:"id"`
	English string   `json:"english"`
	Kept    string   `json:"kept"`      // the translation that stays live ("" when none)
	From    string   `json:"kept_from"` // live (the last good translation), round (passed before the fix round) or none
	Tried   []string `json:"tried"`
	Reasons []string `json:"reasons"`
}

// MergeLive makes the new live file of one language. A text that passed takes
// its new translation. A text that failed keeps its fallback, else the live
// translation (its src stays the old English, so the next round tries it
// again), else it has none; it goes to the review list. A text that is not in
// cur leaves the file. The other texts stay as they are. The result has the
// order of cur; Missing lists the texts with no translation.
func MergeLive(lang string, live rung.Result, src map[string]string, cur []i18n.Message, out map[string]Outcome) (rung.Result, map[string]string, []ReviewItem) {
	have := map[string]rung.Chosen{}
	for _, c := range live.Messages {
		have[c.ID] = c
	}
	inCur := map[string]bool{}
	for _, m := range cur {
		inCur[m.ID] = true
	}
	res := live
	res.Lang, res.Messages, res.Missing = lang, nil, []string{}
	newSrc := map[string]string{}
	var review []ReviewItem
	for _, m := range cur {
		o, inRound := out[m.ID]
		c, ok := have[m.ID]
		switch {
		case inRound && o.Pass:
			c, ok = o.New, true
			newSrc[m.ID] = m.Text
		case inRound:
			item := ReviewItem{Lang: lang, ID: m.ID, English: m.Text, From: "none", Tried: o.Tried, Reasons: o.Reasons}
			if o.Fallback != nil {
				c, ok = *o.Fallback, true
				newSrc[m.ID] = m.Text
				item.From = "round"
			} else if ok {
				newSrc[m.ID] = src[m.ID]
				item.From = "live"
			}
			if ok {
				item.Kept = c.Text
			}
			if item.Tried == nil {
				item.Tried = []string{}
			}
			review = append(review, item)
		case ok:
			if s, known := src[m.ID]; known {
				newSrc[m.ID] = s
			} else {
				newSrc[m.ID] = m.Text
			}
		}
		if !ok {
			res.Missing = append(res.Missing, m.ID)
			continue
		}
		c.ID = m.ID
		if c.SameAs != "" && !inCur[c.SameAs] {
			c.SameAs = ""
		}
		res.Messages = append(res.Messages, c)
	}
	return res, newSrc, review
}

// Published is <lang>.json in -out: what the app reads.
type Published struct {
	Lang     string            `json:"lang"`
	Round    string            `json:"round"`
	Commit   string            `json:"commit"`
	Dir      string            `json:"dir"`
	Messages map[string]string `json:"messages"` // id -> text, with {name} values
}

// Index is index.json in -out.
type Index struct {
	Round     string      `json:"round"`
	Commit    string      `json:"commit"`
	Source    string      `json:"source"`
	Languages []IndexLang `json:"languages"`
}

// IndexLang is one language of index.json.
type IndexLang struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Native  string `json:"native"`
	Dir     string `json:"dir"`
	File    string `json:"file"`
	Texts   int    `json:"texts"`
	Missing int    `json:"missing"` // the app shows the English text for these
}

// WriteAtomic writes path through a temp file in the same directory and a
// rename, so a reader sees the old file or the new one, never a part.
func WriteAtomic(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Publish writes <lang>.json for each live file, then index.json last, each
// one atomically. The English file is published too, so the app has one
// source for every language.
func Publish(outDir, tag, commit string, langs i18n.Languages, en []i18n.Message, live map[string]rung.Result) error {
	idx := Index{Round: tag, Commit: commit, Source: langs.Source}
	for _, l := range langs.Languages {
		p := Published{Lang: l.Code, Round: tag, Commit: commit, Dir: l.Dir, Messages: map[string]string{}}
		missing := 0
		if l.Role == "source" {
			for _, m := range en {
				p.Messages[m.ID] = m.Text
			}
		} else {
			r, ok := live[l.Code]
			if !ok {
				continue
			}
			for _, c := range r.Messages {
				p.Messages[c.ID] = c.Text
			}
			missing = len(r.Missing)
		}
		file := l.Code + ".json"
		if err := WriteAtomic(filepath.Join(outDir, file), i18n.Marshal(p)); err != nil {
			return fmt.Errorf("publish %s: %v", file, err)
		}
		idx.Languages = append(idx.Languages, IndexLang{Code: l.Code, Name: l.Name, Native: l.Native, Dir: l.Dir, File: file,
			Texts: len(p.Messages), Missing: missing})
	}
	return WriteAtomic(filepath.Join(outDir, "index.json"), i18n.Marshal(idx))
}

// Graph builds the text graph of the deployed app text. The hand-made parts of
// concepts.json (pairs, opens, concepts) were written for an earlier app text;
// cmd/extract fails on a stale one, but a round at deploy time must not, so a
// pair or an opens edge that names a text or screen that is gone, and a
// concept that matches no text, are left out and returned for the plan.
func Graph(src i18n.Source, files map[string]string, msgs []i18n.Message, cf i18n.ConceptFile) (i18n.GraphFile, []string, error) {
	screens, err := i18n.Screens(files, msgs)
	if err != nil {
		return i18n.GraphFile{}, nil, err
	}
	ids := map[string]bool{}
	for _, m := range msgs {
		ids[m.ID] = true
	}
	var dropped []string
	keep := i18n.ConceptFile{Note: cf.Note, Concepts: map[string]i18n.Concept{}}
	used := map[string]bool{}
	for _, m := range msgs {
		for _, c := range i18n.Uses(m.Text, cf.Concepts) {
			used[c] = true
		}
	}
	for n, c := range cf.Concepts {
		if used[n] {
			keep.Concepts[n] = c
		} else {
			dropped = append(dropped, "concept "+n+": matches no text")
		}
	}
	for _, o := range cf.Opens {
		if ids[o[0]] && screens[o[1]] != nil {
			keep.Opens = append(keep.Opens, o)
		} else {
			dropped = append(dropped, fmt.Sprintf("opens %s %s: no such text or screen", o[0], o[1]))
		}
	}
	for _, p := range cf.Pairs {
		if ids[p[0]] && ids[p[1]] {
			keep.Pairs = append(keep.Pairs, p)
		} else {
			dropped = append(dropped, fmt.Sprintf("pair %s %s: no such text", p[0], p[1]))
		}
	}
	sort.Strings(dropped)
	g, err := i18n.BuildGraph(src, msgs, screens, keep)
	return g, dropped, err
}
