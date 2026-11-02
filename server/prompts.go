package analyze

// P1 (T9 2026-11-01: "i would rather not change the prompt unless we have a
// strict method to ensure ... full tracibility in the logs so that we can
// attribute a change in performance to a changed prompt"): every prompt sent
// to a Meta model has an id and a version, and every call line in the events
// names them.
//
//   - PromptSpec: id, version, the call purposes it serves (prefixes), a
//     Golden function (the prompt for one fixed input: a template made from
//     parts, such as MultiPrompt, is rendered with a fixed list of boxes), and
//     an optional Match on the text that was sent, for two prompts with one
//     purpose (rescue:draft is RescuePrompt or DropoffPrompt).
//   - A purpose with no spec, or a sent text that no spec matches, logs the
//     id "unknown": the learn report counts it (L2); the inference flow tests
//     and the purpose tests of each module fail on it.
//   - The pin tests read a history of (id, version, sha) rows that only gets
//     new rows (prompts_pins.txt in each module): one sha per id and version,
//     and the current text is the newest row of its id. So a text change
//     needs a new row with a new version; the generator of the SUP apply
//     script refuses a change that edits or removes an earlier row.
//   - PromptRef (logged on each call): id, version, and SHA = the sha256 of
//     the golden text (16 hex). A test in each module pins id -> version ->
//     SHA: a change of the prompt text without a new version fails the SUP.
//   - InputSHA (logged on each call): the sha256 of the text that was really
//     sent (16 hex), so two calls with the same version but other parts (box
//     list, hint, language) can be told apart.
//
// No prompt text changes in P1.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// PromptRef names the prompt of one call.
type PromptRef struct {
	ID      string `json:"prompt_id,omitempty"`
	Version string `json:"prompt_version,omitempty"`
	SHA     string `json:"prompt_sha,omitempty"`
}

// PromptSpec is one registered prompt.
type PromptSpec struct {
	ID       string
	Version  string
	Purposes []string      // call purposes (exact, or a prefix that ends with ":" or "-")
	Golden   func() string // the prompt for one fixed input
	// Match (optional) is true when sent (TextOf the request) is this prompt.
	// Among the specs of the longest matching purpose, a spec whose Match is
	// true comes first, then a spec with no Match.
	Match func(sent string) bool
}

// Unknown is the PromptRef.ID of a call that no spec matches.
const Unknown = "unknown"

// StartsWith is a Match: sent starts with tmpl up to its first fmt verb ("%").
func StartsWith(tmpl string) func(string) bool {
	if i := strings.Index(tmpl, "%"); i >= 0 {
		tmpl = tmpl[:i]
	}
	return func(sent string) bool { return strings.HasPrefix(sent, tmpl) }
}

// EndsWith is a Match: sent ends with tail.
func EndsWith(tail string) func(string) bool {
	return func(sent string) bool { return strings.HasSuffix(sent, tail) }
}

// Not is a Match: m is false.
func Not(m func(string) bool) func(string) bool {
	return func(sent string) bool { return !m(sent) }
}

// Prompts is a set of specs, looked up by the call purpose.
type Prompts struct {
	specs []PromptSpec
	refs  map[string]PromptRef // by id
}

// NewPrompts computes the SHA of each spec. Two specs with one id panic (a
// programming error, found by the tests).
func NewPrompts(specs ...PromptSpec) *Prompts {
	p := &Prompts{specs: specs, refs: map[string]PromptRef{}}
	for _, s := range specs {
		if _, dup := p.refs[s.ID]; dup {
			panic("prompt id twice: " + s.ID)
		}
		p.refs[s.ID] = PromptRef{ID: s.ID, Version: s.Version, SHA: ShortSHA(s.Golden())}
	}
	return p
}

// For is the prompt of a call with this purpose and sent text. The specs
// of the longest matching purpose (exact, or a prefix that ends with ":" or
// "-") come first: the first whose Match is true, else the first with no
// Match; when none fits, the next shorter purpose is tried (so a SAM concept
// named "box" is sam:<concept>, not sam:box). No spec: {ID: Unknown}, false.
func (p *Prompts) For(purpose, sent string) (PromptRef, bool) {
	if p == nil {
		return PromptRef{ID: Unknown}, false
	}
	byLen := map[int][]PromptSpec{}
	var lens []int
	for _, s := range p.specs {
		for _, pp := range s.Purposes {
			if !servesPurpose(pp, purpose) {
				continue
			}
			if _, ok := byLen[len(pp)]; !ok {
				lens = append(lens, len(pp))
			}
			byLen[len(pp)] = append(byLen[len(pp)], s)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(lens)))
	for _, n := range lens {
		for _, s := range byLen[n] {
			if s.Match != nil && s.Match(sent) {
				return p.refs[s.ID], true
			}
		}
		for _, s := range byLen[n] {
			if s.Match == nil {
				return p.refs[s.ID], true
			}
		}
	}
	return PromptRef{ID: Unknown}, false
}

// Serves is true when a spec has this purpose (its Match not looked at).
func (p *Prompts) Serves(purpose string) bool {
	if p == nil {
		return false
	}
	for _, s := range p.specs {
		for _, pp := range s.Purposes {
			if servesPurpose(pp, purpose) {
				return true
			}
		}
	}
	return false
}

// servesPurpose: purpose is pp, or pp is a prefix of it that ends with ":"
// or "-".
func servesPurpose(pp, purpose string) bool {
	return purpose == pp || ((strings.HasSuffix(pp, ":") || strings.HasSuffix(pp, "-")) && strings.HasPrefix(purpose, pp))
}

// Refs lists every registered prompt, sorted by id (the pin test).
func (p *Prompts) Refs() []PromptRef {
	out := make([]PromptRef, 0, len(p.refs))
	for _, r := range p.refs {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// CheckPinHistory compares the registry with a pin history: one row per
// line, "id version sha" (text after "#" and empty lines are left out). It
// lists every problem: a row that is not 3 fields, an id and version with
// two shas, a registered prompt with no row, a prompt whose newest row (the
// last row of its id) is not its id, version and sha now, a row of an id that
// is not registered. Empty = the pins hold.
func CheckPinHistory(refs []PromptRef, history string) []string {
	var bad []string
	type key struct{ id, version string }
	sha := map[key]string{}
	newest := map[string]PromptRef{}
	for n, line := range strings.Split(history, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) != 3 {
			bad = append(bad, fmt.Sprintf("line %d: want \"id version sha\", got %q", n+1, line))
			continue
		}
		k := key{f[0], f[1]}
		if old, ok := sha[k]; ok && old != f[2] {
			bad = append(bad, fmt.Sprintf("line %d: %s version %s has two shas (%s, %s): a new text needs a new version", n+1, f[0], f[1], old, f[2]))
		}
		sha[k] = f[2]
		newest[f[0]] = PromptRef{ID: f[0], Version: f[1], SHA: f[2]}
	}
	reg := map[string]bool{}
	for _, r := range refs {
		reg[r.ID] = true
		got, ok := newest[r.ID]
		switch {
		case !ok:
			bad = append(bad, fmt.Sprintf("%s has no pin row: add \"%s %s %s\"", r.ID, r.ID, r.Version, r.SHA))
		case got != r:
			bad = append(bad, fmt.Sprintf("%s is version %s sha %s now; its newest pin row is version %s sha %s (a text change needs a NEW row with a new version)",
				r.ID, r.Version, r.SHA, got.Version, got.SHA))
		}
	}
	for id := range newest {
		if !reg[id] {
			bad = append(bad, fmt.Sprintf("pin rows of %s, which is not registered", id))
		}
	}
	sort.Strings(bad)
	return bad
}

// ShortSHA is the first 16 hex characters of the sha256 of s.
func ShortSHA(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// TextOf is the text parts of a Responses API body: every string under a
// "text" key (maps in key order, lists in order), joined by a 0 byte. Images
// and other fields are not in it.
func TextOf(body any) string {
	var parts []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if s, ok := t[k].(string); ok && k == "text" {
					parts = append(parts, s)
					continue
				}
				walk(t[k])
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		case []map[string]any:
			for _, x := range t {
				walk(x)
			}
		}
	}
	walk(body)
	return strings.Join(parts, "\x00")
}

// InputSHA is the ShortSHA of sent; "" when sent is "".
func InputSHA(sent string) string {
	if sent == "" {
		return ""
	}
	return ShortSHA(sent)
}

// Effort is the reasoning effort in a Responses API body ("" when none).
func Effort(body map[string]any) string {
	r, _ := body["reasoning"].(map[string]any)
	e, _ := r["effort"].(string)
	return e
}
