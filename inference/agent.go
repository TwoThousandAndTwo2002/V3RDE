package inference

// Agent mode: Muse Spark orchestrates and SAM 3.1 is its tool. SAM takes ONE
// concept per request (dev.meta.ai/docs/sam/segmenting: lists give zero
// matches), so the model looks at the photo, decides which kinds of food it
// sees, and calls segment(concept) once per kind. Each call returns
// numbered boxes on Muse's 0–1000 grid (dev.meta.ai/docs/image-understanding);
// the final answer is the same {"items":[...]} contract as the single call,
// so the same validation and grouping apply. Every instance SAM finds is
// counted; at most MaxNamed boxes are given to the model to name.
// Tool calling: function tools, tool_choice auto, results as
// function_call_output with previous_response_id (dev.meta.ai/docs
// /tool-calling, READ 2026-10-26).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// MaxAgentTurns bounds the tool loop (the API does not bound function calls).
const MaxAgentTurns = 4

// AgentPrompt is the orchestration ask. The field rules are analyze.Prompt's.
const AgentPrompt = `You are listing donated food from this photo.
1. Look at the photo and decide which kinds of food items are in it.
2. Call segment once for EACH kind, with a short noun phrase (for example "cereal box", "canned beans", "apple"). Do not list several kinds in one call.
3. segment returns numbered boxes. ` + gridRule + `
4. When you have the boxes, return ONLY valid JSON:
` + itemFields

var segmentTool = map[string]any{
	"type": "function", "name": "segment",
	"description": "Find every instance of ONE kind of food in the photo. Returns numbered boxes [x1, y1, x2, y2] on the 0-1000 grid.",
	"parameters": map[string]any{
		"type":       "object",
		"properties": map[string]any{"concept": map[string]any{"type": "string", "description": "a short noun phrase, one kind of item"}},
		"required":   []string{"concept"},
	},
}

type boxOut struct {
	Box  int  `json:"box"`
	Norm Norm `json:"xyxy"`
}

func (e *Engine) agent(ctx context.Context, inv *Inventory, ph *photo) error {
	p := ph.prepared()
	if p == nil {
		return e.whole(ctx, inv, ph, nil)
	}
	inv.PrepW, inv.PrepH = p.W, p.H
	jb, err := JPEG(p.Img)
	if err != nil {
		return e.whole(ctx, inv, ph, nil)
	}
	tools := []any{segmentTool}
	c, err := e.API.Do(ctx, "muse:agent:0", map[string]any{
		"model": e.Muse, "tools": tools,
		"input": UserMessage(TextPart(AgentPrompt), ImagePart(jb, "image/jpeg"))})
	inv.add(c)
	if err != nil {
		return err
	}
	var all, named []Instance // every instance found; the ones given to the model
	for turn := 1; turn <= MaxAgentTurns; turn++ {
		fcs := c.FunctionCalls()
		if len(fcs) == 0 {
			break
		}
		// every segment call of this turn runs at the same time; the results
		// are then merged in the model's order, so the numbering is stable
		concepts := make([]string, len(fcs))
		found := make([][]Instance, len(fcs))
		errs := make([]error, len(fcs))
		var wg sync.WaitGroup
		for i, fc := range fcs {
			var args struct {
				Concept string `json:"concept"`
			}
			if fc.Name != "segment" || json.Unmarshal([]byte(fc.Arguments), &args) != nil || strings.TrimSpace(args.Concept) == "" {
				continue
			}
			concepts[i] = strings.TrimSpace(args.Concept)
			wg.Add(1)
			go func(i int) { defer wg.Done(); found[i], errs[i] = e.segment(ctx, inv, ph, concepts[i]) }(i)
		}
		wg.Wait()
		var outputs []any
		for i, fc := range fcs {
			res := map[string]any{}
			switch {
			case concepts[i] == "":
				res["error"] = "call segment with {\"concept\": \"<one kind of food>\"}"
			case errs[i] != nil:
				res["error"] = "segment failed"
			default:
				outs := []boxOut{}
				for _, in := range e.keep(found[i]) {
					dup := false
					for _, k := range all {
						if u, s := iou(in, k); u > 0.7 || s > 0.9 {
							dup = true
							break
						}
					}
					if dup {
						continue
					}
					all = append(all, in)
					if SeenSize(in, p.W, p.H).ShortPx < float64(e.NameMinPx) {
						inv.Small++ // counted, too small to name
						continue
					}
					if len(named) < e.MaxNamed {
						named = append(named, in)
						outs = append(outs, boxOut{len(named), NormBox(in)})
					}
				}
				res["boxes"] = outs
				if len(named) >= e.MaxNamed {
					res["note"] = fmt.Sprintf("box limit %d reached; answer now", e.MaxNamed)
				}
			}
			b, _ := json.Marshal(res)
			outputs = append(outputs, map[string]any{"type": "function_call_output", "call_id": fc.CallID, "output": string(b)})
		}
		c, err = e.API.Do(ctx, fmt.Sprintf("muse:agent:%d", turn), map[string]any{
			"model": e.Muse, "tools": tools, "previous_response_id": c.ID(), "input": outputs})
		inv.add(c)
		if err != nil {
			return err
		}
	}
	inv.Kept, inv.Named, inv.Unnamed = len(all), len(named), len(all)-len(named)-inv.Small
	inv.CloserPhoto = inv.Small > 0
	if len(named) <= 1 {
		// one item (or none found): the analyze-identical answer
		inv.Unnamed = 0
		return e.whole(ctx, inv, ph, named)
	}
	ents, _ := parseEntries(c.Text(), len(named))
	got := map[int]Item{}
	var failed []int
	for n := 1; n <= len(named); n++ {
		if en, ok := ents[n]; ok {
			inv.score(named[n-1], en.point)
			if en.notFood {
				inv.notListed(named[n-1], n, en.res, "agent")
				continue
			}
			got[n] = Item{Result: en.res, Source: "agent", Members: []int{n}, sameAs: en.sameAs, Tray: en.tray, Pan: en.panRef(),
				Check: SeenSize(named[n-1], p.W, p.H).ShortPx < float64(e.ConfidentPx)}
		} else {
			failed = append(failed, n)
		}
	}
	if len(failed) > 0 { // the same one retry as the single call
		var more map[int]Item
		more, failed = e.single(ctx, inv, ph, named, failed, "retry")
		for n, it := range more {
			got[n] = it
		}
	}
	for _, n := range failed {
		inv.Rejected = append(inv.Rejected, Rejection{Box: n, Reason: "no valid item in the call(s)"})
	}
	inv.members = named
	inv.Items = group(got, named, p)
	if len(inv.Items) == 0 {
		return ErrNoItem
	}
	return nil
}
