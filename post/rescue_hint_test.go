package post

// R1 tests: rescue gives the scanner's meal_condition to Muse Spark as a hint.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	listings "github.com/TwoThousandAndTwo2002/Icarus/listings"
)

// A listing with no details, a value that is not in the contract, or a scan
// that was not the vision route gets the X35b prompt byte for byte (regression).
func TestRescuePromptWithoutHintIsUnchanged(t *testing.T) {
	want := fmt.Sprintf(RescuePrompt, "Canned beans", 24)
	for _, d := range []*ListingDetails{nil, {}, {MealCondition: "frozen"}, {Description: "Beans in cans"}} {
		got, cond := rescuePrompt(Listing{Title: "Canned beans", NumberOfServing: 24, Details: d}, ScanVision)
		if got != want || cond != "" {
			t.Fatalf("details %+v: hint %q, prompt changed:\n%s", d, cond, got)
		}
	}
	for _, src := range []string{"", ScanBarcode, "Vision", "other"} {
		got, cond := rescuePrompt(Listing{Title: "Canned beans", NumberOfServing: 24,
			Details: &ListingDetails{MealCondition: "sealed", Description: "Beans"}}, src)
		if got != want || cond != "" {
			t.Fatalf("route %q: hint %q, prompt changed:\n%s", src, cond, got)
		}
	}
}

func TestRescuePromptHint(t *testing.T) {
	for cond, words := range hintWords {
		got, c := rescuePrompt(Listing{Title: "Food 100%", NumberOfServing: 3,
			Details: &ListingDetails{MealCondition: cond, Description: "  Cups of\n\tyogurt, 5% fat  "}}, ScanVision)
		if c != cond {
			t.Fatalf("%s: hint %q", cond, c)
		}
		note := "Scanner note, made when the food was listed (it can be wrong; the photo decides): " + words +
			"; the scanner described it as: Cups of yogurt, 5% fat.\n"
		// the note is the line after "Servings:", the rest of the prompt is the same
		if !strings.Contains(got, "Title: Food 100%\nServings: 3\n"+note+"Look at the photo.") {
			t.Fatalf("%s: prompt\n%s", cond, got)
		}
		if strings.Replace(got, note, "", 1) != fmt.Sprintf(RescuePrompt, "Food 100%", 3) || strings.Contains(got, "%!") {
			t.Fatalf("%s: more than the note changed:\n%s", cond, got)
		}
	}
	// no description: the condition only; a long one is cut at 200 runes
	got, _ := rescuePrompt(Listing{Title: "Rice", NumberOfServing: 1, Details: &ListingDetails{MealCondition: "cooking_required"}}, ScanVision)
	if !strings.Contains(got, "(it can be wrong; the photo decides): the food needs cooking.\nLook at the photo.") {
		t.Fatalf("prompt\n%s", got)
	}
	// R1 review: bidi and zero-width marks (Unicode Cf) become spaces too
	got, _ = rescuePrompt(Listing{Title: "X", NumberOfServing: 1, Details: &ListingDetails{MealCondition: "sealed",
		Description: "Beans\u202e evil\u2066x\u200by"}}, ScanVision)
	if !strings.Contains(got, "described it as: Beans evil x y.\n") {
		t.Fatalf("format characters kept:\n%q", got)
	}
	long := strings.Repeat("é", 250)
	got, _ = rescuePrompt(Listing{Title: "X", NumberOfServing: 1, Details: &ListingDetails{MealCondition: "sealed", Description: long}}, ScanVision)
	if !strings.Contains(got, ": "+strings.Repeat("é", MaxHintDescription)+".\n") || strings.Contains(got, strings.Repeat("é", MaxHintDescription+1)) {
		t.Fatal("description not cut at 200 runes")
	}
}

// analyzeEvents writes the analyze events of the fixture's two scans (the
// vision scan of l_..0f, the barcode scan of l_..10) on the listings' UTC day.
func analyzeEvents(t *testing.T, e *rsEnv) {
	d := t.TempDir()
	day := e.now.Add(-31 * time.Minute).UTC().Format("20060102")
	lines := `{"service":"analyze","request_id":"` + strings.Repeat("ab", 16) + `","status":200,"user_id":"u1","source":"vision"}
{"service":"analyze","request_id":"` + strings.Repeat("cd", 16) + `","status":200,"user_id":"u1","source":"barcode"}
`
	if err := os.WriteFile(d+"/events-"+day+".jsonl", []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	e.cfg.AnalyzeEvents = d
}

// The handler sends the hint to Muse only for a vision scan, and records it
// and the route in the events line; a barcode scan and a listing with no
// details send none (hint "").
func TestRescueSendsTheScannerHint(t *testing.T) {
	e := newRS(t, stableDraft, 200, false)
	defer e.stop()
	analyzeEvents(t, e)
	for _, id := range []string{"l_000000000000000f", "l_000000000000000a", "l_0000000000000010"} {
		if w := do(e.h, jsonReq("POST", "/api/rescue/"+id, `{"zip":"10001"}`, "u1")); w.Code != 200 {
			t.Fatalf("%s: %d %s", id, w.Code, w.Body)
		}
	}
	text := func(i int) string {
		return (*e.muse)[i]["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	}
	if len(*e.muse) != 3 {
		t.Fatalf("muse calls %d", len(*e.muse))
	}
	if !strings.Contains(text(0), "Servings: 12\nScanner note, made when the food was listed (it can be wrong; the photo decides): "+
		"the food is in a sealed package; the scanner described it as: Twelve sealed cups of strawberry yogurt.\nLook at the photo.") {
		t.Fatalf("hint prompt\n%s", text(0))
	}
	if text(1) != fmt.Sprintf(RescuePrompt, "Canned beans", 24) {
		t.Fatalf("no-hint prompt changed\n%s", text(1))
	}
	if text(2) != fmt.Sprintf(RescuePrompt, "Chips", 5) {
		t.Fatalf("a barcode scan got a hint\n%s", text(2))
	}
	b, _ := os.ReadFile(e.cfg.Events.Path(e.now))
	var hints, routes []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var ev struct {
			Route  string         `json:"route"`
			Detail map[string]any `json:"detail"`
		}
		if json.Unmarshal([]byte(line), &ev) == nil && ev.Route == "rescue.create" {
			h, ok := ev.Detail["hint"].(string)
			if !ok {
				t.Fatalf("no hint in %s", line)
			}
			r, _ := ev.Detail["scan_source"].(string)
			hints, routes = append(hints, h), append(routes, r)
		}
	}
	if strings.Join(hints, ",") != "sealed,," || strings.Join(routes, ",") != "vision,,barcode" {
		t.Fatalf("event hints %q routes %q\n%s", hints, routes, b)
	}
}

// Contract: post decodes the details from the listings service's own Listing
// type (X48d), so a change there that breaks rescue fails here.
func TestListingDetailsDecodeFromTheListingsService(t *testing.T) {
	nv, w, mc := 1, 0.4, 3.5
	src := listings.Listing{ID: "l_000000000000000f", OwnerID: "u1", Title: "Yogurt", NumberOfServing: 12, Status: "active",
		Details: &listings.Details{Description: "Sealed cups", IsNonVeg: &nv, ServingType: "individual",
			MealCondition: "sealed", Weight: &w, MealCost: &mc}}
	b, err := json.Marshal(map[string]any{"items": []listings.Listing{src, {ID: "l_0000000000000010", Title: "No details"}}})
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Items []Listing `json:"items"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	if d := body.Items[0].Details; d == nil || d.MealCondition != "sealed" || d.Description != "Sealed cups" {
		t.Fatalf("details %+v", d)
	}
	if body.Items[1].Details != nil {
		t.Fatalf("a listing without details got %+v", body.Items[1].Details)
	}
}

// R1 review: a failed draft still records the hint, so F6 can see failures by hint.
func TestRescueFailureKeepsTheHint(t *testing.T) {
	e := newRS(t, stableDraft, 500, false)
	defer e.stop()
	analyzeEvents(t, e)
	if w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000f", `{"zip":"10001"}`, "u1")); w.Code != 502 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	b, _ := os.ReadFile(e.cfg.Events.Path(e.now))
	if !strings.Contains(string(b), `"route":"rescue.create"`) || !strings.Contains(string(b), `"hint":"sealed"`) {
		t.Fatalf("events %s", b)
	}
}

// scanSource reads the scan's own answer line: the listing's day and the day
// before, status 200 with a route, the same user; anything else is "".
func TestScanSource(t *testing.T) {
	d := t.TempDir()
	id, other := strings.Repeat("0f", 16), strings.Repeat("1e", 16)
	write := func(day, text string) {
		if err := os.WriteFile(d+"/events-"+day+".jsonl", []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// the day before: a failed try, then the answer (a scan late at night, posted after midnight UTC)
	write("20261027", `{"request_id":"`+id+`","status":503,"user_id":"u1"}`+"\n"+
		`{"request_id":"`+id+`","status":200,"user_id":"u1","source":"vision"}`+"\n"+
		`{"request_id":"`+other+`","status":200,"user_id":"u2","source":"vision"}`+"\n")
	// the listing's day: the label report of the same scan (204, no route) and a bad line
	write("20261028", `{"request_id":"`+id+`","status":204,"route":"labels","user_id":"u1"}`+"\n"+`{not json `+id+"\n")
	at := "2026-10-28T00:10:00Z"
	l := Listing{OwnerID: "u1", RequestID: id, CreatedAt: at}
	if got := scanSource(d, l); got != ScanVision {
		t.Fatalf("got %q", got)
	}
	for name, c := range map[string]struct {
		dir string
		l   Listing
	}{
		"no folder":           {"", l},
		"another user":        {d, Listing{OwnerID: "u1", RequestID: other, CreatedAt: at}},
		"not an id":           {d, Listing{OwnerID: "u1", RequestID: "../x", CreatedAt: at}},
		"no request id":       {d, Listing{OwnerID: "u1", CreatedAt: at}},
		"bad time":            {d, Listing{OwnerID: "u1", RequestID: id, CreatedAt: "yesterday"}},
		"two days later":      {d, Listing{OwnerID: "u1", RequestID: id, CreatedAt: "2026-10-29T12:00:00Z"}},
		"missing folder":      {d + "/nope", l},
		"empty user (bearer)": {d, Listing{OwnerID: "", RequestID: id, CreatedAt: at}},
	} {
		if got := scanSource(c.dir, c.l); got != "" {
			t.Fatalf("%s: got %q", name, got)
		}
	}
}
