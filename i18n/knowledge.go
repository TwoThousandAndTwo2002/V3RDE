package i18n

import (
	"fmt"
	"sort"
	"strings"
)

// Knowledge is what the graph and the contexts tell about each text (L4h). Its
// Block is the context that goes into a prompt: what the text does, its values,
// its pairs, the line it is joined into, the texts near it on its screen, and
// the earlier translations of the texts that name the same idea.
type Knowledge struct {
	text     map[string]string
	order    map[string]int
	ctx      map[string]Context
	adj      map[string][]Edge
	concepts map[string][]string // concept -> text ids, in en.json order
}

// NewKnowledge reads the graph and the contexts.
func NewKnowledge(msgs []Message, g GraphFile, cf ContextFile) *Knowledge {
	k := &Knowledge{text: map[string]string{}, order: map[string]int{}, ctx: cf.Contexts, adj: map[string][]Edge{}, concepts: map[string][]string{}}
	for i, m := range msgs {
		k.text[m.ID], k.order[m.ID] = m.Text, i
	}
	for _, e := range g.Edges {
		k.adj[e.From] = append(k.adj[e.From], e)
		k.adj[e.To] = append(k.adj[e.To], Edge{From: e.To, To: e.From, Kind: e.Kind + "<"})
		if e.Kind == "uses" {
			c := strings.TrimPrefix(e.To, "concept:")
			k.concepts[c] = append(k.concepts[c], e.From)
		}
	}
	for c := range k.concepts {
		ids := k.concepts[c]
		sort.Slice(ids, func(a, b int) bool { return k.order[ids[a]] < k.order[ids[b]] })
	}
	return k
}

// Key is the dedupe key of a text: the English text and its context (kind and
// what it does). The same English with another context is another text.
func (k *Knowledge) Key(id string) string {
	c := k.ctx[id]
	return k.text[id] + "\x00" + c.Kind + "\x00" + c.Does
}

// plain: other texts in the block show their values as [value], so that no
// {name} of another text is copied into the translation (Restore rejects it).
func plain(s string) string { return reParam.ReplaceAllString(s, "[value]") }

func (k *Knowledge) short(id string) string {
	c := k.ctx[id]
	s := fmt.Sprintf("%q", plain(k.text[id]))
	if c.Kind != "" {
		s += " (" + c.Kind + ")"
	}
	return s
}

// Block is the context of id for a prompt. tags maps a placeholder to its tag in
// the masked text ("{plural}" -> "<x1/>"); prev has earlier translations by id
// (may be nil). Limits keep the block short: 3 pairs, 8 screen texts, 6 per concept.
func (k *Knowledge) Block(id string, tags map[string]string, prev map[string]string) string {
	c, ok := k.ctx[id]
	if !ok {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Kind: %s. Screen: %s.\n", c.Kind, c.Screen)
	does := c.Does
	for ph, tag := range tags { // the prompt shows tags, not {names}
		does = strings.ReplaceAll(does, ph, tag)
	}
	fmt.Fprintf(&b, "What it does: %s\n", does)
	if len(c.Values) > 0 {
		var names []string
		for v := range c.Values {
			names = append(names, v)
		}
		sort.Strings(names)
		var vs []string
		for _, v := range names {
			t := tags["{"+v+"}"]
			if t == "" {
				t = "{" + v + "}"
			}
			vs = append(vs, t+" is "+c.Values[v])
		}
		fmt.Fprintf(&b, "Values: %s\n", strings.Join(vs, "; "))
	}
	var pairs, into, parts []string
	screens := map[string]bool{}
	for _, e := range k.adj[id] {
		switch e.Kind {
		case "pair", "pair<":
			if len(pairs) < 3 {
				pairs = append(pairs, k.short(e.To)+": "+plain(k.ctx[e.To].Does))
			}
		case "part_of":
			into = append(into, fmt.Sprintf("%q", plain(k.text[e.To])))
		case "part_of<":
			parts = append(parts, fmt.Sprintf("%q", plain(k.text[e.To])))
		case "on_screen":
			screens[e.To] = true
		}
	}
	if len(pairs) > 0 {
		fmt.Fprintf(&b, "Paired with: %s\n", strings.Join(pairs, " | "))
	}
	if len(into) > 0 {
		fmt.Fprintf(&b, "The app joins this text into: %s\n", strings.Join(into, ", "))
	}
	if len(parts) > 0 {
		fmt.Fprintf(&b, "The app puts these texts into it: %s\n", strings.Join(parts, ", "))
	}
	// near: the controls and titles of the same screens, the same sheet (same
	// Screen text) first; error lines and joined parts only from the same sheet;
	// one entry for one English text
	seen := map[string]bool{id: true}
	seenText := map[string]bool{k.text[id]: true}
	var near []string
	for s := range screens {
		for _, e := range k.adj[s] {
			n := e.To
			if e.Kind != "on_screen<" || seen[n] || seenText[k.text[n]] {
				continue
			}
			nc := k.ctx[n]
			if (nc.Kind == "Error line" || nc.Kind == "Part of a joined line") && nc.Screen != c.Screen {
				continue
			}
			seen[n], seenText[k.text[n]] = true, true
			near = append(near, n)
		}
	}
	rank := func(n string) int {
		r := 0
		if k.ctx[n].Screen != c.Screen {
			r += 10
		}
		switch k.ctx[n].Kind {
		case "Button", "Title", "Link", "Field label", "Card title and tab":
		case "Error line", "Part of a joined line":
			r += 2
		default:
			r++
		}
		return r
	}
	sort.Slice(near, func(a, b int) bool {
		if ra, rb := rank(near[a]), rank(near[b]); ra != rb {
			return ra < rb
		}
		return k.order[near[a]] < k.order[near[b]]
	})
	if len(near) > 8 {
		near = near[:8]
	}
	if len(near) > 0 {
		var ns []string
		for _, n := range near {
			ns = append(ns, k.short(n))
		}
		fmt.Fprintf(&b, "Near it on the screen: %s\n", strings.Join(ns, ", "))
	}
	// the same idea: earlier translations keep one word for one idea
	var cs []string
	for _, e := range k.adj[id] {
		if e.Kind == "uses" {
			cs = append(cs, strings.TrimPrefix(e.To, "concept:"))
		}
	}
	sort.Strings(cs)
	for _, cn := range cs {
		var ex []string
		for _, o := range k.concepts[cn] {
			if o == id || len(ex) >= 6 {
				continue
			}
			if t, ok := prev[o]; ok && t != "" {
				ex = append(ex, fmt.Sprintf("%q = %q", plain(k.text[o]), plain(t)))
			}
		}
		if len(ex) > 0 {
			fmt.Fprintf(&b, "Same idea (%s), earlier translations: %s\n", cn, strings.Join(ex, "; "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// StripParams removes the {name} placeholders of a text.
func StripParams(s string) string { return reParam.ReplaceAllString(s, "") }
