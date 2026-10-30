package judge

import (
	"sort"

	"v3rde/i18n"
)

// Flag is one finding about one text, for the review page: a check (no model)
// or a judge (model name), and the group it came from.
type Flag struct {
	By     string `json:"by"`              // the check name, or the judge model
	Detail string `json:"detail"`          // the reason
	Group  string `json:"group,omitempty"` // concept or screen name of a judge
}

// Line is the state of one text in one language after the loop (L4j).
type Line struct {
	Text        string   `json:"text"`
	Score       *float64 `json:"score,omitempty"`
	Before      string   `json:"before,omitempty"` // the translation before the fix round, when it changed
	BeforeScore *float64 `json:"before_score,omitempty"`
	Round1      []Flag   `json:"round1,omitempty"` // findings before the fix round
	Now         []Flag   `json:"now,omitempty"`    // findings of the last cmd/judge
	SpreadFrom  string   `json:"spread_from,omitempty"`
	Revised     bool     `json:"revised,omitempty"`
	SameAs      string   `json:"same_as,omitempty"`
	// Status: ok (never flagged), fixed (flagged, corrected, not flagged now),
	// spread (reached by a correction, not flagged now), review (corrected once
	// and flagged again: a person decides), flagged (flagged, not corrected yet),
	// missing (no translation).
	Status string `json:"status"`
}

// Summary counts the statuses of one language.
type Summary struct {
	Lang        string         `json:"lang"`
	Name        string         `json:"name"`
	Texts       int            `json:"texts"`
	Status      map[string]int `json:"status"`
	Round1      int            `json:"round1_flagged"`
	Now         int            `json:"now_flagged"`
	GroupsFail1 map[string]int `json:"round1_groups_failed"` // judge model -> groups failed
	GroupsFail  map[string]int `json:"now_groups_failed"`
	Errors1     int            `json:"round1_judge_errors"`
	Errors      int            `json:"now_judge_errors"`
}

// Text is one English text with what the graph knows about it.
type Text struct {
	ID       string   `json:"id"`
	En       string   `json:"en"`
	Kind     string   `json:"kind,omitempty"`
	Screen   string   `json:"screen,omitempty"`
	Does     string   `json:"does,omitempty"`
	Concepts []string `json:"concepts,omitempty"`
}

// Review is lif_review.json: all languages, for the review page and the graph page.
type Review struct {
	Commit  string                     `json:"en_commit"`
	Langs   []Summary                  `json:"langs"`
	Texts   []Text                     `json:"texts"`
	ByLang  map[string]map[string]Line `json:"by_lang"`
	Concept map[string][]string        `json:"concepts"` // concept -> text ids
}

// Lang is what cmd/report reads for one language: the final result, the result
// before the fix round and its report (both nil when no -fix ran), and the last report.
type Lang struct {
	Language i18n.Language
	Final    Result
	Before   *Result
	Round1   *Report
	Now      *Report
}

// Result is the part of a rung result that the review needs (rung imports
// nothing from judge, so judge does not import rung).
type Result struct {
	Messages []struct {
		ID         string   `json:"id"`
		Text       string   `json:"text"`
		Score      *float64 `json:"score"`
		SameAs     string   `json:"same_as"`
		Revised    bool     `json:"revised"`
		SpreadFrom string   `json:"spread_from"`
	} `json:"messages"`
}

func flagsOf(r *Report) map[string][]Flag {
	out := map[string][]Flag{}
	if r == nil {
		return out
	}
	for _, is := range append(append([]Issue{}, r.Checks...), Issues(r.Verdicts)...) {
		by := is.Check
		if is.Model != "" {
			by = is.Model
		}
		for _, id := range is.IDs {
			out[id] = append(out[id], Flag{By: by, Detail: is.Detail, Group: is.Group})
		}
	}
	return out
}

func groupsFailed(r *Report) (map[string]int, int) {
	out := map[string]int{}
	if r == nil {
		return out, 0
	}
	for _, v := range r.Verdicts {
		if v.Verdict == "fail" {
			out[v.Model]++
		}
	}
	return out, r.Errors
}

// BuildReview joins the texts, the graph and each language's results and reports.
func BuildReview(en i18n.EnFile, g i18n.GraphFile, cf i18n.ContextFile, langs []Lang) Review {
	rv := Review{Commit: en.Source.Commit, ByLang: map[string]map[string]Line{}, Concept: map[string][]string{}}
	uses := map[string][]string{}
	for _, e := range g.Edges {
		if e.Kind == "uses" {
			c := e.To[len("concept:"):]
			uses[e.From] = append(uses[e.From], c)
			rv.Concept[c] = append(rv.Concept[c], e.From)
		}
	}
	for c := range rv.Concept {
		sort.Strings(rv.Concept[c])
	}
	for _, m := range en.Messages {
		cx := cf.Contexts[m.ID]
		cs := append([]string{}, uses[m.ID]...)
		sort.Strings(cs)
		rv.Texts = append(rv.Texts, Text{ID: m.ID, En: m.Text, Kind: cx.Kind, Screen: cx.Screen, Does: cx.Does, Concepts: cs})
	}
	for _, l := range langs {
		code := l.Language.Code
		final := map[string]int{}
		for i, c := range l.Final.Messages {
			final[c.ID] = i
		}
		before := map[string]int{}
		if l.Before != nil {
			for i, c := range l.Before.Messages {
				before[c.ID] = i
			}
		}
		r1, now := flagsOf(l.Round1), flagsOf(l.Now)
		if l.Round1 == nil { // no fix round yet: the last report is round 1
			r1 = now
		}
		sum := Summary{Lang: code, Name: l.Language.Name, Status: map[string]int{}}
		sum.GroupsFail1, sum.Errors1 = groupsFailed(l.Round1)
		sum.GroupsFail, sum.Errors = groupsFailed(l.Now)
		if l.Round1 == nil {
			sum.GroupsFail1, sum.Errors1 = sum.GroupsFail, sum.Errors
		}
		lines := map[string]Line{}
		for _, m := range en.Messages {
			ln := Line{Round1: r1[m.ID], Now: now[m.ID]}
			i, ok := final[m.ID]
			if !ok {
				ln.Status = "missing"
				lines[m.ID] = ln
				sum.Status[ln.Status]++
				continue
			}
			c := l.Final.Messages[i]
			ln.Text, ln.Score, ln.SameAs, ln.Revised, ln.SpreadFrom = c.Text, c.Score, c.SameAs, c.Revised, c.SpreadFrom
			if j, ok := before[m.ID]; ok && l.Before.Messages[j].Text != c.Text {
				ln.Before, ln.BeforeScore = l.Before.Messages[j].Text, l.Before.Messages[j].Score
			}
			switch {
			case len(ln.Now) > 0 && ln.Revised:
				ln.Status = "review"
			case len(ln.Now) > 0:
				ln.Status = "flagged"
			case ln.SpreadFrom != "":
				ln.Status = "spread"
			case ln.Revised:
				ln.Status = "fixed"
			default:
				ln.Status = "ok"
			}
			if len(ln.Round1) > 0 {
				sum.Round1++
			}
			if len(ln.Now) > 0 {
				sum.Now++
			}
			sum.Status[ln.Status]++
			lines[m.ID] = ln
		}
		sum.Texts = len(en.Messages)
		rv.ByLang[code] = lines
		rv.Langs = append(rv.Langs, sum)
	}
	return rv
}
