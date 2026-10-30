package rung

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"v3rde/i18n"
	"v3rde/i18n/mask"
	"v3rde/i18n/muse"
)

// runBatched translates each group of cfg.Batch in one call (L4k). Every text
// of the answer gets its own ledger entry (with the group in Batch) and the
// same tag check as one call. A text with no good answer from its group, and a
// text of only keep names, goes to runUnique: one call (or a copy) of its own.
func runBatched(ctx context.Context, cfg Config, msgs []i18n.Message, also map[string][]string, tr Translator, sc Scorer, ledger io.Writer) (Result, error) {
	now := func() string { return time.Now().UTC().Format(time.RFC3339) }
	enc := json.NewEncoder(ledger)
	enc.SetEscapeHTML(false)
	masks := map[string]mask.Masked{}
	var todo []i18n.Message
	for _, m := range msgs {
		mk, err := mask.Mask(m.Text, cfg.Keep)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %v", m.ID, err)
		}
		masks[m.ID] = mk
		if !onlyTags(mk.Text) {
			todo = append(todo, m)
		}
	}
	groups := cfg.Batch(todo)
	type job struct{ g, try int }
	jobs := make(chan job)
	out := make(chan []Entry)
	var wg sync.WaitGroup
	for w := 0; w < cfg.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				out <- batchCall(ctx, cfg, groups[j.g], masks, also, j.try, tr, now)
			}
		}()
	}
	go func() {
		for g := range groups {
			for t := 1; t <= cfg.K; t++ {
				select {
				case jobs <- job{g, t}:
				case <-ctx.Done():
				}
			}
		}
		close(jobs)
		wg.Wait()
		close(out)
	}()
	var entries []Entry
	var werr error
	for es := range out {
		for _, e := range es {
			entries = append(entries, e)
			if werr == nil {
				werr = enc.Encode(e) // now, so a later failure keeps the paid answers
			}
		}
	}
	if werr != nil {
		return Result{}, fmt.Errorf("ledger: %v", werr)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	good := map[string]bool{}
	for _, e := range entries {
		if e.Error == "" {
			good[e.ID] = true
		}
	}
	var rest []i18n.Message // no good batch answer, or only keep names
	for _, m := range msgs {
		if !good[m.ID] {
			rest = append(rest, m)
		}
	}
	res, err := finish(ctx, cfg, msgs, entries, sc, enc)
	if err != nil || len(rest) == 0 {
		return res, err
	}
	one := cfg
	one.Batch = nil
	add, err := runUnique(ctx, one, rest, also, tr, sc, ledger)
	if err != nil {
		return Result{}, err
	}
	by := map[string]Chosen{}
	for _, c := range res.Messages {
		by[c.ID] = c
	}
	for _, c := range add.Messages {
		by[c.ID] = c
	}
	res.Messages, res.Missing = nil, []string{}
	for _, m := range msgs {
		if c, ok := by[m.ID]; ok {
			res.Messages = append(res.Messages, c)
		} else {
			res.Missing = append(res.Missing, m.ID)
		}
	}
	res.Unscored = res.Unscored || add.Unscored
	return res, nil
}

// batchCall sends one group and returns one entry for each text of it.
func batchCall(ctx context.Context, cfg Config, group []i18n.Message, masks map[string]mask.Masked, also map[string][]string,
	try int, tr Translator, now func() string) []Entry {
	name := fmt.Sprintf("%s+%d", group[0].ID, len(group))
	items := make([]muse.BatchItem, len(group))
	es := make([]Entry, len(group))
	for i, m := range group {
		mk := masks[m.ID]
		about := ""
		if cfg.About != nil {
			about = cfg.About(m.ID, mk)
		}
		items[i] = muse.BatchItem{ID: m.ID, Context: about, Text: mk.Text}
		es[i] = Entry{Kind: "try", Time: now(), Run: cfg.Run, ID: m.ID, Also: also[m.ID], Lang: cfg.Lang.Code, Try: try, Source: m.Text,
			Masked: mk.Text, Settings: cfg.Settings, Commit: cfg.Commit, Prompt: cfg.Prompt, Batch: name}
		if cfg.Fix != nil {
			es[i].Fix = cfg.Fix(m.ID)
		}
		if cfg.From != nil {
			es[i].SpreadFrom = cfg.From(m.ID)
		}
	}
	fail := func(msg string, rt int) []Entry {
		for i := range es {
			es[i].Error, es[i].ReasoningTokens = msg, rt
		}
		return es
	}
	sys, user, err := muse.PromptBatchWith(cfg.Lang.Name, cfg.Lang.Code, items)
	if err != nil {
		return fail(err.Error(), 0)
	}
	r, err := tr.Chat(ctx, sys, user)
	if err != nil {
		return fail(err.Error(), r.ReasoningTokens)
	}
	got, err := muse.ParseBatch(r.Text)
	if err != nil {
		return fail(err.Error(), r.ReasoningTokens)
	}
	for i, m := range group {
		es[i].ReasoningTokens = r.ReasoningTokens / len(group) // the call's reasoning, shared
		raw, ok := got[m.ID]
		if !ok {
			es[i].Error = "batch: no translation for this id"
			continue
		}
		es[i].Raw = raw
		if txt, err := masks[m.ID].Restore(raw); err != nil {
			es[i].Error = "tags: " + err.Error()
		} else {
			es[i].Text = txt
		}
	}
	return es
}
