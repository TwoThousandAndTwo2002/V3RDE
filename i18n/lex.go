// Package i18n reads the English text of the mobile app (branch ui, mobile/src)
// and writes it as i18n/en.json, the source file of every translation (L3).
package i18n

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Kind of a token.
const (
	Ident = iota
	Num
	Op
	Open
	Close
	Str // a string or template literal
	Regex
)

// Token is one token of a TypeScript or TSX file.
type Token struct {
	Kind  int
	Text  string // the source text; for Str the decoded text, a template ${x} as {x}
	Line  int
	Tmpl  bool      // Str: a template literal
	Parts [][]Token // Str template: the tokens of each ${...}, in order
	Exprs []string  // Str template: the source of each ${...}, in order
	names map[string]string
}

// Lex returns the tokens of a TypeScript or TSX file. Comments are skipped.
// It returns an error for JSX text: the app keeps all its text in TypeScript
// strings, and a test keeps it so.
func Lex(file, src string) ([]Token, error) {
	l := &lexer{src: []rune(src), line: 1, file: file, tsx: strings.HasSuffix(file, ".tsx")}
	toks := l.run(false)
	if l.err != nil {
		return nil, l.err
	}
	return toks, nil
}

type lexer struct {
	src  []rune
	i    int
	line int
	file string
	tsx  bool
	err  error
}

func (l *lexer) peek(k int) rune {
	if l.i+k < len(l.src) {
		return l.src[l.i+k]
	}
	return 0
}

func (l *lexer) fail(f string, a ...any) {
	if l.err == nil {
		l.err = fmt.Errorf("%s:%d: %s", l.file, l.line, fmt.Sprintf(f, a...))
	}
}

// regexOK: a "/" starts a regex literal when the token before it cannot end an expression.
func regexOK(toks []Token) bool {
	if len(toks) == 0 {
		return true
	}
	t := toks[len(toks)-1]
	switch t.Kind {
	case Num, Str, Regex, Close:
		return false
	case Ident:
		switch t.Text {
		case "return", "typeof", "case", "in", "of", "else", "void", "delete", "throw", "new":
			return true
		}
		return false
	}
	return t.Text != "<" // JSX "</"
}

var ops = []string{"===", "!==", "...", "**", "=>", "==", "!=", "<=", ">=", "&&", "||", "??", "?.", "++", "--", "+=", "-=", "*=", "/="}

func isIDStart(r rune) bool { return r == '_' || r == '$' || unicode.IsLetter(r) }
func isIDPart(r rune) bool  { return isIDStart(r) || unicode.IsDigit(r) }

// run lexes to the end, or (inTmpl) to the "}" that closes a ${.
func (l *lexer) run(inTmpl bool) []Token {
	var toks []Token
	depth := 0
	for l.i < len(l.src) && l.err == nil {
		c := l.src[l.i]
		line := l.line
		switch {
		case c == '\n':
			l.line++
			l.i++
		case unicode.IsSpace(c):
			l.i++
		case c == '/' && l.peek(1) == '/':
			for l.i < len(l.src) && l.src[l.i] != '\n' {
				l.i++
			}
		case c == '/' && l.peek(1) == '*':
			l.i += 2
			for l.i < len(l.src) && !(l.src[l.i] == '*' && l.peek(1) == '/') {
				if l.src[l.i] == '\n' {
					l.line++
				}
				l.i++
			}
			if l.i >= len(l.src) {
				l.fail("unterminated comment")
			}
			l.i += 2
		case c == '"' || c == '\'':
			toks = append(toks, l.str(c, line))
		case c == '`':
			toks = append(toks, l.tmpl(line))
		case c == '/' && l.peek(1) != '>' && regexOK(toks):
			l.regex()
			toks = append(toks, Token{Kind: Regex, Text: "/re/", Line: line})
		case c == '{' || c == '(' || c == '[':
			depth++
			toks = append(toks, Token{Kind: Open, Text: string(c), Line: line})
			l.i++
		case c == '}' || c == ')' || c == ']':
			if inTmpl && depth == 0 {
				if c != '}' {
					l.fail("bad %q in ${...}", c)
				}
				l.i++
				return toks
			}
			depth--
			toks = append(toks, Token{Kind: Close, Text: string(c), Line: line})
			l.i++
		case isIDStart(c):
			s := l.i
			for l.i < len(l.src) && isIDPart(l.src[l.i]) {
				l.i++
			}
			toks = append(toks, Token{Kind: Ident, Text: string(l.src[s:l.i]), Line: line})
		case unicode.IsDigit(c) || (c == '.' && unicode.IsDigit(l.peek(1))):
			s := l.i
			for l.i < len(l.src) && (isIDPart(l.src[l.i]) || l.src[l.i] == '.') {
				l.i++
			}
			toks = append(toks, Token{Kind: Num, Text: string(l.src[s:l.i]), Line: line})
		default:
			op := string(c)
			rest := string(l.src[l.i:min(l.i+3, len(l.src))])
			for _, m := range ops {
				if strings.HasPrefix(rest, m) {
					op = m
					break
				}
			}
			if c == '>' && l.tsx {
				l.jsxText()
			}
			toks = append(toks, Token{Kind: Op, Text: op, Line: line})
			l.i += len([]rune(op))
		}
	}
	if inTmpl && l.err == nil {
		l.fail("unterminated ${")
	}
	if !inTmpl && depth != 0 && l.err == nil {
		l.fail("unbalanced brackets (%d open)", depth)
	}
	return toks
}

// jsxText fails on text between a ">" and a "<" on one line, such as <Text>Send</Text>.
func (l *lexer) jsxText() {
	j := l.i + 1
	for j < len(l.src) && (l.src[j] == ' ' || l.src[j] == '\t') {
		j++
	}
	if j >= len(l.src) || !unicode.IsLetter(l.src[j]) {
		return
	}
	k := j
	for k < len(l.src) && !strings.ContainsRune("\n<{};=()&|?:,\"'`", l.src[k]) {
		k++
	}
	if k < len(l.src) && l.src[k] == '<' && k+1 < len(l.src) && (l.src[k+1] == '/' || unicode.IsLetter(l.src[k+1])) {
		l.fail("JSX text %q: the app keeps its text in TypeScript strings", strings.TrimSpace(string(l.src[j:k])))
	}
}

func (l *lexer) regex() {
	l.i++
	cls := false
	for l.i < len(l.src) {
		c := l.src[l.i]
		switch {
		case c == '\\':
			l.i += 2
			continue
		case c == '\n':
			l.fail("unterminated regex")
			return
		case c == '[':
			cls = true
		case c == ']':
			cls = false
		case c == '/' && !cls:
			l.i++
			for l.i < len(l.src) && unicode.IsLetter(l.src[l.i]) {
				l.i++
			}
			return
		}
		l.i++
	}
	l.fail("unterminated regex")
}

func (l *lexer) hex(n int) rune {
	if l.i+1+n > len(l.src) {
		l.fail("short escape")
		return 0
	}
	v, err := strconv.ParseUint(string(l.src[l.i+1:l.i+1+n]), 16, 32)
	if err != nil {
		l.fail("bad escape")
	}
	l.i += n
	return rune(v)
}

// escape reads one escape (l.i is at the backslash).
func (l *lexer) escape(b *strings.Builder) {
	l.i++
	if l.i >= len(l.src) {
		l.fail("escape at the end")
		return
	}
	switch c := l.src[l.i]; c {
	case 'n':
		b.WriteRune('\n')
	case 't':
		b.WriteRune('\t')
	case 'r':
		b.WriteRune('\r')
	case '\n':
		l.line++ // a line continuation
	case 'u':
		if l.peek(1) == '{' {
			e := l.i + 2
			for e < len(l.src) && l.src[e] != '}' {
				e++
			}
			v, err := strconv.ParseUint(string(l.src[l.i+2:e]), 16, 32)
			if err != nil {
				l.fail("bad \\u{} escape")
			}
			b.WriteRune(rune(v))
			l.i = e
		} else {
			b.WriteRune(l.hex(4))
		}
	case 'x':
		b.WriteRune(l.hex(2))
	default:
		b.WriteRune(c)
	}
	l.i++
}

func (l *lexer) str(q rune, line int) Token {
	l.i++
	var b strings.Builder
	for l.err == nil {
		if l.i >= len(l.src) || l.src[l.i] == '\n' {
			l.fail("unterminated string")
			break
		}
		c := l.src[l.i]
		if c == q {
			l.i++
			break
		}
		if c == '\\' {
			l.escape(&b)
			continue
		}
		b.WriteRune(c)
		l.i++
	}
	return Token{Kind: Str, Text: b.String(), Line: line}
}

func (l *lexer) tmpl(line int) Token {
	l.i++
	t := Token{Kind: Str, Tmpl: true, Line: line}
	var b strings.Builder
	for l.err == nil {
		if l.i >= len(l.src) {
			l.fail("unterminated template")
			break
		}
		c := l.src[l.i]
		if c == '`' {
			l.i++
			break
		}
		if c == '\\' {
			l.escape(&b)
			continue
		}
		if c == '$' && l.peek(1) == '{' {
			l.i += 2
			s := l.i
			inner := l.run(true)
			if l.err != nil {
				break
			}
			expr := strings.TrimSpace(string(l.src[s : l.i-1]))
			t.Parts = append(t.Parts, inner)
			t.Exprs = append(t.Exprs, expr)
			b.WriteString("{" + uniqueName(ParamName(inner, len(t.Parts)), expr, t.Exprs[:len(t.Exprs)-1], &t) + "}")
			continue
		}
		if c == '\n' {
			l.line++
		}
		b.WriteRune(c)
		l.i++
	}
	t.Text = b.String()
	return t
}

// ParamName names a placeholder from its expression: the last name of a
// plain a.b.c chain or of the function a call goes to (servings(n) is
// {servings}); anything else is p<k>, k counted from 1 in the message.
func ParamName(expr []Token, k int) string {
	end := len(expr)
	// drop one trailing call: f(...)
	if end > 0 && expr[end-1].Text == ")" {
		d := 0
		for j := end - 1; j >= 0; j-- {
			if expr[j].Kind == Close {
				d++
			} else if expr[j].Kind == Open {
				d--
				if d == 0 {
					end = j
					break
				}
			}
		}
		if d != 0 {
			end = 0
		}
	}
	if end == 0 || end%2 == 0 {
		return fmt.Sprintf("p%d", k)
	}
	for j := 0; j < end; j++ {
		if j%2 == 0 && expr[j].Kind != Ident {
			return fmt.Sprintf("p%d", k)
		}
		if j%2 == 1 && expr[j].Text != "." && expr[j].Text != "?." {
			return fmt.Sprintf("p%d", k)
		}
	}
	return expr[end-1].Text
}

// uniqueName keeps one name for one expression: the same name for a second,
// different expression gets _2, _3, ...
func uniqueName(name, expr string, before []string, t *Token) string {
	if t.names == nil {
		t.names = map[string]string{}
	}
	if n, ok := t.names[expr]; ok {
		return n
	}
	used := map[string]bool{}
	for _, n := range t.names {
		used[n] = true
	}
	n := name
	for k := 2; used[n]; k++ {
		n = fmt.Sprintf("%s_%d", name, k)
	}
	t.names[expr] = n
	return n
}
