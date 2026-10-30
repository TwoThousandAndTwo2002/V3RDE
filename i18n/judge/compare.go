package judge

import (
	"sort"
)

// Arm is one way to translate (L4k): plain, context, context + loop, batch, ...
// The comparison uses the same measures for every arm: the model calls, the
// MetricX QE scores, the checks with no model, and the two judges. None of
// them is an accuracy measure: there are no human labels.
type Arm struct {
	Arm          string         `json:"arm"`
	Lang         string         `json:"lang"`
	Texts        int            `json:"texts"`
	Missing      int            `json:"missing"`
	Calls        int            `json:"calls"`
	Scored       int            `json:"scored"`
	Median       float64        `json:"metricx_median"`
	Mean         float64        `json:"metricx_mean"`
	Over5        int            `json:"metricx_over_5"`
	Checks       map[string]int `json:"checks"`
	Judged       bool           `json:"judged"`
	Groups       int            `json:"judge_groups"`
	GroupsFailed map[string]int `json:"groups_failed"` // model -> groups it failed
	Flagged      int            `json:"flagged"`       // texts flagged by a check or a judge
	ByBoth       int            `json:"flagged_by_both_judges"`
	ByOne        map[string]int `json:"flagged_by_one_judge_only"` // model -> texts only it flagged
	JudgeErrors  int            `json:"judge_errors"`
}

// Scores is the MetricX score of each text of a result (nil: not scored or copied).
type Scores map[string]*float64

// Measure fills one Arm from a language's translations (s), its scores, its
// calls and its judge report (nil when the judges did not read this arm).
func Measure(arm string, s Set, sc Scores, calls int, rep *Report) Arm {
	a := Arm{Arm: arm, Lang: s.Lang.Code, Texts: len(s.Trans), Calls: calls, Checks: map[string]int{},
		GroupsFailed: map[string]int{}, ByOne: map[string]int{}}
	for _, m := range s.Msgs {
		if _, ok := s.Trans[m.ID]; !ok {
			a.Missing++
		}
	}
	var xs []float64
	for id := range s.Trans {
		if v := sc[id]; v != nil && s.Shared[id] == "" {
			xs = append(xs, *v)
		}
	}
	sort.Float64s(xs)
	a.Scored = len(xs)
	if n := len(xs); n > 0 {
		sum := 0.0
		for _, x := range xs {
			sum += x
			if x > 5 {
				a.Over5++
			}
		}
		a.Mean = sum / float64(n)
		if n%2 == 1 {
			a.Median = xs[n/2]
		} else {
			a.Median = (xs[n/2-1] + xs[n/2]) / 2
		}
	}
	for _, is := range Checks(s) {
		a.Checks[is.Check]++
	}
	if rep == nil {
		return a
	}
	a.Judged, a.JudgeErrors = true, rep.Errors
	groups := map[string]bool{}
	models := map[string]map[string]bool{} // id -> judge models that flagged it
	for _, v := range rep.Verdicts {
		groups[v.Group.Kind+":"+v.Group.Name] = true
		if v.Verdict == "fail" {
			a.GroupsFailed[v.Model]++
		}
		for _, p := range v.Issues {
			if models[p.ID] == nil {
				models[p.ID] = map[string]bool{}
			}
			models[p.ID][v.Model] = true
		}
	}
	a.Groups = len(groups)
	a.Flagged = len(rep.Flagged)
	for _, ms := range models {
		if len(ms) >= 2 {
			a.ByBoth++
			continue
		}
		for m := range ms {
			a.ByOne[m]++
		}
	}
	return a
}
