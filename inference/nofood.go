package inference

// X32a (T9, 2026-10-27): when the gate says FOOD but the model's answer
// says that it found no food, the item is dropped and the request gets
// 422 model_no_food_single (one item) or model_no_food_multi (2+ items).
// The single-item prompt is analyze.Prompt, which has no food field, so
// the only signal is the text. T9: "keep it simple": a fixed list of
// names, and a description that says no food is visible.

import (
	"regexp"
	"strings"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// noFoodNames are the exact names (lower case, no end punctuation) that
// mean "nothing found". Seen in the Morpheus logs: "None", "Unknown",
// "No food", "No Food Detected" (testdata/x32_muse_names.json).
var noFoodNames = map[string]bool{"none": true, "unknown": true, "nothing": true, "n/a": true, "na": true,
	"not food": true, "no food": true}

// noFoodName: a name that starts "no food" or "not food" ("No Food Detected").
var noFoodName = regexp.MustCompile(`^(no|not) (edible |visible |identifiable )?food\b`)

// noFoodDesc: a description that says no food is present ("No edible food
// visible in image."). It must say "no ... food" AND visible/detected/found
// or image/photo, so "No added sugar; a healthy food bar." does not match.
var noFoodDesc = regexp.MustCompile(`^(there is )?no ([a-z]+ ){0,2}food\b.*\b(visible|detected|found|present|shown|image|photo|picture)\b`)

// ModelSaysNoFood reports whether a contract answer says that no food was found.
func ModelSaysNoFood(r analyze.Result) bool {
	n := strings.TrimRight(strings.ToLower(strings.TrimSpace(r.Name)), ".!")
	if noFoodNames[n] || noFoodName.MatchString(n) {
		return true
	}
	return noFoodDesc.MatchString(strings.ToLower(strings.TrimSpace(r.Description)))
}
