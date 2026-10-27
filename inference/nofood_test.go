package inference

import (
	"encoding/json"
	"os"
	"testing"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// The rule against the 57 real Muse answers in the Morpheus logs: it must
// find the 8 "no food" names and none of the 49 food names.
func TestModelSaysNoFoodOnTheLoggedAnswers(t *testing.T) {
	b, err := os.ReadFile("testdata/x32_muse_names.json")
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Answers []struct {
			Name   string `json:"name"`
			NoFood bool   `json:"no_food"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	found, food := 0, 0
	for _, a := range d.Answers {
		got := ModelSaysNoFood(analyze.Result{Name: a.Name})
		if got != a.NoFood {
			t.Errorf("%q: got %v, want %v", a.Name, got, a.NoFood)
		}
		if a.NoFood {
			found++
		} else {
			food++
		}
	}
	if len(d.Answers) != 57 || found != 8 || food != 49 {
		t.Fatalf("answers %d, no food %d, food %d", len(d.Answers), found, food)
	}
}

func TestModelSaysNoFoodDescription(t *testing.T) {
	for _, c := range []struct {
		name, desc string
		want       bool
	}{
		{"No food", "No edible food visible in image.", true}, // T9's Postman answer, 2026-10-27
		{"Empty plate", "No edible food visible in image.", true},
		{"Table", "There is no food in this photo.", true},
		{"Granola bar", "No added sugar; a healthy food bar.", false},
		{"Nonfat yogurt", "Plain nonfat yogurt cup.", false},
		{"No-bake cookies", "Chocolate oat no-bake cookies.", false},
		{"Unknown.", "", true},
	} {
		if got := ModelSaysNoFood(analyze.Result{Name: c.name, Description: c.desc}); got != c.want {
			t.Errorf("%q / %q: got %v", c.name, c.desc, got)
		}
	}
}
