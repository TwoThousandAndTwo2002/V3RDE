package barcode

import "strings"

// Product is the eight-field listing contract with the barcode and the
// source. Every key is always in the JSON (no omitempty): a 0 for
// is_non_veg or meal_cost is a value, and a dropped key reads as a missing
// field. Weight is in kilograms.
type Product struct {
	Barcode         string  `json:"barcode"`
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	IsNonVeg        int     `json:"is_non_veg"`
	NumberOfServing float64 `json:"number_of_serving"`
	ServingType     string  `json:"serving_type"`
	MealCondition   string  `json:"meal_condition"`
	Weight          float64 `json:"weight"`
	MealCost        float64 `json:"meal_cost"`
	Source          string  `json:"source"`
}

// A barcode finds a packaged retail product: one item, ready to eat, unless
// the catalog says more.
const (
	ServingTypeIndividual = "individual"
	MealConditionReady    = "ready_to_eat"
	DefaultServings       = 1.0
	SourceCatalog         = "fdc" // the source tag of a field from the catalog (the same tag as before)
	SourceConstant        = "constant"
)

// ModelFields are the contract fields a barcode cannot decide; a model or
// the donor fills them.
var ModelFields = []string{"description", "is_non_veg", "meal_cost"}

// Complete fills only what a catalog hit can decide (servings 1 when the
// catalog has none, individual, ready_to_eat). It keeps values that are
// there and never sets a model field.
func Complete(p Product) Product {
	if !(p.NumberOfServing > 0) {
		p.NumberOfServing = DefaultServings
	}
	if strings.TrimSpace(p.ServingType) == "" {
		p.ServingType = ServingTypeIndividual
	}
	if strings.TrimSpace(p.MealCondition) == "" {
		p.MealCondition = MealConditionReady
	}
	return p
}

// Provenance says which fields were decided, and by what (the catalog or a
// constant). It never names a model field.
func Provenance(p Product) map[string]string {
	prov := map[string]string{
		"serving_type":      SourceConstant,
		"meal_condition":    SourceConstant,
		"number_of_serving": SourceConstant,
	}
	if strings.TrimSpace(p.Title) != "" {
		prov["title"] = SourceCatalog
	}
	if p.NumberOfServing > 0 && p.NumberOfServing != DefaultServings {
		prov["number_of_serving"] = SourceCatalog
	}
	if p.Weight > 0 {
		prov["weight"] = SourceCatalog
	}
	return prov
}

// Admissible: the hit can fill a listing at once (a title, servings above 0
// and a weight above 0).
func Admissible(p Product) bool {
	return strings.TrimSpace(p.Title) != "" && p.NumberOfServing > 0 && p.Weight > 0
}
