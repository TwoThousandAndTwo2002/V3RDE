package i18n

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Message is one line of app text, as en.json keeps it.
type Message struct {
	ID     string   `json:"id"`
	Text   string   `json:"text"`             // {name} marks a value the app puts in
	Params []string `json:"params,omitempty"` // the {name}s, in order
	Where  string   `json:"where"`            // file:line in mobile/src
	Arg    string   `json:"arg,omitempty"`    // the function the text goes to, when it is an argument (plural)
}

// Skipped is one literal that is not app text, and why (skipped.json).
type Skipped struct {
	Where string `json:"where"`
	Text  string `json:"text"`
	Rule  string `json:"rule"`
}

// Lists are the hand-made decisions, in i18n/rules.json. Each entry names a
// file and the exact text; an entry with no text covers the whole file.
type Lists struct {
	Text    []Entry `json:"text"`     // text that a rule would skip
	NotText []Entry `json:"not_text"` // code that has the shape of text
}

// Entry is one hand-made decision.
type Entry struct {
	File string `json:"file"`
	Text string `json:"text,omitempty"`
	Why  string `json:"why"`
}

// Props whose string is code, not text (a value on a JSX element or an object key).
var codeProps = set("testID", "glyph", "kind", "accessibilityRole", "autoCapitalize", "autoComplete", "keyboardType",
	"inputMode", "returnKeyType", "submitBehavior", "textContentType", "pointerEvents", "importantForAccessibility",
	"accessibilityLiveRegion", "live", "keyboardShouldPersistTaps", "resizeMode", "animationType", "round", "fill", "strokeLinecap", "strokeLinejoin",
	"nativeID", "controls", "role", "mode", "source", "status", "type", "name", "encoding",
	"format", "credentials", "method", "alignItems", "alignSelf", "justifyContent", "flexDirection", "flexWrap",
	"position", "overflow", "textAlign", "textDecorationLine", "outlineStyle", "blurTint", "fontVariant", "chosen", "code",
	"state", "behavior", "facing", "barcodeTypes", "mediaTypes", "fontFamily", "viewBox", "d", "transform", "width", "height")

// keywords cannot be an operand (from, as, of, in and type can be names, so they are not here)
var keywords = set("return", "typeof", "new", "case", "else", "void", "delete", "throw", "await", "yield",
	"if", "for", "while", "const", "let", "var", "function", "export", "import", "instanceof")

// calls whose string argument is code (a selector, a canvas kind, an event name)
var codeCalls = set("querySelector", "querySelectorAll", "getContext", "addEventListener", "removeEventListener",
	"getElementById", "createElement", "open", "startsWith", "endsWith", "includes", "getItem", "setItem", "removeItem")

var (
	reCodeWord = regexp.MustCompile(`^[a-z][A-Za-z0-9]*([_\-./:@?=&+][A-Za-z0-9]*)*$|^[/?&#.:_-][\x21-\x7e]*$|^[A-Za-z][A-Za-z0-9]*([_-][A-Za-z0-9]+)+$`)
	reSVG      = regexp.MustCompile(`^[Mm][0-9 .,\-MmLlHhVvCcSsQqTtAaZz]+$`)
	reColor    = regexp.MustCompile(`^#[0-9A-Fa-f]{3,8}$|^rgba?\(`)
	reURL      = regexp.MustCompile(`^[a-z][a-z0-9+.-]*:(//)?\S*$`)
	reMethod   = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE)$`)
	reParam    = regexp.MustCompile(`\{([A-Za-z_$][A-Za-z0-9_$]*)\}`)
	reSlug     = regexp.MustCompile(`[a-z0-9]+`)
)

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// hasLetter: a letter outside the {name} placeholders.
func hasLetter(s string) bool {
	for _, r := range reParam.ReplaceAllString(s, "") {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func isUpperName(s string) bool {
	return s != "" && strings.ToUpper(s) == s && hasLetter(s)
}

// frame is one open bracket while the file is read.
type frame struct {
	kind   string // "obj" (const NAME = {), "fn" (function NAME / const NAME = () => {), or ""
	name   string
	open   string
	curKey string // obj: the key whose value is being read
}

// File reads the tokens of one file: its messages and the literals that are not text.
type fileState struct {
	file   string
	stem   string
	lists  *Lists
	msgs   []Message
	skips  []Skipped
	owners []string // the owner of each message, before ids are made
	seen   int      // literals that a chain, a skip or an object key took
}

// owner is the name a message gets: OBJ.KEY for a value in a named object,
// stem.fn in a function, stem at the top.
func (f *fileState) owner(stack []frame) string {
	for k := len(stack) - 1; k >= 0; k-- {
		fr := stack[k]
		if fr.kind == "obj" && fr.curKey != "" {
			var p []string
			for _, o := range stack[:k+1] {
				if o.kind == "obj" {
					p = append(p, o.name)
				}
			}
			return strings.Join(p, ".") + "." + fr.curKey
		}
		if fr.kind == "fn" {
			return f.stem + "." + fr.name
		}
	}
	return f.stem
}

func (f *fileState) listed(l []Entry, text string) (string, bool) {
	for _, e := range l {
		if e.File == f.file && (e.Text == "" || e.Text == text) {
			return e.Why, true
		}
	}
	return "", false
}

// rule says why a literal is not text, or "" when it is text.
func (f *fileState) rule(toks []Token, i int, inUpperObj bool, call string) string {
	t := toks[i]
	at := func(k int) string {
		if k >= 0 && k < len(toks) {
			return toks[k].Text
		}
		return ""
	}
	if _, ok := f.listed(f.lists.Text, t.Text); ok {
		return ""
	}
	if why, ok := f.listed(f.lists.NotText, t.Text); ok {
		return "listed: " + why
	}
	prev, prev2, prev3, next := at(i-1), at(i-2), at(i-3), at(i+1)
	switch {
	case prev == "from" || prev == "import" || (prev == "(" && (prev2 == "require" || prev2 == "import")):
		return "import"
	case !hasLetter(t.Text):
		return "no letters"
	case prev == "===" || prev == "!==" || prev == "==" || prev == "!=" || prev == "case" || next == "===" || next == "!==":
		return "compared"
	case (prev == "=" || prev == ":") && codeProps[prev2]:
		return "code prop " + prev2
	case prev == "{" && prev2 == "=" && codeProps[prev3]:
		return "code prop " + prev3
	case codeCalls[call]:
		return "code call " + call
	case inUpperObj:
		return "" // a value in a words object (ROLE, SIGN_IN, ...) is text, whatever its shape
	case reSVG.MatchString(t.Text):
		return "svg path"
	case reColor.MatchString(t.Text):
		return "color"
	case reMethod.MatchString(t.Text):
		return "http method"
	case reURL.MatchString(t.Text) && !strings.Contains(t.Text, " "):
		return "url"
	case reCodeWord.MatchString(t.Text):
		return "code word"
	}
	return ""
}

// operand returns the end of the operand that starts at i, or -1.
func operand(toks []Token, i int) int {
	if i >= len(toks) {
		return -1
	}
	t := toks[i]
	end := -1
	switch t.Kind {
	case Str, Num, Regex:
		return i + 1
	case Ident:
		if keywords[t.Text] {
			return -1
		}
		end = i + 1
	case Open:
		if t.Text != "(" {
			return -1
		}
		end = skip(toks, i)
	default:
		return -1
	}
	for end > 0 && end < len(toks) {
		switch n := toks[end]; {
		case (n.Text == "." || n.Text == "?.") && end+1 < len(toks) && toks[end+1].Kind == Ident:
			end += 2
		case n.Text == "(" || n.Text == "[":
			end = skip(toks, end)
		case n.Text == "!" && end+1 < len(toks) && (toks[end+1].Text == "." || toks[end+1].Kind == Close):
			end++
		default:
			return end
		}
	}
	return end
}

// skip returns the index after the bracket that closes toks[i].
func skip(toks []Token, i int) int {
	d := 0
	for j := i; j < len(toks); j++ {
		if toks[j].Kind == Open {
			d++
		} else if toks[j].Kind == Close {
			d--
			if d == 0 {
				return j + 1
			}
		}
	}
	return -1
}

// walk reads one token list (a file, or one ${...} of a template).
func (f *fileState) walk(toks []Token, stack []frame) {
	pendingFn, bodyFn := "", ""
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		at := func(k int) Token {
			if k >= 0 && k < len(toks) {
				return toks[k]
			}
			return Token{}
		}
		switch {
		case t.Kind == Ident && t.Text == "function" && at(i+1).Kind == Ident:
			pendingFn = at(i + 1).Text
		case t.Kind == Op && t.Text == "=>":
			// const NAME = (...) => { ... }: the body is the function NAME
			if at(i+1).Text == "{" {
				for j := i - 1; j >= 1 && j >= i-40; j-- {
					if toks[j].Text == "=" && toks[j-1].Kind == Ident && j >= 2 && (toks[j-2].Text == "const" || toks[j-2].Text == "let") {
						bodyFn = toks[j-1].Text
						break
					}
					if toks[j].Text == ";" || toks[j].Text == "{" || toks[j].Text == "}" && j < i-1 {
						break
					}
				}
			}
		case t.Kind == Open:
			fr := frame{open: t.Text}
			switch {
			case t.Text == "{" && constName(toks, i) != "":
				fr.kind, fr.name = "obj", constName(toks, i)
			case t.Text == "{" && at(i-1).Text == ":" && len(stack) > 0 && stack[len(stack)-1].kind == "obj" && stack[len(stack)-1].curKey != "":
				fr.kind, fr.name = "obj", stack[len(stack)-1].curKey
			case t.Text == "(" && pendingFn != "":
				fr.kind, fr.name = "fn", pendingFn
				pendingFn = ""
			case t.Text == "{" && bodyFn != "" && at(i-1).Text != ":": // ): { a: T } { -- the first {} is the return type
				fr.kind, fr.name = "fn", bodyFn
				bodyFn = ""
			}
			// a chain can start with "(": (a ? b : c) + " text" (L4g: such literals were lost)
			// (not a call: f( or x[0]( or a)( continue the operand before them)
			if pv := at(i - 1); t.Text == "(" && pv.Text != "+" && pv.Text != "." && pv.Text != "?." &&
				pv.Kind != Close && pv.Kind != Str && pv.Kind != Num && (pv.Kind != Ident || keywords[pv.Text]) {
				f.chain(toks, i, stack)
			}
			stack = append(stack, fr)
			continue
		case t.Kind == Close:
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.kind == "fn" && top.open == "(" {
					bodyFn = top.name // the next "{" is the body of this function
				}
			}
			continue
		}
		// an object key: { KEY: or , KEY:
		if len(stack) > 0 && stack[len(stack)-1].kind == "obj" {
			top := &stack[len(stack)-1]
			if (t.Kind == Ident || t.Kind == Num || t.Kind == Str) && at(i+1).Text == ":" && (at(i-1).Text == "{" || at(i-1).Text == ",") {
				top.curKey = t.Text
				if t.Kind == Str {
					top.curKey = "" // a quoted key: not a words key
					f.seen++
				}
				i++ // the ':'
				continue
			}
			if t.Text == "," {
				top.curKey = ""
				continue
			}
		}
		// a chain starts here when the token before it is not '+' or '.'
		if p := at(i - 1).Text; p == "+" || p == "." || p == "?." {
			continue
		}
		f.chain(toks, i, stack)
	}
}

// chain reads a + b + "c" ... that starts at i, and records a message when a literal in it is text.
func (f *fileState) chain(toks []Token, i int, stack []frame) {
	var parts [][2]int
	j := i
	for {
		e := operand(toks, j)
		if e < 0 {
			break
		}
		parts = append(parts, [2]int{j, e})
		if e < len(toks) && toks[e].Text == "+" {
			j = e + 1
			continue
		}
		break
	}
	upper := false
	for _, fr := range stack {
		if fr.kind == "obj" && isUpperName(fr.name) {
			upper = true
		}
	}
	hasStr, text := false, false
	for _, p := range parts {
		if p[1]-p[0] == 1 && toks[p[0]].Kind == Str {
			hasStr = true
			f.seen++
			lit := toks[p[0]]
			if lit.Tmpl {
				for _, part := range lit.Parts {
					f.walk(part, append([]frame(nil), stack...))
				}
			}
			why := f.rule(toks, p[0], upper, callOf(toks, i))
			if why == "" {
				text = true
			} else if why != "import" {
				f.skips = append(f.skips, Skipped{Where: fmt.Sprintf("%s:%d", f.file, lit.Line), Text: lit.Text, Rule: why})
			}
		}
	}
	if !hasStr || !text {
		return
	}
	var b strings.Builder
	names := map[string]string{} // expression -> name
	used := map[string]bool{}
	for k, p := range parts {
		if p[1]-p[0] == 1 && toks[p[0]].Kind == Str {
			b.WriteString(toks[p[0]].Text)
			for _, m := range reParam.FindAllStringSubmatch(toks[p[0]].Text, -1) {
				used[m[1]] = true
			}
			continue
		}
		src := srcOf(toks[p[0]:p[1]])
		n, ok := names[src]
		if !ok {
			base := ParamName(toks[p[0]:p[1]], k+1)
			n = base
			for c := 2; used[n]; c++ {
				n = fmt.Sprintf("%s_%d", base, c)
			}
			names[src], used[n] = n, true
		}
		b.WriteString("{" + n + "}")
	}
	m := Message{Text: b.String(), Where: fmt.Sprintf("%s:%d", f.file, toks[i].Line)}
	seen := map[string]bool{}
	for _, x := range reParam.FindAllStringSubmatch(m.Text, -1) {
		if !seen[x[1]] {
			seen[x[1]] = true
			m.Params = append(m.Params, x[1])
		}
	}
	m.Arg = callOf(toks, i)
	f.msgs = append(f.msgs, m)
	f.owners = append(f.owners, f.owner(stack))
}

// callOf: the function a chain at i is an argument of (f(a, "text") gives f), or "".
func callOf(toks []Token, i int) string {
	d := 0
	for k := i - 1; k >= 0; k-- {
		if toks[k].Kind == Close {
			d++
		} else if toks[k].Kind == Open {
			if d == 0 {
				if toks[k].Text == "(" && k > 0 && toks[k-1].Kind == Ident && !keywords[toks[k-1].Text] {
					return toks[k-1].Text
				}
				return ""
			}
			d--
		} else if d == 0 && (toks[k].Text == ";" || toks[k].Text == "=" || toks[k].Text == "=>" || toks[k].Text == "?" || toks[k].Text == ":" || toks[k].Text == "return") {
			return ""
		}
	}
	return ""
}

// constName: for the "{" at i after "=", the NAME of `const NAME = {` or `const NAME: Type = {`.
func constName(toks []Token, i int) string {
	if i < 2 || toks[i-1].Text != "=" {
		return ""
	}
	for j := i - 2; j >= 1 && j >= i-16; j-- {
		t := toks[j].Text
		if t == ";" || t == "{" || t == "}" || t == "=" {
			return ""
		}
		if toks[j].Kind == Ident && (toks[j-1].Text == "const" || toks[j-1].Text == "let") && (j == i-2 || toks[j+1].Text == ":") {
			return toks[j].Text
		}
	}
	return ""
}

func srcOf(toks []Token) string {
	var p []string
	for _, t := range toks {
		p = append(p, t.Text)
	}
	return strings.Join(p, " ")
}

func slug(text string) string {
	w := reSlug.FindAllString(strings.ToLower(reParam.ReplaceAllString(text, " ")), -1)
	if len(w) > 4 {
		w = w[:4]
	}
	if len(w) == 0 {
		return "x"
	}
	return strings.Join(w, "_")
}

// File is the result of Extract for one file.
type File struct {
	Messages []Message
	Skipped  []Skipped
}

// Extract reads one file. stem names its messages outside a words object.
func Extract(file, src string, lists *Lists) (File, error) {
	toks, err := Lex(file, src)
	if err != nil {
		return File{}, err
	}
	stem := strings.TrimSuffix(strings.TrimSuffix(file, ".tsx"), ".ts")
	stem = strings.ReplaceAll(stem, "/", ".")
	f := &fileState{file: file, stem: stem, lists: lists}
	f.walk(toks, nil)
	// every literal must be in a message, in skipped.json, or be an object key (L4g)
	if total := countStr(toks); f.seen != total {
		return File{}, fmt.Errorf("%s: %d of %d literals are not in a message or a skip", file, total-f.seen, total)
	}
	// ids: the owner; owners with more than one message add a slug of the text
	count := map[string]int{}
	for _, o := range f.owners {
		count[o]++
	}
	used := map[string]bool{}
	for k := range f.msgs {
		id := f.owners[k]
		if count[id] > 1 {
			id += "." + slug(f.msgs[k].Text)
		}
		base := id
		for c := 2; used[id]; c++ {
			id = fmt.Sprintf("%s_%d", base, c)
		}
		used[id] = true
		f.msgs[k].ID = id
	}
	return File{Messages: f.msgs, Skipped: f.skips}, nil
}

// Source is the app code the text comes from (i18n/source.json).
type Source struct {
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	Dir    string `json:"dir"`
}

// All reads every .ts and .tsx file (files maps a path under Dir to its text).
// It fails when two messages get the same id, or a hand-made entry matches no literal.
func All(files map[string]string, lists *Lists) ([]Message, []Skipped, error) {
	var names []string
	for n := range files {
		if strings.HasSuffix(n, ".ts") || strings.HasSuffix(n, ".tsx") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var msgs []Message
	var skips []Skipped
	ids := map[string]string{}
	for _, n := range names {
		r, err := Extract(n, files[n], lists)
		if err != nil {
			return nil, nil, err
		}
		for _, m := range r.Messages {
			if w, dup := ids[m.ID]; dup {
				return nil, nil, fmt.Errorf("id %s is in %s and %s", m.ID, w, m.Where)
			}
			ids[m.ID] = m.Where
		}
		msgs = append(msgs, r.Messages...)
		skips = append(skips, r.Skipped...)
	}
	// every hand-made entry must match a literal (no stale entries)
	for _, l := range [][]Entry{lists.Text, lists.NotText} {
		for _, e := range l {
			src, ok := files[e.File]
			if !ok {
				return nil, nil, fmt.Errorf("rules.json names %s, which is not in the app", e.File)
			}
			if e.Text == "" {
				continue
			}
			found := false
			toks, _ := Lex(e.File, src)
			var look func([]Token)
			look = func(ts []Token) {
				for _, t := range ts {
					if t.Kind == Str && t.Text == e.Text {
						found = true
					}
					for _, p := range t.Parts {
						look(p)
					}
				}
			}
			look(toks)
			if !found {
				return nil, nil, fmt.Errorf("rules.json: %q is not a literal in %s", e.Text, e.File)
			}
		}
	}
	return msgs, skips, nil
}

func countStr(toks []Token) int {
	n := 0
	for _, t := range toks {
		if t.Kind == Str {
			n++
			for _, p := range t.Parts {
				n += countStr(p)
			}
		}
	}
	return n
}
