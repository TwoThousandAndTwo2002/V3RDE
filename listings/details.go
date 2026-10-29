package listings

// X48d (T9 2026-10-29: "store the results even if we are not displaying it.
// ideally we should display it"): the item details of the analyze answer.
// The app sends the fields the scanner found for an item, besides the title
// and the servings, as one optional form field:
//
//	details = {"description": "...", "is_non_veg": 0|1,
//	           "serving_type": "individual"|"bulk",
//	           "meal_condition": "ready_to_eat"|"cooking_required"|"sealed",
//	           "weight": <kg>, "meal_cost": <number>}
//
// Every key is optional; a key that is not one of these, a wrong type or a
// value out of range is a 400. The listing keeps them under "details" (left
// out when there are none), so the listing JSON and the data file of a
// listing with no details do not change.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

// The limits of the details field.
const (
	MaxDetailsBytes = 1024
	MaxDescription  = 200     // characters (runes); 200 of 4 bytes still fit MaxDetailsBytes
	MaxWeightKg     = 1000    // kg
	MaxMealCost     = 100_000 // in the app's currency
)

// Details are the scanner's fields of one item (all optional).
type Details struct {
	Description   string   `json:"description,omitempty"`
	IsNonVeg      *int     `json:"is_non_veg,omitempty"`
	ServingType   string   `json:"serving_type,omitempty"`
	MealCondition string   `json:"meal_condition,omitempty"`
	Weight        *float64 `json:"weight,omitempty"`
	MealCost      *float64 `json:"meal_cost,omitempty"`
}

// parseDetails reads the form field; "" (or {}) is no details.
func parseDetails(raw string) (*Details, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > MaxDetailsBytes {
		return nil, fmt.Errorf("details must be at most %d bytes", MaxDetailsBytes)
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	var d Details
	var extra json.RawMessage
	if err := dec.Decode(&d); err != nil || dec.Decode(&extra) != io.EOF { // one object, nothing after it
		return nil, errors.New("details must be a JSON object of description, is_non_veg, serving_type, meal_condition, weight, meal_cost")
	}
	return d.check()
}

// check applies the rules; an empty result is nil.
func (d Details) check() (*Details, error) {
	d.Description = strings.TrimSpace(d.Description)
	if utf8.RuneCountInString(d.Description) > MaxDescription {
		return nil, fmt.Errorf("details.description must be at most %d characters", MaxDescription)
	}
	if d.IsNonVeg != nil && *d.IsNonVeg != 0 && *d.IsNonVeg != 1 {
		return nil, errors.New("details.is_non_veg must be 0 or 1")
	}
	if d.ServingType != "" && d.ServingType != "individual" && d.ServingType != "bulk" {
		return nil, errors.New("details.serving_type must be individual or bulk")
	}
	switch d.MealCondition {
	case "", "ready_to_eat", "cooking_required", "sealed":
	default:
		return nil, errors.New("details.meal_condition must be ready_to_eat, cooking_required or sealed")
	}
	if !inRange(d.Weight, MaxWeightKg) {
		return nil, fmt.Errorf("details.weight must be 0-%d kg", MaxWeightKg)
	}
	if !inRange(d.MealCost, MaxMealCost) {
		return nil, fmt.Errorf("details.meal_cost must be 0-%d", MaxMealCost)
	}
	if d == (Details{}) {
		return nil, nil
	}
	return &d, nil
}

func inRange(v *float64, max float64) bool {
	return v == nil || (!math.IsNaN(*v) && *v >= 0 && *v <= max)
}
