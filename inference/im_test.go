package inference

import "testing"

// IM review (the items merge): the learn report (LR1) and the items work (I1, I4).

// I1: each catalog item of a photo with many codes is a barcode item in the
// scan's answer line, also when the scanner cannot answer.
func TestIMManyBarcodesInTheAnswerLine(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	if w, _ := s.do(reqCodes(t, "u1", "", []CodeBox{at(codeMarinara, 700, 150), at(codeHershey, 400, 500), {Code: codeMiss}})); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	ev := s.events(t)[0]
	if len(ev.Answer) != 3 || ev.Answer[0].Via != SourceBarcode || ev.Answer[1].Via != SourceBarcode || ev.Answer[2].Via == SourceBarcode || ev.Answer[2].Via == "" {
		t.Fatalf("answer %+v", ev.Answer)
	}
	n := newSvc(t, threeBoxes(t), "NOT_FOOD", 0, nil)
	if w, _ := n.do(reqCodes(t, "u1", "", []CodeBox{at(codeMarinara, 100, 100), at(codeHershey, 700, 100)})); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if ev := n.events(t)[0]; len(ev.Answer) != 2 || ev.Answer[0].Via != SourceBarcode || ev.Answer[1].Via != SourceBarcode {
		t.Fatalf("answer %+v", ev.Answer)
	}
}

// I4: an item the find service added (a "found" edit) is "found" in the
// report, not "<source> (before LR1)".
func TestIMLearnFoundItems(t *testing.T) {
	lines := [][]byte{
		jline(t, scanLine("f", SourceVision, 0.01, 4000, []EventCall{museCall("muse:whole", "analyze.whole", "1")},
			[]EventItem{{Item: 1, Via: "whole"}})),
		jline(t, labelLine("f", "2026-11-02T10:00:00Z", "posted", []LabelItem{
			{Item: 1, ModelTitle: "Soup", FinalTitle: "Soup", Kept: true},
			{Item: 2, ModelTitle: "Rice", FinalTitle: "Rice", Kept: true}},
			[]LabelEdit{{Item: 2, Field: "found", To: "Rice", How: "text"}})),
	}
	v := map[string]LearnTally{}
	for _, x := range Learn(lines, 96).ByVia {
		v[x.Group] = x
	}
	if v["whole"].Items != 1 || v["found"].Items != 1 || v[SourceVision+" (before LR1)"].Items != 0 {
		t.Fatalf("by via %+v", v)
	}
}
