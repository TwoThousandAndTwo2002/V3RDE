package i18n

import (
	"strings"
	"testing"
)

// The context block of a prompt (L4h): kind and action, tags in place of
// {names}, pairs, joined parts, near controls, and the earlier translations
// of the same idea.
func TestKnowledgeBlock(t *testing.T) {
	var en EnFile
	var g GraphFile
	var cf ContextFile
	for p, v := range map[string]any{"en.json": &en, "graph.json": &g, "context.json": &cf} {
		if err := ReadJSON(p, v); err != nil {
			t.Fatal(err)
		}
	}
	k := NewKnowledge(en.Messages, g, cf)
	prev := map[string]string{"GIVE.title": "Tus publicaciones"}
	post := k.Block("logic.postTitle.post", map[string]string{"{plural}": "<x1/>"}, prev)
	for _, want := range []string{"Kind: Button.", "Verb in the imperative", "<x1/> is a count and a noun", "The app puts these texts into it: \"listing\", \"listings\""} {
		if !strings.Contains(post, want) {
			t.Errorf("Post block has no %q:\n%s", want, post)
		}
	}
	if reParam.MatchString(post) || reParam.MatchString(listing0(k, prev)) {
		t.Errorf("the block must show tags and [value], never a {name}:\n%s", post)
	}
	logout := k.Block("ROLE.logout", nil, nil)
	if !strings.Contains(logout, `Paired with: "Continue" (Button)`) || !strings.Contains(logout, "Not 'sign in'") {
		t.Errorf("Log out block:\n%s", logout)
	}
	listing := k.Block("logic.postTitle.listing", nil, prev)
	if !strings.Contains(listing, `"Your listings" = "Tus publicaciones"`) || !strings.Contains(listing, `joins this text into: "Post [value]"`) {
		t.Errorf("listing block:\n%s", listing)
	}
	retake := k.Block("CAPTURE.retake", nil, nil)
	if strings.Contains(retake, "Error line") || !strings.Contains(retake, `"Use photo" (Button)`) {
		t.Errorf("Retake block: errors of another sheet, or no near button:\n%s", retake)
	}
	if k.Key("ROLE.close") == k.Key("CAPTURE.closeFlow") {
		t.Error("the same English with another context must be another key")
	}
}

func listing0(k *Knowledge, prev map[string]string) string {
	return k.Block("logic.postedSummary.posted_kept_here_try", map[string]string{"{plural}": "<x1/>", "{p2}": "<x2/>"}, map[string]string{"logic.postingText": "Publicando {i} de {n}…"})
}
