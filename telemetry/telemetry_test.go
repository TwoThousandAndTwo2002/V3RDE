package telemetry

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// ---- /proc ----------------------------------------------------------------

// statOf is a /proc/<pid>/stat line (the field layout of proc_pid_stat(5))
// with a comm that has spaces and parentheses.
func statOf(utime, stime, start int) string {
	f := make([]string, 52)
	for i := range f {
		f[i] = "0"
	}
	f[0] = "R"                    // field 3
	f[14-3] = strconv.Itoa(utime) // field 14
	f[15-3] = strconv.Itoa(stime) // field 15
	f[22-3] = strconv.Itoa(start) // field 22
	f[16-3], f[20-3] = "-5", "12" // cutime and num_threads: not read
	return "1 (my svc) (x)) " + strings.Join(f, " ") + "\n"
}

func TestParseStat(t *testing.T) {
	ticks, start, err := ParseStat([]byte(statOf(700, 300, 98765)))
	if err != nil || ticks != 1000 || start != 98765 {
		t.Fatalf("%d %d %v", ticks, start, err)
	}
	for _, bad := range []string{"", "1 no-parens R 0", "1 (x) R 1 2 3", strings.Replace(statOf(1, 1, 1), " 1 1 ", " x 1 ", 1)} {
		if _, _, err := ParseStat([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestParseStatus(t *testing.T) {
	rss, th, err := ParseStatus([]byte("Name:\tsvc\nVmPeak:\t 9 kB\nVmRSS:\t   51200 kB\nThreads:\t7\nnot a key\nEmpty:\n"))
	if err != nil || rss != 51200 || th != 7 {
		t.Fatalf("%d %d %v", rss, th, err)
	}
	if rss, th, _ := ParseStatus([]byte("Name:\tkthread\n")); rss != 0 || th != 0 {
		t.Fatal("a missing key is 0")
	}
}

func fakeProc(t *testing.T, root string, pid, utime, start int, rssKiB int) {
	t.Helper()
	d := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "stat"), []byte(statOf(utime, 0, start)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "status"), []byte("VmRSS:\t"+strconv.Itoa(rssKiB)+" kB\nThreads:\t4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadProc(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, 1, 250, 4242, 3072)
	p, err := ReadProc(root, 1)
	if err != nil || p.CPUTicks != 250 || p.StartTime != 4242 || p.RSSKiB != 3072 || p.Threads != 4 {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := ReadProc(root, 2); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("no such pid: %v", err)
	}
	// the path is root + "/<pid>/stat" as written (it is in the error text of the spool)
	if _, err := ReadProc(root+"/", 2); err == nil || !strings.Contains(err.Error(), root+"//2/stat") {
		t.Fatalf("path form: %v", err)
	}
	if _, err := ReadProc("", 999999); err == nil || !strings.Contains(err.Error(), "/999999/stat") {
		t.Fatalf("root \"\" is the absolute /<pid>: %v", err)
	}
}

func TestCPUPercent(t *testing.T) {
	for _, tc := range []struct {
		prev, cur  uint64
		secs, want float64
	}{{0, 100, 1, 100}, {100, 150, 1, 50}, {0, 300, 2, 150}, {50, 10, 1, 0}, {0, 100, 0, 0}} {
		if got := CPUPercent(tc.prev, tc.cur, tc.secs); got != tc.want {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}

// ---- files ----------------------------------------------------------------

func TestReadNew(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events-20261028.jsonl")
	appendFile(t, p, "{\"n\":1}\n\n  {\"n\":2}  \r\n{\"n\":")
	lines, next, err := ReadNew(p, 0)
	if err != nil || len(lines) != 2 || string(lines[0].B) != `{"n":1}` || lines[0].Off != 0 ||
		string(lines[1].B) != `{"n":2}` || lines[1].Off != 9 || next != 22 {
		t.Fatalf("%+v %d %v", lines, next, err)
	}
	if again, n2, _ := ReadNew(p, next); len(again) != 0 || n2 != next {
		t.Fatal("a part line must wait")
	}
	appendFile(t, p, "3}\n")
	lines, next, _ = ReadNew(p, next)
	if len(lines) != 1 || string(lines[0].B) != `{"n":3}` || lines[0].Off != 22 || next != 30 {
		t.Fatalf("%+v %d", lines, next)
	}
	os.WriteFile(p, []byte("{\"new\":1}\n"), 0o644) // replaced by a shorter file
	if lines, n3, _ := ReadNew(p, next); len(lines) != 1 || string(lines[0].B) != `{"new":1}` || n3 != 10 {
		t.Fatalf("a replaced file starts again: %+v", lines)
	}
	if _, off, err := ReadNew(filepath.Join(t.TempDir(), "none"), 7); err == nil || off != 7 {
		t.Fatal("a missing file is an error and keeps the offset")
	}
}

func TestOffsets(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "offsets.json")
	o, err := LoadOffsets(p)
	if err != nil || o.Get("a") != 0 {
		t.Fatal(err)
	}
	o.Set("events-20261028.jsonl", 123)
	o.Set(startKey, 9)
	if err := o.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != `{"offsets":{"@service_start_ticks":9,"events-20261028.jsonl":123}}` {
		t.Fatalf("file %s", b)
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("a temp file was left")
	}
	o2, err := LoadOffsets(p)
	if err != nil || o2.Get("events-20261028.jsonl") != 123 || o2.Get(startKey) != 9 {
		t.Fatalf("reload %v", err)
	}
	os.WriteFile(p, []byte(`{"offsets":null}`), 0o644)
	if o3, err := LoadOffsets(p); err != nil || o3.M == nil {
		t.Fatal("null offsets are an empty map")
	}
	os.WriteFile(p, []byte(`{broken`), 0o644)
	if _, err := LoadOffsets(p); err == nil {
		t.Fatal("a broken file is an error")
	}
}

func TestFilesAndAppender(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"events-20261028.jsonl", "events-20261027.jsonl", "offsets.json", "x.jsonl.tmp"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o644)
	}
	got := Files(dir)
	if len(got) != 2 || filepath.Base(got[0]) != "events-20261027.jsonl" {
		t.Fatalf("%v", got)
	}
	a := Appender{Dir: dir, Prefix: "ledger"}
	line := make([]byte, 7, 16) // room after the end: an append there would change the caller's array
	copy(line, `{"a":1}`)
	a.Write("20261028", line)
	a.Write("20261028", []byte(`{"a":2}`))
	a.Write("20261029", []byte(`{"a":3}`))
	a.Close()
	if line[:8][7] == '\n' {
		t.Fatal("Write changed the caller's bytes")
	}
	if l := readLines(t, filepath.Join(dir, "ledger-20261028.jsonl")); len(l) != 2 || l[1] != `{"a":2}` {
		t.Fatalf("%v", l)
	}
	if l := readLines(t, filepath.Join(dir, "ledger-20261029.jsonl")); len(l) != 1 {
		t.Fatalf("%v", l)
	}
}

// ---- the sidecar ----------------------------------------------------------

type rig struct {
	events, spool, proc string
	now                 time.Time
}

func newRig(t *testing.T) *rig {
	return &rig{events: t.TempDir(), spool: t.TempDir(), proc: t.TempDir(), now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
}

func (g *rig) sidecar(t *testing.T) *Sidecar {
	t.Helper()
	s := &Sidecar{Service: "listings", Build: "abc1234", EventsDir: g.events, SpoolDir: g.spool,
		ProcRoot: g.proc, PID: 1, Now: func() time.Time { return g.now }}
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	return s
}

func spool(t *testing.T, g *rig, kind string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(g.spool, kind+"-20261028.jsonl"))
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("bad spool line %q", l)
		}
		out = append(out, m)
	}
	return out
}

func kinds(lines []map[string]any, kind string) []map[string]any {
	var out []map[string]any
	for _, l := range lines {
		if l["kind"] == kind {
			out = append(out, l)
		}
	}
	return out
}

func TestSidecarShips(t *testing.T) {
	g := newRig(t)
	appendFile(t, filepath.Join(g.events, "events-20261027.jsonl"), "{\"trace_id\":\"t0\"}\n")
	appendFile(t, filepath.Join(g.events, "events-20261028.jsonl"), "{\"trace_id\":\"t1\"}\nplain text\n{\"trace_id\":")
	s := g.sidecar(t)
	if n, err := s.Ship(); err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
	if n, _ := s.Ship(); n != 0 {
		t.Fatal("shipped again")
	}
	s.Close()
	ev := kinds(spool(t, g, "events"), "event")
	if len(ev) != 3 {
		t.Fatalf("%d lines", len(ev))
	}
	first, plain := ev[0], ev[2]
	line, _ := first["line"].(map[string]any)
	if first["service"] != "listings" || first["build"] != "abc1234" || first["file"] != "events-20261027.jsonl" ||
		first["offset"] != 0.0 || line["trace_id"] != "t0" || first["shipped_at"] != "2026-10-28T12:00:00Z" {
		t.Fatalf("%v", first)
	}
	if pl, _ := plain["line"].(map[string]any); pl["unparsed"] != "plain text" || plain["offset"] != 18.0 {
		t.Fatalf("%v", plain)
	}
	// a new sidecar process goes on from the saved offsets
	appendFile(t, filepath.Join(g.events, "events-20261028.jsonl"), "\"t2\"}\n")
	s2 := g.sidecar(t)
	if n, err := s2.Ship(); err != nil || n != 1 {
		t.Fatalf("resume %d %v", n, err)
	}
	s2.Close()
	if ev := kinds(spool(t, g, "events"), "event"); len(ev) != 4 || ev[3]["offset"] != 29.0 {
		t.Fatalf("%v", ev[len(ev)-1])
	}
}

func TestSidecarNoEventsYet(t *testing.T) {
	g := newRig(t)
	g.events = filepath.Join(g.events, "not-made-yet")
	s := g.sidecar(t)
	if n, err := s.Ship(); err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
}

func TestSidecarSamples(t *testing.T) {
	g := newRig(t)
	fakeProc(t, g.proc, 1, 1000, 500, 10240)
	s := g.sidecar(t)
	first := s.SampleOnce()
	if first.CPUPct != 0 || first.RSSMiB != 10 || first.Threads != 4 || first.Build != "abc1234" || first.Error != "" {
		t.Fatalf("%+v", first)
	}
	g.now = g.now.Add(2 * time.Second)
	fakeProc(t, g.proc, 1, 1050, 500, 11000)
	second := s.SampleOnce()
	if second.CPUPct != 25 || second.RSSMiB != 10.74 {
		t.Fatalf("%+v", second)
	}
	s.Close()
	if sm := spool(t, g, "samples"); len(sm) != 2 || sm[1]["cpu_pct"] != 25.0 || sm[1]["kind"] != "sample" {
		t.Fatalf("%v", sm)
	}
}

func TestSidecarLifecycle(t *testing.T) {
	g := newRig(t)
	fakeProc(t, g.proc, 1, 100, 500, 1024)
	s := g.sidecar(t)
	s.SampleOnce() // started
	s.SampleOnce() // nothing new
	os.RemoveAll(filepath.Join(g.proc, "1"))
	if smp := s.SampleOnce(); smp.Error == "" {
		t.Fatal("a missing process is an error sample")
	}
	s.SampleOnce() // still down: no second line
	fakeProc(t, g.proc, 1, 5, 900, 1024)
	if smp := s.SampleOnce(); smp.CPUPct != 0 { // up, and a new process: no CPU across it
		t.Fatalf("%+v", smp)
	}
	s.Close()
	// the sidecar restarts; the service did not: no new lifecycle line
	s2 := g.sidecar(t)
	s2.SampleOnce()
	// the service restarts while the sidecar is up
	fakeProc(t, g.proc, 1, 1, 1200, 1024)
	s2.SampleOnce()
	s2.Close()
	var got []string
	for _, l := range kinds(spool(t, g, "events"), "lifecycle") {
		got = append(got, l["event"].(string))
	}
	want := "service_started service_down service_up service_restarted service_restarted"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %q\nwant %q", strings.Join(got, " "), want)
	}
	lc := kinds(spool(t, g, "events"), "lifecycle")
	if lc[3]["prev_start_ticks"] != 500.0 || lc[3]["start_ticks"] != 900.0 || lc[4]["prev_start_ticks"] != 900.0 {
		t.Fatalf("%v %v", lc[3], lc[4])
	}
	if lc[1]["error"] == nil || lc[1]["prev_start_ticks"] != 500.0 {
		t.Fatalf("down line %v", lc[1])
	}
}

func TestSidecarRunStops(t *testing.T) {
	g := newRig(t)
	fakeProc(t, g.proc, 1, 1, 1, 1024)
	s := g.sidecar(t)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { s.Run(5*time.Millisecond, stop); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(filepath.Join(g.spool, "samples-"+time.Now().UTC().Format("20060102")+".jsonl"))
		if strings.Count(string(b), "\n") >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
	s.Close()
	if len(spool(t, g, "samples")) < 2 {
		t.Fatal("Run did not sample")
	}
}

// The rig's clock is not the real one in Run: Run reads s.Now, which is the
// rig clock (fixed at 2026-10-28), so the samples go to that day's file.

func TestRound2(t *testing.T) {
	for in, want := range map[float64]float64{0: 0, 1.004: 1, 1.006: 1.01, 1.0049: 1, 10.7421875: 10.74, 99.999: 100,
		1e15: 1e15} {
		if got := round2(in); got != want {
			t.Errorf("%v: %v", in, got)
		}
	}
}

// ---- the ledger -----------------------------------------------------------

func TestLedgerMerges(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	for _, svc := range []string{"gate", "listings"} {
		os.MkdirAll(filepath.Join(root, svc), 0o755)
	}
	appendFile(t, filepath.Join(root, "gate", "events-20261028.jsonl"), "{\"kind\":\"event\",\"service\":\"gate\"}\n{\"part")
	appendFile(t, filepath.Join(root, "gate", "samples-20261028.jsonl"), "{\"kind\":\"sample\"}\n")
	appendFile(t, filepath.Join(root, "listings", "events-20261027.jsonl"), "{\"kind\":\"event\",\"service\":\"listings\"}\n")
	appendFile(t, filepath.Join(root, "listings", "other.jsonl"), "{\"x\":1}\n")
	l := &Ledger{SpoolRoot: root, Dir: dir}
	if err := l.Open(); err != nil {
		t.Fatal(err)
	}
	if n, err := l.Merge(); err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
	if n, _ := l.Merge(); n != 0 {
		t.Fatal("merged again")
	}
	appendFile(t, filepath.Join(root, "gate", "events-20261028.jsonl"), "\":1}\n")
	l.Close()
	l2 := &Ledger{SpoolRoot: root, Dir: dir} // a restart goes on from the saved positions
	if err := l2.Open(); err != nil {
		t.Fatal(err)
	}
	if n, _ := l2.Merge(); n != 1 {
		t.Fatalf("resume %d", n)
	}
	l2.Close()
	if lines := readLines(t, filepath.Join(dir, "ledger-20261028.jsonl")); len(lines) != 2 || lines[1] != `{"part":1}` {
		t.Fatalf("%v", lines)
	}
	if lines := readLines(t, filepath.Join(dir, "ledger-20261027.jsonl")); len(lines) != 1 {
		t.Fatalf("the day is the spool file's day: %v", lines)
	}
	if lines := readLines(t, filepath.Join(dir, "samples-20261028.jsonl")); len(lines) != 1 {
		t.Fatalf("%v", lines)
	}
	if _, err := os.Stat(filepath.Join(dir, "other-20261028.jsonl")); !os.IsNotExist(err) {
		t.Fatal("a file that is not events or samples was merged")
	}
}

func ledgerWith(t *testing.T, days map[string]string) *Ledger {
	t.Helper()
	dir := t.TempDir()
	for day, text := range days {
		appendFile(t, filepath.Join(dir, "ledger-"+day+".jsonl"), text)
	}
	l := &Ledger{SpoolRoot: t.TempDir(), Dir: dir}
	if err := l.Open(); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestTrace(t *testing.T) {
	l := ledgerWith(t, map[string]string{
		"20261028": `{"kind":"event","service":"inference","line":{"trace_id":"scan-1","analyze_request_id":"aaaa"}}` + "\n" +
			`{"kind":"event","service":"gate","line":{"trace_id":"scan-2"}}` + "\n" +
			`{"kind":"event","service":"analyze","line":{"request_id":"aaaa"}}` + "\n" +
			`{"kind":"event","service":"listings","line":{"request_id":"aaaa"}}` + "\n",
		"20261027": `{"kind":"event","service":"barcode","line":{"trace_id":"scan-1"}}` + "\n",
	})
	got, err := l.Trace("scan-1", []string{"20261028", "20261027", "20261026"})
	if err != nil || len(got) != 3 {
		t.Fatalf("%d %v", len(got), err)
	}
	if !strings.Contains(string(got[0]), `"inference"`) || !strings.Contains(string(got[1]), `"barcode"`) ||
		!strings.Contains(string(got[2]), `"analyze"`) {
		t.Fatalf("order: %s", got)
	}
	if got, _ := l.Trace("scan-2", []string{"20261028"}); len(got) != 1 {
		t.Fatalf("%s", got)
	}
	if got, _ := l.Trace("none", []string{"20261028"}); got != nil {
		t.Fatal("no match is nil")
	}
}

func TestLedgerHandler(t *testing.T) {
	today := time.Date(2026, 10, 28, 23, 59, 59, 0, time.UTC)
	l := ledgerWith(t, map[string]string{
		today.Format("20060102"):                   `{"line":{"trace_id":"t-now"}}` + "\n",
		today.AddDate(0, 0, -1).Format("20060102"): `{"line":{"trace_id":"t-now","n":2}}` + "\n",
		today.AddDate(0, 0, -2).Format("20060102"): `{"line":{"trace_id":"t-now","n":3}}` + "\n",
	})
	l.now = func() time.Time { return today } // one clock for the test and the handler (no midnight race)
	h := l.Handler("b9")
	get := func(path string) (int, map[string]any) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		var m map[string]any
		json.Unmarshal(w.Body.Bytes(), &m)
		return w.Code, m
	}
	if code, m := get("/v1/trace/t-now"); code != 200 || m["trace_id"] != "t-now" || len(m["events"].([]any)) != 2 {
		t.Fatalf("today and yesterday only: %d %v", code, m)
	}
	if code, m := get("/v1/trace/bad%20id"); code != 400 || m["error"] != "bad trace id" {
		t.Fatalf("%d %v", code, m)
	}
	if code, m := get("/v1/trace/none"); code != 200 || m["events"] != nil {
		t.Fatalf("%d %v", code, m)
	}
	if code, m := get("/healthz"); code != 200 || m["service"] != "ledger" || m["build"] != "b9" || m["merged"] != 0.0 {
		t.Fatalf("%d %v", code, m)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/trace/t-now", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST %d", w.Code)
	}
}

// One spool file that cannot be read does not stop the merge of the others.
func TestLedgerSkipsAnUnreadableFile(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(root, "a", "events-20261028.jsonl"), 0o755) // a folder with a file name
	os.MkdirAll(filepath.Join(root, "b"), 0o755)
	appendFile(t, filepath.Join(root, "b", "events-20261028.jsonl"), "{\"ok\":1}\n")
	l := &Ledger{SpoolRoot: root, Dir: dir}
	if err := l.Open(); err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if n, err := l.Merge(); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
}

// A ledger line that is not JSON is not in a trace answer, so the answer is
// still a whole JSON body.
func TestTraceSkipsBrokenLines(t *testing.T) {
	l := ledgerWith(t, map[string]string{
		"20261028": `{"line":{"trace_id":"t1"}}` + "\n" + `{"line":{"trace_id":"t1"` + "\n" + `{"line":{"trace_id":"t1","n":2}}` + "\n",
	})
	l.now = func() time.Time { return time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC) }
	got, _ := l.Trace("t1", []string{"20261028"})
	if len(got) != 2 {
		t.Fatalf("%s", got)
	}
	w := httptest.NewRecorder()
	l.Handler("b").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/trace/t1", nil))
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil || len(m["events"].([]any)) != 2 {
		t.Fatalf("body %q %v", w.Body, err)
	}
}

// The offsets are saved after the lines are on the disk; a save leaves a
// whole file (no temp file).
func TestShipSavesOffsetsAfterTheLines(t *testing.T) {
	g := newRig(t)
	appendFile(t, filepath.Join(g.events, "events-20261028.jsonl"), "{\"a\":1}\n")
	s := g.sidecar(t)
	if _, err := s.Ship(); err != nil {
		t.Fatal(err)
	}
	s.Close()
	o, err := LoadOffsets(filepath.Join(g.spool, "offsets.json"))
	if err != nil || o.Get("events-20261028.jsonl") != 8 {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(filepath.Join(g.spool, "offsets.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("temp file left")
	}
	if ev := kinds(spool(t, g, "events"), "event"); len(ev) != 1 {
		t.Fatal("the line is not in the spool")
	}
}
