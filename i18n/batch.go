package i18n

import (
	"sort"
	"strings"
)

// BatchGroups puts the texts into groups for batch translation (L4k): the texts
// of one screen together (a text on more than one screen goes to the smallest
// one, the most specific), in the order of their first concept; the texts on no
// screen by their first concept. A group has at most max texts.
func BatchGroups(g GraphFile, msgs []Message, max int) [][]Message {
	if max < 1 {
		max = 1
	}
	size := map[string]int{}
	screens := map[string][]string{}
	concept := map[string]string{}
	for _, e := range g.Edges {
		switch e.Kind {
		case "on_screen":
			size[e.To]++
			screens[e.From] = append(screens[e.From], e.To)
		case "uses":
			if c, ok := concept[e.From]; !ok || e.To < c {
				concept[e.From] = e.To
			}
		}
	}
	home := func(id string) string {
		best := ""
		for _, s := range screens[id] {
			if best == "" || size[s] < size[best] || size[s] == size[best] && s < best {
				best = s
			}
		}
		if best != "" {
			return best
		}
		if c := concept[id]; c != "" {
			return c
		}
		return "other"
	}
	order := map[string]int{}
	by := map[string][]Message{}
	var names []string
	for i, m := range msgs {
		order[m.ID] = i
		h := home(m.ID)
		if _, ok := by[h]; !ok {
			names = append(names, h)
		}
		by[h] = append(by[h], m)
	}
	sort.Strings(names)
	var out [][]Message
	for _, n := range names {
		ms := by[n]
		sort.SliceStable(ms, func(a, b int) bool {
			ca, cb := concept[ms[a].ID], concept[ms[b].ID]
			if ca != cb {
				return strings.Compare(ca, cb) < 0
			}
			return order[ms[a].ID] < order[ms[b].ID]
		})
		for len(ms) > 0 { // equal parts of at most max
			parts := (len(ms) + max - 1) / max
			n := (len(ms) + parts - 1) / parts
			out = append(out, ms[:n])
			ms = ms[n:]
		}
	}
	return out
}
