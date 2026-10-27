package inference

// Two passes (T9, 2026-10-26). Output tokens (with reasoning) are most of
// the cost and the time, and the one-call answer writes all eight contract
// fields for every box. Here:
//
//	pass 1 (NameEffort):   every box -> name, same_as, food, point. Short.
//	pass 2 (DetailEffort): the eight fields ONCE per product group.
//
// Both passes send the same image first, then the text: Meta's prompt cache
// matches the prefix ("Put stable content first", dev.meta.ai/docs
// /prompt-caching, READ 2026-10-26), so pass 2 reads the image at the cached
// rate. Each request sets its own reasoning effort ("set it on every
// request", dev.meta.ai/docs/protocols/responses).

import (
	"context"
	"fmt"
	"strings"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

const nameFields = `{"items": [{"box": <box number>, "point": [x, y] (the centre of the item you identified, on the same grid),
"same_as": <number of an earlier box holding the identical product, or null>,
"food": true, or false if the item is a product that is not food (for example paper towels or soap),
"name": <the product name, from its label when you can read it, 6 words maximum>}]}
One entry per box.`

// NamePrompt is pass 1.
func NamePrompt(boxes []Instance, marks, coords bool) string {
	return boxList(boxes, marks, coords) + "For EVERY box, name the item in it. Return ONLY valid JSON:\n" + nameFields
}

// DetailPrompt is pass 2: one entry per group, analyze.Prompt's fields.
func DetailPrompt(groups []Item, named []Instance, marks, coords bool) string {
	var sb strings.Builder
	sb.WriteString("These products were found in this photo. ")
	if coords {
		sb.WriteString(gridRule + " ")
	}
	sb.WriteString("For EACH group, give the details of ONE unit.\n")
	for gi, g := range groups {
		fmt.Fprintf(&sb, "group %d: %q, %d unit(s)", gi+1, g.Name, g.Count)
		if marks {
			fmt.Fprintf(&sb, ", tag(s) %s", joinInts(g.Members))
		}
		if coords {
			n := NormBox(named[g.Members[0]-1])
			fmt.Fprintf(&sb, ", for example [%d, %d, %d, %d]", n[0], n[1], n[2], n[3])
		}
		sb.WriteString("\n")
	}
	sb.WriteString(`Return ONLY valid JSON:
{"groups": [{"group": <group number>, name (keep the given name unless it is wrong), description, is_non_veg (0 vegetarian or 1),
number_of_serving, serving_type (individual or bulk), meal_condition
(ready_to_eat, cooking_required, sealed), weight (kg), meal_cost}]}
One entry per group. description must be ONE sentence, 10 words maximum.
Use conservative estimates if unsure.`)
	return sb.String()
}

func joinInts(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = fmt.Sprint(n)
	}
	return strings.Join(s, ", ")
}

// imageFirst is one user message: the image (stable, cached), then the text.
func imageFirst(jb []byte, text string) []any {
	return UserMessage(ImagePart(jb, "image/jpeg"), TextPart(text))
}

func (e *Engine) callEffort(ctx context.Context, inv *Inventory, purpose, effort string, input []any) (*Call, error) {
	body := map[string]any{"model": e.Muse, "input": input}
	if effort != "" {
		body["reasoning"] = map[string]any{"effort": effort}
	}
	c, err := e.API.Do(ctx, purpose, body)
	inv.add(c)
	return c, err
}

type nameEntry struct {
	name    string
	sameAs  int // original box number
	notFood bool
}

// namePass names the boxes in which (numbers into named); returns names by
// original number and the numbers without a valid name.
func (e *Engine) namePass(ctx context.Context, inv *Inventory, jb func([]Instance) ([]byte, error), named []Instance, which []int, purpose string) (map[int]nameEntry, []int) {
	got := map[int]nameEntry{}
	sub := make([]Instance, len(which))
	for i, n := range which {
		sub[i] = named[n-1]
	}
	pic, err := jb(sub)
	if err != nil {
		return got, which
	}
	c, err := e.callEffort(ctx, inv, "muse:"+purpose, e.NameEffort, imageFirst(pic, NamePrompt(sub, e.Marks, !e.NoCoords)))
	if err != nil {
		return got, which
	}
	m, err := analyze.ExtractJSON(c.Text())
	if err != nil {
		return got, which
	}
	items, _ := m["items"].([]any)
	for _, raw := range items {
		obj, _ := raw.(map[string]any)
		bf, ok := obj["box"].(float64)
		b := int(bf)
		name, _ := obj["name"].(string)
		if !ok || float64(b) != bf || b < 1 || b > len(sub) || strings.TrimSpace(name) == "" {
			continue
		}
		n := which[b-1]
		if _, dup := got[n]; dup {
			continue
		}
		var pt *[2]float64
		if p, ok := obj["point"].([]any); ok && len(p) == 2 {
			x, okx := p[0].(float64)
			y, oky := p[1].(float64)
			if okx && oky {
				pt = &[2]float64{x, y}
			}
		}
		inv.score(sub[b-1], pt)
		ne := nameEntry{name: strings.TrimSpace(name)}
		if f, isBool := obj["food"].(bool); isBool && !f {
			ne.notFood = true
		}
		if sf, ok := obj["same_as"].(float64); ok && sf >= 1 && sf < float64(b) && sf == float64(int(sf)) {
			ne.sameAs = which[int(sf)-1]
		}
		got[n] = ne
	}
	var failed []int
	for _, n := range which {
		if _, ok := got[n]; !ok {
			failed = append(failed, n)
		}
	}
	return got, failed
}

// detailPass fills the contract for groups (indexes into groups); returns
// results by index and the indexes still without a valid answer.
func (e *Engine) detailPass(ctx context.Context, inv *Inventory, pic []byte, groups []Item, named []Instance, which []int, purpose string) (map[int]analyze.Result, []int) {
	got := map[int]analyze.Result{}
	sub := make([]Item, len(which))
	for i, gi := range which {
		sub[i] = groups[gi]
	}
	c, err := e.callEffort(ctx, inv, "muse:"+purpose, e.DetailEffort, imageFirst(pic, DetailPrompt(sub, named, e.Marks, !e.NoCoords)))
	if err != nil {
		return got, which
	}
	m, err := analyze.ExtractJSON(c.Text())
	if err != nil {
		return got, which
	}
	gs, _ := m["groups"].([]any)
	for _, raw := range gs {
		obj, _ := raw.(map[string]any)
		gf, ok := obj["group"].(float64)
		g := int(gf)
		if !ok || float64(g) != gf || g < 1 || g > len(sub) {
			continue
		}
		r, err := analyze.Validate(obj)
		if err != nil {
			continue
		}
		if _, dup := got[which[g-1]]; !dup {
			got[which[g-1]] = r
		}
	}
	var failed []int
	for _, gi := range which {
		if _, ok := got[gi]; !ok {
			failed = append(failed, gi)
		}
	}
	return got, failed
}

// twoPass: names (one retry for missing boxes), group, details per group
// (one retry for missing groups).
func (e *Engine) twoPass(ctx context.Context, inv *Inventory, ph *photo, named []Instance, mode Mode) error {
	p := ph.prepared()
	inv.PrepW, inv.PrepH = p.W, p.H
	var full []byte // the pass-1 image with every mark: pass 2 reuses it (cache)
	render := func(sub []Instance) ([]byte, error) {
		img := p.Img
		if e.Marks {
			img = DrawMarks(p, sub)
		}
		jb, err := JPEG(img)
		if err == nil && e.Sent != nil {
			e.Sent(fmt.Sprintf("names-%d", len(sub)), jb)
		}
		return jb, err
	}
	names, failed := e.namePass(ctx, inv, func(sub []Instance) ([]byte, error) {
		jb, err := render(sub)
		full = jb
		return jb, err
	}, named, seq(len(named)), "names")
	if mode == ModeAuto && len(failed) > 0 && full != nil {
		more, still := e.namePass(ctx, inv, render, named, failed, "names-retry")
		for n, ne := range more {
			names[n] = ne
		}
		failed = still
	}
	for _, n := range failed {
		inv.Rejected = append(inv.Rejected, Rejection{Box: n, Reason: "no name in the call(s)"})
	}
	got := map[int]Item{}
	for n, ne := range names {
		if ne.notFood {
			inv.notListed(named[n-1], n, analyze.Result{Name: ne.name}, "names")
			continue
		}
		got[n] = Item{Result: analyze.Result{Name: ne.name}, Source: "two-pass", Members: []int{n}, sameAs: ne.sameAs}
	}
	groups := group(got, named, p)
	if len(groups) == 0 || full == nil {
		return ErrNoItem
	}
	det, gfailed := e.detailPass(ctx, inv, full, groups, named, seq0(len(groups)), "details")
	if len(gfailed) > 0 {
		more, still := e.detailPass(ctx, inv, full, groups, named, gfailed, "details-retry")
		for gi, r := range more {
			det[gi] = r
		}
		gfailed = still
	}
	for _, gi := range gfailed {
		for _, n := range groups[gi].Members {
			inv.Rejected = append(inv.Rejected, Rejection{Box: n, Reason: "no valid details for group " + groups[gi].Name})
		}
	}
	for gi := range groups {
		r, ok := det[gi]
		if !ok {
			continue
		}
		r.NumberOfServing = float64(groups[gi].Count)
		g := groups[gi]
		g.Result = r
		inv.Items = append(inv.Items, g)
	}
	if len(inv.Items) == 0 {
		return ErrNoItem
	}
	return nil
}

func seq0(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i
	}
	return s
}
