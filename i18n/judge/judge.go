package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"v3rde/i18n/muse"
)

// Model answers one prompt (Muse Spark Open on Baseten, Muse Spark 1.3 on the Meta Model API, or a fake).
type Model interface {
	Name() string
	Ask(ctx context.Context, instructions, input string) (string, error)
}

// Baseten adapts muse.Client to Model.
type Baseten struct{ C *muse.Client }

// Name is the model name.
func (b Baseten) Name() string { return b.C.Settings.Model }

// Ask sends the instructions as the system message.
func (b Baseten) Ask(ctx context.Context, instructions, input string) (string, error) {
	r, err := b.C.Chat(ctx, instructions, input)
	return r.Text, err
}

// Group is one set of texts that a judge reads together: the texts of one
// concept or of one screen, in one language.
type Group struct {
	Kind  string   `json:"kind"` // concept or screen
	Name  string   `json:"name"`
	About string   `json:"about,omitempty"`
	IDs   []string `json:"ids"`
}

// Groups: one group for each concept and each screen of the graph that has at
// least 2 translated texts. Shared texts (same English and context) appear once.
func Groups(s Set) []Group {
	members := map[string][]string{}
	for _, e := range s.Graph.Edges {
		if (e.Kind == "uses" || e.Kind == "on_screen") && s.Trans[e.From] != "" && s.Shared[e.From] == "" {
			members[e.To] = append(members[e.To], e.From)
		}
	}
	order := map[string]int{}
	for i, m := range s.Msgs {
		order[m.ID] = i
	}
	about := map[string]string{}
	for _, n := range s.Graph.Nodes {
		about[n.ID] = n.Label
	}
	var out []Group
	for _, node := range sortedKeys(members) {
		ids := members[node]
		if len(ids) < 2 {
			continue
		}
		sort.Slice(ids, func(a, b int) bool { return order[ids[a]] < order[ids[b]] })
		kind, name, _ := strings.Cut(node, ":")
		out = append(out, Group{Kind: kind, Name: name, IDs: ids})
	}
	return out
}

// Verdict is the answer of one judge for one group.
type Verdict struct {
	Group   Group     `json:"group"`
	Model   string    `json:"model"`
	Verdict string    `json:"verdict"` // pass or fail; "" when Error is set
	Issues  []Problem `json:"issues"`
	Error   string    `json:"error,omitempty"`
	Raw     string    `json:"raw,omitempty"` // the answer as it came, for a check by a person
}

// Problem is one text that a judge marks.
type Problem struct {
	ID         string `json:"id"`
	Problem    string `json:"problem"`
	Suggestion string `json:"suggestion"`
}

const rulesConcept = `You check the translations of the user interface of a mobile app for sharing food.
You get the texts of the app that name one idea (a concept), each with its English text, what it does in the app, and its translation into %s (%s).
Check:
1. The idea is named with the same word (or the same word root) in all texts, in the grammar that each text needs. Report each text that uses another word for the idea.
2. Each translation fits what the text does: a button needs the action form that apps in %s use for buttons; a title or a label needs a noun phrase.
3. Each translation is correct %s. Placeholders like {n} stand for values and stay as they are.
Do not report style choices that are correct. Do not report the brand names V3RDE and WhatsApp.
Reply with JSON only, no other text, in this form:
{"verdict": "pass" or "fail", "issues": [{"id": "<text id>", "problem": "<what is wrong, one sentence>", "suggestion": "<a better translation>"}]}
Use "pass" with an empty issues list when all translations are correct.`

const rulesScreen = `You check the translations of the user interface of a mobile app for sharing food.
You get all the texts of one screen, each with its English text, what kind of text it is, what it does, and its translation into %s (%s).
Check:
1. Texts that are pairs or opposites in English (for example "Log out" and "Continue" to sign in, "Show" and "Hide") read as different actions in the translation.
2. All texts use the same form of address to the user (formal or informal). Report each text that uses the other form.
3. Each button uses the action form that apps in %s use for buttons; titles and labels use noun phrases.
4. Each translation fits what the text does and is correct %s. Placeholders like {n} stand for values and stay as they are.
Do not report style choices that are correct. Do not report the brand names V3RDE and WhatsApp.
Reply with JSON only, no other text, in this form:
{"verdict": "pass" or "fail", "issues": [{"id": "<text id>", "problem": "<what is wrong, one sentence>", "suggestion": "<a better translation>"}]}
Use "pass" with an empty issues list when all translations are correct.`

// Prompt makes the instructions and the input for one group.
func Prompt(s Set, g Group, conceptAbout string) (string, string) {
	rules := rulesScreen
	if g.Kind == "concept" {
		rules = rulesConcept
	}
	instr := fmt.Sprintf(rules, s.Lang.Name, s.Lang.Code, s.Lang.Name, s.Lang.Name)
	en := map[string]string{}
	for _, m := range s.Msgs {
		en[m.ID] = m.Text
	}
	var b strings.Builder
	if g.Kind == "concept" {
		fmt.Fprintf(&b, "Concept: %s. %s\n", g.Name, conceptAbout)
	} else {
		fmt.Fprintf(&b, "Screen: %s\n", g.Name)
	}
	type item struct {
		ID          string `json:"id"`
		English     string `json:"english"`
		Kind        string `json:"kind"`
		Does        string `json:"does"`
		Translation string `json:"translation"`
	}
	var items []item
	for _, id := range g.IDs {
		c := s.Ctx.Contexts[id]
		items = append(items, item{ID: id, English: en[id], Kind: c.Kind, Does: c.Does, Translation: s.Trans[id]})
	}
	j, _ := json.MarshalIndent(items, "", " ")
	b.Write(j)
	return instr, b.String()
}

// Parse reads the JSON of a judge's answer (the first {...} in it). Issues for
// ids that are not in the group are dropped.
func Parse(raw string, g Group) (string, []Problem, error) {
	i, j := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if i < 0 || j < i {
		return "", nil, fmt.Errorf("no JSON object in the answer")
	}
	var v struct {
		Verdict string    `json:"verdict"`
		Issues  []Problem `json:"issues"`
	}
	if err := json.Unmarshal([]byte(raw[i:j+1]), &v); err != nil {
		return "", nil, fmt.Errorf("bad JSON: %v", err)
	}
	v.Verdict = strings.ToLower(strings.TrimSpace(v.Verdict))
	if v.Verdict != "pass" && v.Verdict != "fail" {
		return "", nil, fmt.Errorf("verdict %q is not pass or fail", v.Verdict)
	}
	in := map[string]bool{}
	for _, id := range g.IDs {
		in[id] = true
	}
	out := []Problem{}
	for _, p := range v.Issues {
		if in[p.ID] && strings.TrimSpace(p.Problem) != "" {
			out = append(out, p)
		}
	}
	if v.Verdict == "fail" && len(out) == 0 {
		return "", nil, fmt.Errorf("verdict fail with no issue for a text of the group")
	}
	return v.Verdict, out, nil
}

// Judge asks every model about every group, with up to workers calls at the
// same time for each model. A failed call is a Verdict with Error (it flags nothing).
func Judge(ctx context.Context, s Set, groups []Group, models []Model, conceptAbout map[string]string, workers int) []Verdict {
	type job struct {
		g Group
		m Model
	}
	out := make([]Verdict, len(groups)*len(models))
	var wg sync.WaitGroup
	for mi, m := range models {
		jobs := make(chan int)
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for gi := range jobs {
					g := groups[gi]
					instr, input := Prompt(s, g, conceptAbout[g.Name])
					v := Verdict{Group: g, Model: m.Name(), Issues: []Problem{}}
					raw, err := m.Ask(ctx, instr, input)
					v.Raw = raw
					if err == nil {
						v.Verdict, v.Issues, err = Parse(raw, g)
						if v.Issues == nil {
							v.Issues = []Problem{}
						}
					}
					if err != nil {
						v.Error = err.Error()
					}
					out[mi*len(groups)+gi] = v
				}
			}()
		}
		go func() {
			for gi := range groups {
				jobs <- gi
			}
			close(jobs)
		}()
	}
	wg.Wait()
	return out
}

// Issues turns the verdicts into issues: until the judges are benchmarked, a
// text is flagged when either judge marks it (decision 2026-10-30).
func Issues(vs []Verdict) []Issue {
	var out []Issue
	for _, v := range vs {
		for _, p := range v.Issues {
			d := p.Problem
			if p.Suggestion != "" {
				d += " Suggestion: " + p.Suggestion
			}
			out = append(out, Issue{Check: v.Group.Kind + "_judge", IDs: []string{p.ID}, Detail: d, Group: v.Group.Name, Model: v.Model})
		}
	}
	return out
}

// Report is <lang>.judge.json.
type Report struct {
	Lang     string              `json:"lang"`
	Result   string              `json:"result_run"`
	Checks   []Issue             `json:"checks"`
	Verdicts []Verdict           `json:"verdicts"`
	Flagged  map[string][]string `json:"flagged"` // id -> reasons, from the checks and the judges
	Errors   int                 `json:"judge_errors"`
}
