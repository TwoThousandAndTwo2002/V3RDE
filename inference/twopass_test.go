package inference

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// SAM "food" and "package" run at the same time; a food box inside a
// package box joins the package; loose food stays.
func TestPackageConceptMergesPicturesIntoTheirPackage(t *testing.T) {
	f := &fakeAPI{sam: func(c string) (int, string) {
		if c == "package" {
			return 200, samLine(t, [4]int{0, 0, 299, 299}, [4]int{400, 0, 599, 299})
		}
		// a picture on package 1, the same object as package 2, a loose apple
		return 200, samLine(t, [4]int{50, 50, 149, 149}, [4]int{402, 2, 599, 299}, [4]int{650, 400, 749, 499})
	}, muse: func(p string, _ map[string]any) map[string]any {
		return textResp("m", itemsText(ent(1, nil, "Spaghetti box"), ent(2, nil, "Rice box"), ent(3, nil, "Apple")))
	}}
	e, done := engine(t, f)
	defer done()
	e.PackageConcept = "package"
	parallel := syncSAM(f) // both SAM calls must be in flight together
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Packages != 2 || inv.Contained != 2 || inv.Kept != 3 || len(inv.Items) != 3 {
		t.Fatalf("packages %d contained %d kept %d items %d", inv.Packages, inv.Contained, inv.Kept, len(inv.Items))
	}
	if !*parallel {
		t.Fatal("the two SAM calls were not in flight at the same time")
	}
}

// syncSAM holds each of the 2 SAM calls until the other one has arrived
// (or 2 s pass). The returned flag is true only if both were in flight
// together.
func syncSAM(f *fakeAPI) *bool {
	inner := f.sam
	var mu sync.Mutex
	arrived := 0
	both := make(chan struct{})
	ok := new(bool)
	f.sam = func(c string) (int, string) {
		mu.Lock()
		arrived++
		if arrived == 2 {
			*ok = true
			close(both)
		}
		mu.Unlock()
		select {
		case <-both:
		case <-time.After(2 * time.Second):
		}
		return inner(c)
	}
	return ok
}

// food:false is not deleted: it is kept as "not listed", outside the items
// and the count.
func TestNotFoodIsNotListedNotDeleted(t *testing.T) {
	f := &fakeAPI{sam: func(string) (int, string) {
		return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{300, 0, 499, 299})
	}, muse: func(string, map[string]any) map[string]any {
		towels := ent(2, nil, "Paper towels")
		towels["food"] = false
		soup := ent(1, nil, "Soup")
		soup["food"] = true
		return textResp("m", itemsText(soup, towels))
	}}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Items) != 1 || inv.Items[0].Name != "Soup" || len(inv.NotListed) != 1 || inv.NotListed[0].Name != "Paper towels" ||
		inv.Units() != 1 || len(inv.Rejected) != 0 {
		t.Fatalf("%+v | %+v", inv.Items, inv.NotListed)
	}
}

// Marks only: the text has no coordinates, only the count of tags.
func TestMarksOnlyPromptHasNoCoordinates(t *testing.T) {
	b := []Instance{{Left: 0, Top: 0, Right: 100, Bottom: 100, W: 800, H: 600}, {Left: 200, Top: 0, Right: 300, Bottom: 100, W: 800, H: 600}}
	p := MultiPrompt(b, true, false)
	if strings.Contains(p, "1: [") || !strings.Contains(p, "number from 1 to 2") || !strings.Contains(p, "0-1000 grid") {
		t.Fatal(p)
	}
	if n := NamePrompt(b, true, false); strings.Contains(n, "[0, 0,") {
		t.Fatal(n)
	}
}

// Two passes: names for every box (pass 1, its own effort), then the
// details ONCE per group (pass 2, its own effort), image first both times.
func TestTwoPassNamesThenDetailsPerGroup(t *testing.T) {
	var mu sync.Mutex
	var efforts, prompts []string
	var firstImg []string
	f := &fakeAPI{sam: func(string) (int, string) {
		return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{200, 0, 399, 299}, [4]int{500, 300, 599, 399})
	}}
	f.muse = func(p string, body map[string]any) map[string]any {
		mu.Lock()
		defer mu.Unlock()
		r, _ := body["reasoning"].(map[string]any)
		efforts = append(efforts, fmt.Sprint(r["effort"]))
		prompts = append(prompts, p)
		content := body["input"].([]any)[0].(map[string]any)["content"].([]any)
		firstImg = append(firstImg, fmt.Sprint(content[0].(map[string]any)["type"]))
		if strings.Contains(p, "name the item") {
			return textResp("n", `{"items":[{"box":1,"name":"Cheerios","point":[100,200]},{"box":2,"same_as":1,"name":"Cheerios"},{"box":3,"name":"Honey Maid"}]}`)
		}
		g1, g2 := contract("Cheerios"), contract("Honey Maid graham crackers")
		g1["group"], g2["group"] = 1.0, 2.0
		return textResp("d", itemsTextKey("groups", g1, g2))
	}
	e, done := engine(t, f)
	defer done()
	e.TwoPass, e.NameEffort, e.DetailEffort, e.Marks = true, "minimal", "low", true
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if p := purposes(inv); strings.Join(p, ",") != "sam:food,muse:names,muse:details" {
		t.Fatal(p)
	}
	if strings.Join(efforts, ",") != "minimal,low" || strings.Join(firstImg, ",") != "input_image,input_image" {
		t.Fatal(efforts, firstImg)
	}
	if !strings.Contains(prompts[1], `group 1: "Cheerios", 2 unit(s), tag(s) 1, 2`) || strings.Contains(prompts[1], "group 3") {
		t.Fatal(prompts[1])
	}
	if len(inv.Items) != 2 || inv.Items[0].Count != 2 || inv.Items[0].NumberOfServing != 2 || inv.Items[0].MealCondition != "sealed" ||
		inv.Items[1].Name != "Honey Maid graham crackers" || inv.Points.Hit != 1 || inv.Points.Missing != 2 {
		t.Fatalf("%+v %+v", inv.Items, inv.Points)
	}
}

func itemsTextKey(key string, entries ...map[string]any) string {
	return strings.Replace(itemsText(entries...), `{"items":`, `{"`+key+`":`, 1)
}

// Every box not food: an answer (NotFood, boxes kept), not an error.
func TestAllNotFoodIsAnAnswer(t *testing.T) {
	f := &fakeAPI{sam: func(string) (int, string) {
		return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{300, 0, 499, 299})
	}, muse: func(string, map[string]any) map[string]any {
		a, b := ent(1, nil, "Dog treats"), ent(2, nil, "Dog food")
		a["food"], b["food"] = false, false
		return textResp("m", itemsText(a, b))
	}}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if _, ok := inv.TopLevel(); err != nil || !inv.NotFood || ok || len(inv.NotListed) != 2 || len(inv.Items) != 0 {
		t.Fatal(err, inv.NotFood, ok, len(inv.NotListed))
	}
}
