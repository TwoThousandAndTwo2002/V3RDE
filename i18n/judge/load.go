package judge

import (
	"fmt"

	"v3rde/i18n"
	"v3rde/i18n/rung"
)

// Load reads a result (<lang>.json) and the i18n files of the current
// directory (en.json, languages.json, graph.json, context.json).
func Load(result string) (Set, error) {
	var r rung.Result
	var en i18n.EnFile
	var langs i18n.Languages
	var s Set
	for p, v := range map[string]any{result: &r, "en.json": &en, "languages.json": &langs, "graph.json": &s.Graph, "context.json": &s.Ctx} {
		if err := i18n.ReadJSON(p, v); err != nil {
			return s, fmt.Errorf("%s: %v", p, err)
		}
	}
	for _, l := range langs.Languages {
		if l.Code == r.Lang {
			s.Lang = l
		}
	}
	if s.Lang.Code == "" {
		return s, fmt.Errorf("%s: language %q is not in languages.json", result, r.Lang)
	}
	s.Keep, s.Msgs = langs.Keep, en.Messages
	s.Trans, s.Shared = map[string]string{}, map[string]string{}
	for _, c := range r.Messages {
		s.Trans[c.ID] = c.Text
		if c.SameAs != "" {
			s.Shared[c.ID] = c.SameAs
		}
	}
	return s, nil
}
