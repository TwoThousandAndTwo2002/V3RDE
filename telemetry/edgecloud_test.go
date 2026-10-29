package telemetry

// X44d: the edge vs cloud report from the ledger.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatOf(t *testing.T) {
	if s := StatOf([]float64{5, 1, 3, 2, 4}); s != (Stat{N: 5, Median: 3, P90: 5, Max: 5}) {
		t.Fatal(s)
	}
	if s := StatOf([]float64{4, 1, 3, 2}); s != (Stat{N: 4, Median: 2, P90: 4, Max: 4}) { // nearest rank: ceil(0.5*4) = 2
		t.Fatal(s)
	}
	if s := StatOf(nil); s != (Stat{}) {
		t.Fatal(s)
	}
}

// shipped is a ledger line as the sidecar writes it (the Shipped struct), so
// the test also checks the field names the report reads.
func shipped(t *testing.T, service string, line any) []byte {
	raw, _ := json.Marshal(line)
	b, err := json.Marshal(Shipped{Kind: "event", Service: service, Build: "b", ShippedAt: "x", File: "events-20261029.jsonl", Line: raw})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sample(t *testing.T, service string, rss float64) []byte {
	b, _ := json.Marshal(Sample{Kind: "sample", Service: service, RSSMiB: rss})
	return b
}

func row(t *testing.T, r Report, source string) Row {
	t.Helper()
	for _, x := range r.Rows {
		if x.Source == source {
			return x
		}
	}
	t.Fatalf("no row %s", source)
	return Row{}
}

func testLines(t *testing.T) [][]byte {
	call := func(p string, st int, ms float64) map[string]any {
		return map[string]any{"purpose": p, "status": st, "ms": ms}
	}
	return [][]byte{
		shipped(t, "gate", map[string]any{"step": "gate", "status": 200, "total_ms": 40.5, "invoke_ms": 30}),
		shipped(t, "gate", map[string]any{"step": "gate", "status": 200, "total_ms": 60, "invoke_ms": 44}),
		shipped(t, "gate", map[string]any{"step": "gate", "status": 500, "total_ms": 9000}), // a failure: not a time
		shipped(t, "barcode", map[string]any{"step": "barcode", "status": 200, "total_ms": 2.5}),
		shipped(t, "analyze", map[string]any{"step": "analyze", "status": 200, "source": "vision", "total_ms": 14000, "cost_usd": 0.043,
			"calls": []any{call("sam:food", 200, 3000), call("sam:package", 200, 3200), call("sam:food tray", 500, 100),
				call("muse:whole:early", 200, 2600), call("muse:single", 200, 9000)}}),
		shipped(t, "analyze", map[string]any{"step": "analyze", "status": 200, "source": "barcode", "total_ms": 80}),
		shipped(t, "analyze", map[string]any{"step": "analyze", "status": 422, "code": "not_food", "total_ms": 300}),
		shipped(t, "touchup", map[string]any{"route": "touchup.job", "status": 200,
			"calls": []any{call("touchup:edit", 200, 18000), call("touchup:check", 200, 4000)}}),
		shipped(t, "voice", map[string]any{"status": 200, "calls": []any{call("voice:transcribe", 200, 900), call("voice:map", 200, 3100)}}),
		sample(t, "gate", 180.5), sample(t, "gate", 210), sample(t, "barcode", 40), sample(t, "analyze", 300),
		[]byte(`{"kind":"lifecycle","service":"gate","event":"service_started"}`),
		[]byte(`not json`),
	}
}

func TestSummarize(t *testing.T) {
	r := Summarize(testLines(t))
	if r.Lines != 15 || r.Bad != 1 {
		t.Fatalf("lines %d bad %d", r.Lines, r.Bad)
	}
	for src, want := range map[string]Stat{
		"gate total_ms":                   {N: 2, Median: 40.5, P90: 60, Max: 60},
		"gate invoke_ms":                  {N: 2, Median: 30, P90: 44, Max: 44},
		"barcode total_ms":                {N: 1, Median: 2.5, P90: 2.5, Max: 2.5},
		"gate sample rss_mib":             {N: 2, Median: 180.5, P90: 210, Max: 210},
		"barcode sample rss_mib":          {N: 1, Median: 40, P90: 40, Max: 40},
		"analyze calls sam:*":             {N: 2, Median: 3000, P90: 3200, Max: 3200}, // the 500 is not counted
		"analyze calls muse:*":            {N: 2, Median: 2600, P90: 9000, Max: 9000},
		"touchup calls touchup:edit":      {N: 1, Median: 18000, P90: 18000, Max: 18000},
		"touchup calls touchup:check":     {N: 1, Median: 4000, P90: 4000, Max: 4000},
		"voice calls voice:transcribe":    {N: 1, Median: 900, P90: 900, Max: 900},
		"voice calls voice:map":           {N: 1, Median: 3100, P90: 3100, Max: 3100},
		"rescue calls rescue:draft":       {},
		"analyze total_ms source barcode": {N: 1, Median: 80, P90: 80, Max: 80},
		"analyze total_ms source vision":  {N: 1, Median: 14000, P90: 14000, Max: 14000},
		"analyze cost_usd source vision":  {N: 1, Median: 0.043, P90: 0.043, Max: 0.043},
	} {
		if got := row(t, r, src).Stat; got != want {
			t.Errorf("%s: %+v, want %+v", src, got, want)
		}
	}
	if row(t, r, "gate total_ms").Part != "edge" || row(t, r, "analyze calls sam:*").Part != "cloud" {
		t.Fatal("parts")
	}
}

func TestEdgeCloudFiles(t *testing.T) {
	dir := t.TempDir()
	ls := testLines(t)
	write := func(name string, lines [][]byte) {
		var b strings.Builder
		for _, l := range lines {
			b.Write(l)
			b.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ledger-20261029.jsonl", ls[:9])
	write("samples-20261029.jsonl", ls[9:13])
	write("ledger-20261030.jsonl", ls[:1])
	write("offsets.json", [][]byte{[]byte(`{}`)}) // not a day file: not read
	all, err := EdgeCloud(dir, nil)
	if err != nil || strings.Join(all.Days, ",") != "20261029,20261030" || row(t, all, "gate total_ms").N != 3 || all.Bad != 0 {
		t.Fatalf("all: %v %+v", err, all)
	}
	one, err := EdgeCloud(dir, []string{"20261029", "20261031"})
	if err != nil || strings.Join(one.Days, ",") != "20261029" || row(t, one, "gate total_ms").N != 2 || row(t, one, "gate sample rss_mib").N != 2 {
		t.Fatalf("one day: %v %+v", err, one)
	}
	md := one.Markdown()
	for _, want := range []string{"| Where | Part | n |", "| edge | Food gate, PE-Core-S16-384 on the box (model only) | 2 | 30 ms | 44 ms | 44 ms |",
		"| cloud | Muse Spark 1.3: rescue draft | 0 | - | - | - |", "$0.0430", "210.0 MiB"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown has no %q:\n%s", want, md)
		}
	}
}
