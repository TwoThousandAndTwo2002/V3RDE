package i18n

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// The text graph (L4g). Nodes are the messages of en.json ("text"), the
// screens of the app ("screen") and the main ideas of the app ("concept").
// Edges say how two nodes depend on each other:
//
//	on_screen  text -> screen   the screen shows the text (from the ui code)
//	part_of    text -> text     the app joins the text into the other one (from the code)
//	uses       text -> concept  the English text names the concept (from concepts.json terms)
//	opens      text -> screen   the control opens that screen (concepts.json, by hand)
//	pair       text -> text     a pair or an opposite: Show/Hide, Log out/Continue (by hand)
//
// When a text changes, Walk finds the texts whose context or translation may
// have to change with it.

// Node is one node of the graph.
type Node struct {
	ID     string `json:"id"`
	Type   string `json:"type"` // text, screen or concept
	Label  string `json:"label"`
	Degree int    `json:"degree"`
}

// Edge is one edge of the graph.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// GraphFile is i18n/graph.json.
type GraphFile struct {
	Source Source `json:"source"`
	Nodes  []Node `json:"nodes"`
	Edges  []Edge `json:"edges"`
}

// Context is the hand-written context of one text (i18n/context.json), in
// ASD-STE100. EnHash is the hash of the English text that the context was
// written for: when the text changes, the context is stale.
type Context struct {
	Kind   string            `json:"kind"`
	Screen string            `json:"screen"`
	Does   string            `json:"does"`
	Values map[string]string `json:"values,omitempty"`
	EnHash string            `json:"en_hash"`
	Status string            `json:"status"` // draft or approved
}

// ContextFile is i18n/context.json.
type ContextFile struct {
	Note     string             `json:"note"`
	Contexts map[string]Context `json:"contexts"`
}

// Concept is one main idea of the app.
type Concept struct {
	About string   `json:"about"`
	Terms []string `json:"terms"` // English words that name it, matched as whole words
}

// ConceptFile is i18n/concepts.json: the concepts and the edges made by hand.
type ConceptFile struct {
	Note     string             `json:"note"`
	Concepts map[string]Concept `json:"concepts"`
	Opens    [][2]string        `json:"opens"` // text id, screen name
	Pairs    [][2]string        `json:"pairs"` // text id, text id
}

// EnHash is the first 12 hex digits of the sha256 of an English text.
func EnHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])[:12]
}

var reWord = regexp.MustCompile(`[A-Za-z][A-Za-z'-]*`)

// Screens finds, for each screen file (screens/X.tsx), the texts it shows:
// a words key it names (CAPTURE.retake), the texts of a function or a
// component it calls (postTitle(...), <Shutter>), the words keys in that
// function, and the keys that the function reads from a words object passed
// to it (timeText(l, RECEIVE) reads words.collected). One level only.
func Screens(files map[string]string, msgs []Message) (map[string][]string, error) {
	type fn struct {
		params []string
		body   []Token
		file   string
	}
	fns := map[string]fn{}
	toks := map[string][]Token{}
	for name, src := range files {
		if !strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".tsx") {
			continue
		}
		t, err := Lex(name, src)
		if err != nil {
			return nil, err
		}
		toks[name] = t
		for i := 0; i+2 < len(t); i++ {
			var fname string
			var open int
			switch {
			case t[i].Text == "function" && t[i+1].Kind == Ident:
				fname, open = t[i+1].Text, i+2
			case (t[i].Text == "const" || t[i].Text == "let") && t[i+1].Kind == Ident && t[i+2].Text == "=" && i+3 < len(t) && t[i+3].Text == "(":
				fname, open = t[i+1].Text, i+3
			default:
				continue
			}
			for open < len(t) && t[open].Text != "(" { // generics: function f<T>(
				open++
			}
			end := skip(t, open)
			if end < 0 {
				continue
			}
			var params []string
			depth := 0
			want := true
			for j := open + 1; j < end-1; j++ {
				switch {
				case t[j].Kind == Open:
					depth++
				case t[j].Kind == Close:
					depth--
				case depth == 0 && t[j].Text == ",":
					want = true
				case depth == 0 && want && t[j].Kind == Ident:
					params = append(params, t[j].Text)
					want = false
				}
			}
			// the body: the next {...} block, or the expression after =>;
			// a return type comes first: ): string { or ): { a: string } {
			b := end
			if b < len(t) && t[b].Text == ":" {
				b++
				if b < len(t) && t[b].Text == "{" {
					if e := skip(t, b); e > 0 {
						b = e
					}
				}
			}
			for b < len(t) && t[b].Text != "{" && t[b].Text != "=>" && t[b].Text != ";" {
				b++
			}
			if b < len(t) && t[b].Text == "=>" {
				b++
			}
			var body []Token
			if b < len(t) && t[b].Text == "{" {
				if e := skip(t, b); e > 0 {
					body = t[b:e]
				}
			} else {
				for e := b; e < len(t) && t[e].Text != ";" && t[e].Text != "," && t[e].Kind != Close; e++ {
					body = append(body, t[e])
				}
			}
			if _, dup := fns[fname]; !dup {
				fns[fname] = fn{params: params, body: body, file: name}
			}
		}
	}
	// messages by owner: a words key (CONST.key) or a function (stem.fn...)
	byKey := map[string][]string{}
	byFn := map[string][]string{}
	for _, m := range msgs {
		file := strings.SplitN(m.Where, ":", 2)[0]
		stem := strings.ReplaceAll(strings.TrimSuffix(strings.TrimSuffix(file, ".tsx"), ".ts"), "/", ".")
		parts := strings.Split(m.ID, ".")
		if isUpperName(parts[0]) {
			byKey[parts[0]+"."+parts[1]] = append(byKey[parts[0]+"."+parts[1]], m.ID)
			continue
		}
		if strings.HasPrefix(m.ID, stem+".") {
			rest := strings.Split(strings.TrimPrefix(m.ID, stem+"."), ".")[0]
			if f, ok := fns[rest]; ok && f.file == file {
				byFn[rest] = append(byFn[rest], m.ID)
			}
		}
	}
	keyRefs := func(t []Token) []string { // CONST . key
		var out []string
		for i := 0; i+2 < len(t); i++ {
			if t[i].Kind == Ident && isUpperName(t[i].Text) && (t[i+1].Text == "." || t[i+1].Text == "?.") && t[i+2].Kind == Ident {
				out = append(out, byKey[t[i].Text+"."+t[i+2].Text]...)
			}
		}
		return out
	}
	out := map[string][]string{}
	var names []string
	for name := range toks {
		if strings.HasPrefix(name, "screens/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		t := toks[name]
		seen := map[string]bool{}
		add := func(ids ...string) {
			for _, id := range ids {
				seen[id] = true
			}
		}
		add(keyRefs(t)...)
		for i := 0; i < len(t); i++ {
			if t[i].Kind != Ident {
				continue
			}
			call := i+1 < len(t) && t[i+1].Text == "(" && (i == 0 || t[i-1].Text != "function")
			comp := i > 0 && t[i-1].Text == "<"
			f, ok := fns[t[i].Text]
			// another screen's component is its own screen node, not part of this one
			if !ok || !(call || comp) || f.file == name || strings.HasPrefix(f.file, "screens/") {
				continue
			}
			add(byFn[t[i].Text]...)
			add(keyRefs(f.body)...)
			if !call {
				continue
			}
			// a words object passed as an argument: f(l, RECEIVE) -> words.collected in f
			end := skip(t, i+1)
			if end < 0 {
				continue
			}
			arg, depth := 0, 0
			for j := i + 2; j < end-1; j++ {
				switch {
				case t[j].Kind == Open:
					depth++
				case t[j].Kind == Close:
					depth--
				case depth == 0 && t[j].Text == ",":
					arg++
				case depth == 0 && t[j].Kind == Ident && isUpperName(t[j].Text) && arg < len(f.params) &&
					(t[j+1].Text == "," || t[j+1].Text == ")") && (t[j-1].Text == "," || t[j-1].Text == "("):
					p := f.params[arg]
					for k := 0; k+2 < len(f.body); k++ {
						if f.body[k].Text == p && f.body[k+1].Text == "." && f.body[k+2].Kind == Ident {
							add(byKey[t[j].Text+"."+f.body[k+2].Text]...)
						}
					}
				}
			}
		}
		screen := strings.TrimSuffix(path.Base(name), ".tsx")
		for id := range seen {
			out[screen] = append(out[screen], id)
		}
		sort.Strings(out[screen])
	}
	return out, nil
}

// partOf: in one function, a text that the app joins into another one. The
// joined line has a placeholder named after the call ({plural}) or a pN
// placeholder (an expression); its parts are the other texts of the function.
func partOf(msgs []Message) []Edge {
	owner := func(m Message) string {
		p := strings.Split(m.ID, ".")
		if isUpperName(p[0]) || len(p) < 3 {
			return ""
		}
		return p[0] + "." + p[1]
	}
	by := map[string][]Message{}
	var order []string
	for _, m := range msgs {
		o := owner(m)
		if o == "" {
			continue
		}
		if _, ok := by[o]; !ok {
			order = append(order, o)
		}
		by[o] = append(by[o], m)
	}
	var out []Edge
	for _, o := range order {
		ms := by[o]
		for _, parent := range ms {
			for _, part := range ms {
				if part.ID == parent.ID {
					continue
				}
				joins := part.Arg != "" && strings.Contains(parent.Text, "{"+part.Arg+"}")
				expr := part.Arg == "" && regexp.MustCompile(`\{p\d+\}`).MatchString(parent.Text) && !regexp.MustCompile(`\{p\d+\}`).MatchString(part.Text)
				if joins || expr {
					out = append(out, Edge{From: part.ID, To: parent.ID, Kind: "part_of"})
				}
			}
		}
	}
	return out
}

// Uses: the concepts whose terms are whole words of the English text (placeholders removed).
func Uses(text string, concepts map[string]Concept) []string {
	words := map[string]bool{}
	for _, w := range reWord.FindAllString(reParam.ReplaceAllString(text, " "), -1) {
		words[strings.ToLower(w)] = true
	}
	var out []string
	for name, c := range concepts {
		for _, t := range c.Terms {
			ok := true
			for _, w := range strings.Fields(strings.ToLower(t)) {
				if !words[w] {
					ok = false
				}
			}
			if ok {
				out = append(out, name)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// BuildGraph makes the graph. It fails on a hand-made edge or concept that names nothing.
func BuildGraph(src Source, msgs []Message, screens map[string][]string, cf ConceptFile) (GraphFile, error) {
	g := GraphFile{Source: src}
	ids := map[string]bool{}
	for _, m := range msgs {
		ids[m.ID] = true
	}
	var nodes []Node
	for _, m := range msgs {
		nodes = append(nodes, Node{ID: m.ID, Type: "text", Label: m.Text})
	}
	var snames []string
	for s := range screens {
		snames = append(snames, s)
	}
	sort.Strings(snames)
	for _, s := range snames {
		nodes = append(nodes, Node{ID: "screen:" + s, Type: "screen", Label: s})
	}
	var cnames []string
	for c := range cf.Concepts {
		cnames = append(cnames, c)
	}
	sort.Strings(cnames)
	for _, c := range cnames {
		nodes = append(nodes, Node{ID: "concept:" + c, Type: "concept", Label: c})
	}
	var edges []Edge
	for _, s := range snames {
		for _, id := range screens[s] {
			edges = append(edges, Edge{From: id, To: "screen:" + s, Kind: "on_screen"})
		}
	}
	edges = append(edges, partOf(msgs)...)
	used := map[string]int{}
	for _, m := range msgs {
		for _, c := range Uses(m.Text, cf.Concepts) {
			edges = append(edges, Edge{From: m.ID, To: "concept:" + c, Kind: "uses"})
			used[c]++
		}
	}
	for _, c := range cnames {
		if used[c] == 0 {
			return g, fmt.Errorf("concepts.json: concept %q matches no text", c)
		}
	}
	for _, o := range cf.Opens {
		if !ids[o[0]] || screens[o[1]] == nil {
			return g, fmt.Errorf("concepts.json: opens %v names no text or no screen", o)
		}
		edges = append(edges, Edge{From: o[0], To: "screen:" + o[1], Kind: "opens"})
	}
	for _, p := range cf.Pairs {
		if !ids[p[0]] || !ids[p[1]] {
			return g, fmt.Errorf("concepts.json: pair %v names no text", p)
		}
		edges = append(edges, Edge{From: p[0], To: p[1], Kind: "pair"})
	}
	deg := map[string]int{}
	for _, e := range edges {
		deg[e.From]++
		deg[e.To]++
	}
	for i := range nodes {
		nodes[i].Degree = deg[nodes[i].ID]
	}
	g.Nodes, g.Edges = nodes, edges
	return g, nil
}

// Stale: the texts with no context, or whose English changed after the context
// was written (en_hash), and the contexts of texts that are gone.
func Stale(msgs []Message, cf ContextFile) (missing, changed, gone []string) {
	ids := map[string]bool{}
	for _, m := range msgs {
		ids[m.ID] = true
		c, ok := cf.Contexts[m.ID]
		switch {
		case !ok:
			missing = append(missing, m.ID)
		case c.EnHash != EnHash(m.Text):
			changed = append(changed, m.ID)
		}
	}
	for id := range cf.Contexts {
		if !ids[id] {
			gone = append(gone, id)
		}
	}
	sort.Strings(gone)
	return
}

// Reached is one text that a change reaches, and how.
type Reached struct {
	ID     string `json:"id"`
	Degree int    `json:"degree"`
	Via    string `json:"via"` // the start text and the edge kinds, for example "ROLE.logout pair"
}

// Walk starts at the changed texts and returns the texts whose context or
// translation may have to change: the texts on the same screen, the texts
// joined with them, the pairs, and the texts of the same concept (one hop
// through a screen or a concept node). Most connected first.
func Walk(g GraphFile, start []string) []Reached {
	deg := map[string]int{}
	for _, n := range g.Nodes {
		deg[n.ID] = n.Degree
	}
	adj := map[string][]Edge{}
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e)
		adj[e.To] = append(adj[e.To], Edge{From: e.To, To: e.From, Kind: e.Kind})
	}
	isText := func(id string) bool { return !strings.HasPrefix(id, "screen:") && !strings.HasPrefix(id, "concept:") }
	got := map[string]Reached{}
	starts := map[string]bool{}
	for _, s := range start {
		starts[s] = true
	}
	// direct edges first (part_of, pair), then one hop through a screen or a concept
	for _, s := range start {
		for _, e := range adj[s] {
			if isText(e.To) && !starts[e.To] {
				if _, ok := got[e.To]; !ok {
					got[e.To] = Reached{ID: e.To, Degree: deg[e.To], Via: s + " " + e.Kind}
				}
			}
		}
	}
	for _, s := range start {
		for _, e := range adj[s] {
			if isText(e.To) {
				continue
			}
			for _, e2 := range adj[e.To] {
				if isText(e2.To) && !starts[e2.To] {
					if _, ok := got[e2.To]; !ok {
						got[e2.To] = Reached{ID: e2.To, Degree: deg[e2.To], Via: s + " " + e.Kind + " " + strings.SplitN(e.To, ":", 2)[1] + " " + e2.Kind}
					}
				}
			}
		}
	}
	var out []Reached
	for _, r := range got {
		out = append(out, r)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Degree != out[b].Degree {
			return out[a].Degree > out[b].Degree
		}
		return out[a].ID < out[b].ID
	})
	return out
}

// Spread returns the texts that a corrected translation of id must agree with
// (L4j): its pairs and joined parts, and the texts of the same concept. Texts
// that are only on the same screen are not returned: the screen judge sees them.
func Spread(g GraphFile, id string) []Reached {
	var out []Reached
	for _, r := range Walk(g, []string{id}) {
		f := strings.Fields(r.Via) // "<id> pair" or "<id> uses <concept> uses"
		if len(f) == 2 && (f[1] == "pair" || f[1] == "part_of") || len(f) == 4 && f[1] == "uses" && f[3] == "uses" {
			out = append(out, r)
		}
	}
	return out
}

// Impact is what a new version of the app text changes (cmd/graph).
type Impact struct {
	Added   []string  `json:"added"`
	Removed []string  `json:"removed"`
	Changed []string  `json:"changed"`
	Reached []Reached `json:"reached"` // to review: their context or translation may change too
}

// ImpactOf compares the old and the new messages: the removed and changed texts
// are walked in the old graph, the added and changed texts in the new graph.
func ImpactOf(oldMsgs, newMsgs []Message, oldG, newG GraphFile) Impact {
	old := map[string]string{}
	for _, m := range oldMsgs {
		old[m.ID] = m.Text
	}
	cur := map[string]string{}
	var im Impact
	for _, m := range newMsgs {
		cur[m.ID] = m.Text
		t, ok := old[m.ID]
		switch {
		case !ok:
			im.Added = append(im.Added, m.ID)
		case t != m.Text:
			im.Changed = append(im.Changed, m.ID)
		}
	}
	for _, m := range oldMsgs {
		if _, ok := cur[m.ID]; !ok {
			im.Removed = append(im.Removed, m.ID)
		}
	}
	start := map[string]bool{}
	for _, l := range [][]string{im.Added, im.Removed, im.Changed} {
		for _, id := range l {
			start[id] = true
		}
	}
	got := map[string]Reached{}
	for _, r := range append(Walk(oldG, append(append([]string{}, im.Removed...), im.Changed...)), Walk(newG, append(append([]string{}, im.Added...), im.Changed...))...) {
		if _, here := cur[r.ID]; !here || start[r.ID] {
			continue // a removed text needs no review; a start is listed above
		}
		if p, ok := got[r.ID]; !ok || r.Degree > p.Degree {
			got[r.ID] = r
		}
	}
	for _, r := range got {
		im.Reached = append(im.Reached, r)
	}
	sort.Slice(im.Reached, func(a, b int) bool {
		if im.Reached[a].Degree != im.Reached[b].Degree {
			return im.Reached[a].Degree > im.Reached[b].Degree
		}
		return im.Reached[a].ID < im.Reached[b].ID
	})
	return im
}
