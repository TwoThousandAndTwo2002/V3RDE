package inference

// Tray servings (T9, 2026-10-26): a ballpark the user can change in the
// app. Muse names the pan size and depth (with a serving spoon, tongs or a
// plate as a size reference) and whether the dish is a main or a side; the
// servings come from a fixed table, so the same tray always gets the same
// number. Without a hotel-pan size ("other"), the model's own estimate is
// kept.
//
// Table: servings per pan by size, depth and ounces per serving, from the
// manufacturer's chart: Vigor steam table pan layouts (WebstaurantStore,
// webstaurantstore.com/documents/pdf/vigorpan_layouts.pdf, read
// 2026-10-26). Portion: main 6 oz, side 4 oz (our assumption).

// TrayPan is what the model said about one tray, and what we used.
type TrayPan struct {
	Size          string  `json:"size"`  // full | two_thirds | half | third | quarter | sixth | ninth | other
	Depth         string  `json:"depth"` // shallow (2.5") | medium (4") | deep (6")
	Dish          string  `json:"dish"`  // main | side
	FromTable     bool    `json:"from_table"`
	ModelServings float64 `json:"model_servings,omitempty"` // the model's own estimate, kept for review
}

// PortionOz is the serving size used with the table.
var PortionOz = map[string]int{"main": 6, "side": 4}

// panTable[size][depth][oz] = servings (Vigor chart; 4 and 6 oz columns).
var panTable = map[string]map[string]map[int]int{
	"full":       {"shallow": {4: 67, 6: 44}, "medium": {4: 113, 6: 75}, "deep": {4: 171, 6: 114}},
	"two_thirds": {"shallow": {4: 44, 6: 29}, "medium": {4: 74, 6: 49}, "deep": {4: 113, 6: 75}},
	"half":       {"shallow": {4: 34, 6: 22}, "medium": {4: 53, 6: 35}, "deep": {4: 80, 6: 53}},
	"third":      {"shallow": {4: 21, 6: 14}, "medium": {4: 33, 6: 22}, "deep": {4: 49, 6: 32}},
	"quarter":    {"shallow": {4: 15, 6: 10}, "medium": {4: 24, 6: 16}, "deep": {4: 36, 6: 24}},
	"sixth":      {"shallow": {4: 10, 6: 6}, "medium": {4: 15, 6: 10}, "deep": {4: 21, 6: 14}},
	"ninth":      {"shallow": {4: 4, 6: 2}, "medium": {4: 6, 6: 4}, "deep": {4: 10, 6: 6}},
}

// PanServings looks up a tray. Unknown depth = shallow (the usual foil
// pan); unknown dish = main (fewer servings: the safe side).
func PanServings(p TrayPan) (int, bool) {
	bySize, ok := panTable[p.Size]
	if !ok {
		return 0, false
	}
	byDepth, ok := bySize[p.Depth]
	if !ok {
		byDepth = bySize["shallow"]
	}
	oz, ok := PortionOz[p.Dish]
	if !ok {
		oz = PortionOz["main"]
	}
	return byDepth[oz], true
}

func (en entry) panRef() *TrayPan {
	if !en.tray {
		return nil
	}
	p := en.pan
	return &p
}
