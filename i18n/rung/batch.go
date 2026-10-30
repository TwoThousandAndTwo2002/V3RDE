package rung

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"v3rde/i18n"
)

// Batch scoring (L4i): translate many languages and settings with
// -score=false, then score every new translation in one container run, so
// MetricX loads its model once.

type tryKey struct {
	run, id string
	try     int
}

// readLedger reads the try lines and the scores of a ledger. Lines from before
// L4f have no kind and carry their score in the line.
func readLedger(path string) ([]Entry, map[tryKey]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var tries []Entry
	scores := map[tryKey]float64{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var head struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			return nil, nil, fmt.Errorf("%s: %v", path, err)
		}
		if head.Kind == "score" {
			var s ScoreLine
			if err := json.Unmarshal(line, &s); err != nil {
				return nil, nil, err
			}
			scores[tryKey{s.Run, s.ID, s.Try}] = s.Score
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, nil, err
		}
		if e.Score != nil {
			scores[tryKey{e.Run, e.ID, e.Try}] = *e.Score
		}
		tries = append(tries, e)
	}
	return tries, scores, sc.Err()
}

// Rescore rebuilds a result from its ledger tries (with their scores): for
// each id the lowest-scored try; the ids with the same text and context reuse it.
// Messages whose id has no try in the ledger stay as they are.
func Rescore(res Result, msgs []i18n.Message, tries []Entry) Result {
	runs := map[string]bool{res.Run: true}
	for _, p := range res.Previous {
		runs[p] = true
	}
	var mine []Entry
	alsoOf := map[string]string{} // also id -> rep id
	for _, e := range tries {
		if !runs[e.Run] {
			continue
		}
		mine = append(mine, e)
		for _, a := range e.Also {
			alsoOf[a] = e.ID
		}
	}
	sort.SliceStable(mine, func(a, b int) bool { return mine[a].Try < mine[b].Try })
	best, count := choose(mine)
	old := map[string]Chosen{}
	for _, c := range res.Messages {
		old[c.ID] = c
	}
	out := res
	out.Messages, out.Missing, out.Unscored = nil, []string{}, false
	for _, m := range msgs {
		rep := m.ID
		if r, ok := alsoOf[m.ID]; ok {
			rep = r
		}
		if c := best[rep]; c != nil {
			x := *c
			x.Candidates = count[rep]
			if rep != m.ID {
				x.ID, x.SameAs = m.ID, rep
			}
			out.Messages = append(out.Messages, x)
		} else if c, ok := old[m.ID]; ok {
			out.Messages = append(out.Messages, c)
		} else {
			out.Missing = append(out.Missing, m.ID)
		}
	}
	return out
}

// ScoreAll scores, in one Scorer call, every try of the given results that has
// no score yet, writes a score line to each ledger, and rewrites each result
// with the lowest-scored try. A result is <dir>/<lang>.json, its ledger
// <dir>/<lang>.ledger.jsonl. It returns the number of pairs scored.
func ScoreAll(ctx context.Context, results []string, msgs []i18n.Message, sc Scorer) (int, error) {
	type file struct {
		path, ledger string
		res          Result
		tries        []Entry
		scores       map[tryKey]float64
	}
	var files []*file
	var pairs []Pair
	type ref struct{ f, t int }
	var refs []ref
	for _, p := range results {
		f := &file{path: p}
		if err := i18n.ReadJSON(p, &f.res); err != nil {
			return 0, fmt.Errorf("%s: %v", p, err)
		}
		f.ledger = filepath.Join(filepath.Dir(p), f.res.Lang+".ledger.jsonl")
		var err error
		if f.tries, f.scores, err = readLedger(f.ledger); err != nil {
			return 0, err
		}
		runs := map[string]bool{f.res.Run: true}
		for _, r := range f.res.Previous {
			runs[r] = true
		}
		for t, e := range f.tries {
			if !runs[e.Run] || e.Error != "" || e.Copied {
				continue
			}
			if _, done := f.scores[tryKey{e.Run, e.ID, e.Try}]; done {
				continue
			}
			pairs = append(pairs, Pair{Source: e.Source, Hypothesis: e.Text})
			refs = append(refs, ref{len(files), t})
		}
		files = append(files, f)
	}
	if len(pairs) > 0 {
		if err := sc.Check(ctx); err != nil {
			return 0, err
		}
		got, err := sc.Score(ctx, pairs)
		if err != nil {
			return 0, err
		}
		if len(got) != len(pairs) {
			return 0, fmt.Errorf("score: %d scores for %d pairs", len(got), len(pairs))
		}
		lines := map[int][]ScoreLine{}
		now := time.Now().UTC().Format(time.RFC3339)
		for n, r := range refs {
			f, e := files[r.f], files[r.f].tries[r.t]
			f.scores[tryKey{e.Run, e.ID, e.Try}] = got[n]
			lines[r.f] = append(lines[r.f], ScoreLine{Kind: "score", Time: now, Run: e.Run, ID: e.ID, Lang: f.res.Lang, Try: e.Try, Score: got[n]})
		}
		for i, ls := range lines {
			lf, err := os.OpenFile(files[i].ledger, os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return 0, err
			}
			enc := json.NewEncoder(lf)
			enc.SetEscapeHTML(false)
			for _, l := range ls {
				if err := enc.Encode(l); err != nil {
					lf.Close()
					return 0, err
				}
			}
			if err := lf.Close(); err != nil {
				return 0, err
			}
		}
	}
	for _, f := range files {
		for t := range f.tries {
			e := &f.tries[t]
			if s, ok := f.scores[tryKey{e.Run, e.ID, e.Try}]; ok {
				v := s
				e.Score = &v
			}
		}
		res := Rescore(f.res, scope(f.res, msgs), f.tries)
		b, _ := json.MarshalIndent(res, "", "  ")
		if err := os.WriteFile(f.path, append(b, '\n'), 0o644); err != nil {
			return 0, err
		}
	}
	return len(pairs), nil
}

// scope: the messages a result covers (a sweep result covers a few ids).
func scope(r Result, msgs []i18n.Message) []i18n.Message {
	have := map[string]bool{}
	for _, c := range r.Messages {
		have[c.ID] = true
	}
	for _, id := range r.Missing {
		have[id] = true
	}
	var out []i18n.Message
	for _, m := range msgs {
		if have[m.ID] {
			out = append(out, m)
		}
	}
	return out
}
