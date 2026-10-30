package rung

import "fmt"

// Calls counts the model calls of a result from its ledger: one for each try
// of one text, one for each try of a batch group. Only the runs of the result
// (its run and the runs that Merge kept) are counted; copies are not calls.
func Calls(ledgerPath string, res Result) (int, error) {
	es, _, err := readLedger(ledgerPath)
	if err != nil {
		return 0, err
	}
	runs := map[string]bool{res.Run: true}
	for _, p := range res.Previous {
		runs[p] = true
	}
	seen := map[string]bool{}
	n := 0
	for _, e := range es {
		if !runs[e.Run] || e.Copied {
			continue
		}
		if e.Batch != "" {
			k := fmt.Sprintf("%s|%s|%d", e.Run, e.Batch, e.Try)
			if seen[k] {
				continue
			}
			seen[k] = true
		}
		n++
	}
	return n, nil
}

// TextScores reads the scores of a ledger by text: id + "\x00" + translation ->
// the lowest score of a try with that text. A copy of a result (for example
// <lang>.before-round1.json) has no scores of its own when cmd/score ran
// later; its scores are in the ledger.
func TextScores(ledgerPath string) (map[string]float64, error) {
	es, scores, err := readLedger(ledgerPath)
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, e := range es {
		s, ok := scores[tryKey{e.Run, e.ID, e.Try}]
		if e.Error != "" || !ok {
			continue
		}
		k := e.ID + "\x00" + e.Text
		if old, seen := out[k]; !seen || s < old {
			out[k] = s
		}
	}
	return out, nil
}
