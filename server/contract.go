// Package analyze is the V3RDE analyze server and the shared contract code
// that the inference and post services import.
//
// One request: the app sends one food photo to POST /v1/analyze, the server
// asks Meta's Muse Spark for the eight-field listing contract, checks it and
// returns it with number_of_serving set to 1 (the donor changes it in the
// app). The Meta key stays on the server; the app does no model work.
//
// V3RDE #24 (Oct 28 2026): this package is written new for V3RDE. The wire
// contract (the prompt, the eight fields, the routes, the status codes and
// the error texts) is kept the same as the analyze server the app used
// before the Hackathon, so the app and the inference service do not change.
// The Meta API shape is https://dev.meta.ai/docs/image-understanding:
// POST /v1/responses, input_image as a data: URL, the text back in
// output[].content[].text.
package analyze

import (
	"encoding/json"
	"errors"
	"strings"
)

// Prompt is the listing contract the model gets. It is kept byte for byte:
// the contract tests and the eval results depend on this exact text.
const Prompt = `Identify the food in this image and return ONLY valid JSON with:
name, description, is_non_veg (0 vegetarian or 1), number_of_serving,
serving_type (individual or bulk), meal_condition
(ready_to_eat, cooking_required, sealed), weight (kg), meal_cost.
description must be ONE sentence, 10 words maximum.
Use conservative estimates if unsure.`

// DefaultServings is the number_of_serving of every answer: a listing starts
// at one serving and the donor changes it in the app.
const DefaultServings = 1

// Result is the eight-field contract as the app receives it.
type Result struct {
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	IsNonVeg        int     `json:"is_non_veg"`
	NumberOfServing float64 `json:"number_of_serving"`
	ServingType     string  `json:"serving_type"`
	MealCondition   string  `json:"meal_condition"`
	Weight          float64 `json:"weight"`
	MealCost        float64 `json:"meal_cost"`
}

// contractKeys is the order in which Validate reports missing keys.
var contractKeys = []string{"name", "description", "is_non_veg", "number_of_serving",
	"serving_type", "meal_condition", "weight", "meal_cost"}

var errNoObject = errors.New("contract: not a JSON object")

// ExtractJSON takes the text from the first '{' to the last '}' and decodes
// it as one JSON object. Models often put words or code fences around the
// object; this ignores them.
func ExtractJSON(text string) (map[string]any, error) {
	i := strings.IndexByte(text, '{')
	j := strings.LastIndexByte(text, '}')
	if i < 0 || j < i {
		return nil, errNoObject
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text[i:j+1]), &out); err != nil {
		return nil, err
	}
	if out == nil { // a guard: only a JSON null decodes to a nil map
		return nil, errNoObject
	}
	return out, nil
}

// field reads one key of the decoded object.
type field struct {
	key  string
	want string // the error text when the value is wrong
	set  func(r *Result, v any) bool
}

// fields are checked in this order; the first wrong one is the error.
// JSON numbers decode to float64; JSON true/false are never a number here.
var fields = []field{
	{"name", "contract: name is not a string", func(r *Result, v any) bool {
		s, ok := v.(string)
		r.Name = s
		return ok
	}},
	{"description", "contract: description is not a string", func(r *Result, v any) bool {
		s, ok := v.(string)
		r.Description = s
		return ok
	}},
	{"is_non_veg", "contract: is_non_veg is not 0 or 1", func(r *Result, v any) bool {
		f, ok := v.(float64)
		if !ok || (f != 0 && f != 1) {
			return false
		}
		r.IsNonVeg = int(f)
		return true
	}},
	{"number_of_serving", "contract: number_of_serving is not a number", func(r *Result, v any) bool {
		f, ok := v.(float64)
		r.NumberOfServing = f
		return ok
	}},
	{"serving_type", "contract: serving_type is not individual|bulk", func(r *Result, v any) bool {
		s, _ := v.(string)
		r.ServingType = s
		return s == "individual" || s == "bulk"
	}},
	{"meal_condition", "contract: meal_condition is not ready_to_eat|cooking_required|sealed", func(r *Result, v any) bool {
		s, _ := v.(string)
		r.MealCondition = s
		return s == "ready_to_eat" || s == "cooking_required" || s == "sealed"
	}},
	{"weight", "contract: weight is not a number", func(r *Result, v any) bool {
		f, ok := v.(float64)
		r.Weight = f
		return ok
	}},
	{"meal_cost", "contract: meal_cost is not a number", func(r *Result, v any) bool {
		f, ok := v.(float64)
		r.MealCost = f
		return ok
	}},
}

// Validate checks the decoded object against the contract: every key is
// there, strings are strings, numbers are numbers, the enums are exact.
// Nothing half-filled is returned as valid.
func Validate(data map[string]any) (Result, error) {
	var r Result
	var missing []string
	for _, k := range contractKeys {
		if _, ok := data[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return r, errors.New("contract: missing " + strings.Join(missing, ","))
	}
	for _, f := range fields {
		if !f.set(&r, data[f.key]) {
			return r, errors.New(f.want)
		}
	}
	return r, nil
}
