// Command ie-probe runs inference_engine (mode auto v2) on a folder of photos
// against the live Meta Model API and writes a report: the food gate's
// verdict, what SAM found, what each variant answered, every call's latency,
// tokens and cost, the numbered-box test, and overlay PNGs. Bench only.
//
//	go run ./cmd/ie-probe -photos <dir> -out <dir> -gate-url http://127.0.0.1:8094/v1/gate
//
// Variants share one SAM answer per photo (SAMCache), so they differ only in
// the Muse call. A photo the gate calls not-food is not sent to the API.
// The key is read from -key-file (default ~/.config/morpheus/meta_model_api_key)
// and is never printed.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/TwoThousandAndTwo2002/Icarus/inference"
)

var palette = []color.RGBA{{230, 25, 75, 255}, {60, 180, 75, 255}, {0, 130, 200, 255}, {245, 130, 48, 255},
	{145, 30, 180, 255}, {70, 240, 240, 255}, {240, 50, 230, 255}, {210, 245, 60, 255}}

type variant struct {
	Name         string `json:"name"` // coords:low | marksonly:2pass:minimal:low
	Marks        bool   `json:"marks"`
	NoCoords     bool   `json:"no_coords"`
	Effort       string `json:"effort"`
	TwoPass      bool   `json:"two_pass"`
	NameEffort   string `json:"name_effort,omitempty"`
	DetailEffort string `json:"detail_effort,omitempty"`
	Split        int    `json:"split,omitempty"`
	Compact      bool   `json:"compact,omitempty"` // X31b: duplicates give box, point, same_as only
	// App: the photo is first made like the app's upload (mobile/src/photo.ts:
	// upright, long edge <= 1600 px, JPEG 0.85), the camera's own 0.7 step
	// cannot be copied off the phone.
	App bool `json:"app,omitempty"`
	// Early (X32c): the whole-photo call starts with SAM; 0 or 1 box uses it.
	Early bool `json:"early,omitempty"`
}

// parseVariant reads <coords|marks|marksonly>:<effort> or
// <coords|marks|marksonly>:2pass:<name effort>:<detail effort>.
func parseVariant(v string) (variant, error) {
	parts := strings.Split(strings.TrimSpace(v), ":")
	out := variant{Name: strings.TrimSpace(v)}
	switch parts[0] {
	case "coords":
	case "marks":
		out.Marks = true
	case "marksonly":
		out.Marks, out.NoCoords = true, true
	default:
		return out, fmt.Errorf("variant %q: kind must be coords, marks or marksonly", v)
	}
	if n := len(parts); n >= 3 && parts[n-1] == "early" {
		out.Early, parts = true, parts[:n-1]
	}
	if n := len(parts); n >= 3 && parts[n-1] == "compact" {
		out.Compact, parts = true, parts[:n-1]
	}
	if n := len(parts); n >= 3 && parts[n-1] == "app" {
		out.App, parts = true, parts[:n-1]
	}
	if n := len(parts); n >= 3 && strings.HasPrefix(parts[n-1], "split") {
		k, err := strconv.Atoi(strings.TrimPrefix(parts[n-1], "split"))
		if err != nil || k < 1 {
			return out, fmt.Errorf("variant %q: want splitN with N >= 1", v)
		}
		out.Split, parts = k, parts[:n-1]
	}
	switch {
	case len(parts) == 2:
		out.Effort = parts[1]
	case len(parts) == 4 && parts[1] == "2pass":
		out.TwoPass, out.NameEffort, out.DetailEffort, out.Effort = true, parts[2], parts[3], parts[3]
	default:
		return out, fmt.Errorf("variant %q: want <kind>:<effort> or <kind>:2pass:<name effort>:<detail effort>", v)
	}
	return out, nil
}

type gateOut struct {
	Verdict string  `json:"verdict"`
	Food    bool    `json:"food"`
	P       float64 `json:"p"`
	MS      struct {
		Total float64 `json:"total"`
	} `json:"ms"`
	Error  string `json:"error,omitempty"`
	Status int    `json:"status"`
	WallMS int64  `json:"wall_ms"`
}

type run struct {
	Photo   string               `json:"photo"`
	Variant string               `json:"variant"`
	Gate    *gateOut             `json:"gate,omitempty"`
	Error   string               `json:"error,omitempty"`
	Inv     *inference.Inventory `json:"inventory,omitempty"`
}

type agg struct {
	photos, listings, units, counted, calls, hit, miss, missing, notListed int
	cost                                                                   float64
	ms                                                                     int64
}

func main() {
	home, _ := os.UserHomeDir()
	keyFile := flag.String("key-file", filepath.Join(home, ".config/morpheus/meta_model_api_key"), "Meta Model API key file")
	photos := flag.String("photos", "", "folder of photos (jpg, png)")
	out := flag.String("out", "probe_out", "report folder")
	variants := flag.String("variants", "marks:low,marksonly:low,marksonly:2pass:minimal:low", "variants: <coords|marks|marksonly>:<effort> or <kind>:2pass:<name effort>:<detail effort>")
	pkg := flag.String("package-concept", "package", "SAM's second concept, run in parallel (\"\" = off)")
	tray := flag.String("tray-concept", "food tray", "SAM's third concept, run in parallel (\"\" = off)")
	only := flag.String("only", "", "comma-separated photo names to run (\"\" = all)")
	gateURL := flag.String("gate-url", "", "food gate /v1/gate URL (\"\" = no gate)")
	maxNamed := flag.Int("max-named", 24, "boxes named per photo")
	timeout := flag.Duration("timeout", 180*time.Second, "per photo and variant")
	apiURL := flag.String("api-url", inference.DefaultUpstream, "Responses endpoint (tests use a fake)")
	parallel := flag.Int("parallel", 4, "photos at the same time (1 = measure the wait; the local gate queues 8)")
	flag.Parse()
	kb, err := os.ReadFile(*keyFile)
	if err != nil || strings.TrimSpace(string(kb)) == "" {
		log.Fatalf("key file %s: %v", *keyFile, err)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	var vs []variant
	for _, v := range strings.Split(*variants, ",") {
		pv, err := parseVariant(v)
		if err != nil {
			log.Fatal(err)
		}
		vs = append(vs, pv)
	}
	if *gateURL != "" {
		h := strings.TrimSuffix(*gateURL, "/v1/gate") + "/healthz"
		resp, err := http.Get(h)
		if err != nil || resp.StatusCode != 200 {
			log.Fatalf("gate not ready at %s: %v", h, err)
		}
		hb, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("gate: %s\n", trim(strings.TrimSpace(string(hb)), 300))
	}
	files, _ := filepath.Glob(filepath.Join(*photos, "*"))
	sort.Slice(files, func(i, j int) bool { return natLess(files[i], files[j]) })
	cache := &inference.SAMCache{}
	key := strings.TrimSpace(string(kb))
	fmt.Printf("prices: Muse Spark 1.3 $%.2f/1M in, $%.2f/1M out, $%.2f/1M cached; SAM 3.1 $%.4f/image (%s)\n",
		inference.PriceMuseInputPerM, inference.PriceMuseOutputPerM, inference.PriceMuseCachedPerM, inference.PriceSAMPerImage, inference.PricesSource)
	fmt.Printf("image limit: %d tokens of %d px (measured 2026-10-26); %d photos at a time\n\n", inference.MaxImageTokens, inference.TokenPx, *parallel)

	// Photos run -parallel at a time (T9, 2026-10-27). Each photo's text is
	// printed in one piece when it ends; the report keeps the file order.
	// Measured times include the other photos' load: use -parallel 1 to
	// measure the wait.
	type photoOut struct {
		runs  []run
		text  string
		spent float64
		gate  string
	}
	results := make([]*photoOut, len(files))
	var mu sync.Mutex
	sum := map[string]*agg{}
	var wg sync.WaitGroup
	slots := make(chan struct{}, max(*parallel, 1))
	for fi, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		mime := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png"}[ext]
		name := filepath.Base(f)
		stem := strings.TrimSuffix(name, ext)
		if mime == "" || (*only != "" && !strings.Contains(","+*only+",", ","+name+",") && !strings.Contains(","+*only+",", ","+stem+",")) {
			continue
		}
		wg.Add(1)
		go func(fi int, f, ext, mime, name, stem string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			po := &photoOut{}
			var w strings.Builder
			defer func() {
				mu.Lock()
				results[fi] = po
				fmt.Print(w.String())
				mu.Unlock()
			}()
			img, err := os.ReadFile(f)
			if err != nil {
				fmt.Fprintf(&w, "== %s | read: %v\n", name, err)
				return
			}
			var g *gateOut
			if *gateURL != "" {
				g = callGate(*gateURL, name, img)
				po.gate = g.Verdict
				fmt.Fprintf(&w, "== %s | gate %s p=%.3f %.0f ms %s\n", name, g.Verdict, g.P, g.MS.Total, g.Error)
				if !g.Food {
					po.runs = append(po.runs, run{Photo: name, Variant: "gate", Gate: g})
					fmt.Fprintln(&w, "     not sent to the API ($0)")
					return
				}
			} else {
				fmt.Fprintf(&w, "== %s\n", name)
			}
			for _, v := range vs {
				var smu sync.Mutex
				e := &inference.Engine{API: &inference.Client{URL: *apiURL, Key: key, Effort: v.Effort,
					HTTP: &http.Client{Timeout: 120 * time.Second}}, Marks: v.Marks, NoCoords: v.NoCoords, MaxNamed: *maxNamed, SAMCache: cache,
					PackageConcept: *pkg, TrayConcept: *tray, Split: v.Split, Compact: v.Compact, EarlyWhole: v.Early, TwoPass: v.TwoPass, NameEffort: v.NameEffort, DetailEffort: v.DetailEffort,
					Sent: func(purpose string, jb []byte) {
						smu.Lock()
						defer smu.Unlock()
						_ = os.WriteFile(filepath.Join(*out, fmt.Sprintf("%s.%s.%s.sent.jpg", stem, strings.ReplaceAll(v.Name, ":", "-"), purpose)), jb, 0o644)
					}}
				in, inMime := img, mime
				if v.App {
					if b, err := appUpload(img); err == nil {
						in, inMime = b, "image/jpeg"
					} else {
						fmt.Fprintf(&w, "   %s: app version: %v\n", name, err)
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), *timeout)
				inv, err := e.Run(ctx, in, inMime, inference.ModeAuto)
				cancel()
				r := run{Photo: name, Variant: v.Name, Gate: g, Inv: inv}
				if err != nil {
					r.Error = err.Error()
				}
				po.runs = append(po.runs, r)
				po.spent += inv.SpentUSD()
				report(&w, r, inv)
				mu.Lock()
				a := sum[v.Name]
				if a == nil {
					a = &agg{}
					sum[v.Name] = a
				}
				a.photos++
				a.listings += inv.Listings()
				a.units += inv.Units()
				a.counted += inv.Counted()
				a.notListed += len(inv.NotListed)
				a.calls += len(inv.Calls)
				a.cost += inv.Cost
				a.ms += inv.MS
				a.hit, a.miss, a.missing = a.hit+inv.Points.Hit, a.miss+inv.Points.Miss, a.missing+inv.Points.Missing
				mu.Unlock()
				overlay(img, inv, filepath.Join(*out, fmt.Sprintf("%s.%s.png", stem, strings.ReplaceAll(v.Name, ":", "-"))))
			}
		}(fi, f, ext, mime, name, stem)
	}
	wg.Wait()
	var runs []run
	spent := 0.0
	gates := map[string]int{}
	for _, po := range results {
		if po == nil {
			continue
		}
		runs = append(runs, po.runs...)
		spent += po.spent
		if po.gate != "" {
			gates[po.gate]++
		}
	}
	if len(gates) > 0 {
		fmt.Printf("\ngate verdicts: %v\n", gates)
	}
	fmt.Printf("\n%-28s %6s %9s %6s %8s %7s %6s %10s %10s %10s %9s %13s\n", "variant", "photos", "listings", "named", "counted", "notfood", "calls",
		"total $", "$ / photo", "$ / named", "ms/photo", "points hit")
	for _, v := range vs {
		a := sum[v.Name]
		if a == nil || a.photos == 0 {
			continue
		}
		per := 0.0
		if a.units > 0 {
			per = a.cost / float64(a.units)
		}
		pts := a.hit + a.miss + a.missing
		rate := 0.0
		if pts > 0 {
			rate = 100 * float64(a.hit) / float64(pts)
		}
		fmt.Printf("%-28s %6d %9d %6d %8d %7d %6d %10.5f %10.5f %10.5f %9d %6d/%d %4.0f%%\n", v.Name, a.photos, a.listings, a.units, a.counted,
			a.notListed, a.calls, a.cost, a.cost/float64(a.photos), per, a.ms/int64(a.photos), a.hit, pts, rate)
	}
	fmt.Printf("(total $ = production cost: every variant pays its own SAM call)\nprobe spent: $%.4f\n", spent)
	b, _ := json.MarshalIndent(map[string]any{"variants": vs, "runs": runs}, "", " ")
	p := filepath.Join(*out, "probe_"+time.Now().UTC().Format("20060102T150405Z")+".json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println("\nreport:", p)
}

func report(w io.Writer, r run, inv *inference.Inventory) {
	fmt.Fprintf(w, "   -- %s | SAM %d (packages %d, trays %d, food inside %d), kept %d, named %d, unnamed %d, small %d | sent %dx%d | %d listings, %d named, %d counted | points %d/%d/%d (hit/miss/none) | %d ms | $%.5f",
		r.Variant, inv.Instances, inv.Packages, inv.Trays, inv.Contained, inv.Kept, inv.Named, inv.Unnamed, inv.Small, inv.PrepW, inv.PrepH, inv.Listings(), inv.Units(), inv.Counted(),
		inv.Points.Hit, inv.Points.Miss, inv.Points.Missing, inv.MS, inv.Cost)
	if inv.Units() > 0 {
		fmt.Fprintf(w, " | $%.5f per named", inv.Cost/float64(inv.Units()))
	}
	// the parallel saving: the run's wall time against the calls added up
	var callSum int64
	for _, c := range inv.Calls {
		if !c.Cached {
			callSum += c.MS
		}
	}
	fmt.Fprintf(w, " | calls added up %d ms (wall %d ms)", callSum, inv.MS)
	if inv.CloserPhoto {
		fmt.Fprint(w, " | CLOSER PHOTO")
	}
	fmt.Fprintln(w)
	for _, c := range inv.Calls {
		cached := ""
		if c.Cached {
			cached = " [cache: $0 spent]"
		}
		fmt.Fprintf(w, "      %-22s %-14s %4d %6d ms  in %6d (cached %5d)  out %5d (reasoning %5d)  $%.5f%s %s\n",
			trim(c.Purpose, 22), c.Model, c.Status, c.MS, c.Usage.InputTokens, c.CachedTokens, c.Usage.OutputTokens,
			c.Usage.ReasoningTokens, c.Cost(), cached, c.Error)
	}
	for gi, it := range inv.Items {
		size := ""
		if len(it.Seen) > 0 {
			s := it.Seen[0]
			size = fmt.Sprintf(" [box %d: %.0f px short, %.2f%% area]", it.Members[0], s.ShortPx, s.AreaPct)
		}
		flags := ""
		if it.Check {
			flags += " CHECK"
		}
		if it.Tray {
			flags += fmt.Sprintf(" TRAY(%g servings)", it.NumberOfServing)
		}
		fmt.Fprintf(w, "      %2d. %s x%d (%s)%s%s\n", gi+1, it.Name, it.Count, it.Source, size, flags)
	}
	for _, nl := range inv.NotListed {
		fmt.Fprintf(w, "      not listed (not food): %s [box %d]\n", nl.Name, nl.Members[0])
	}
	for _, rj := range inv.Rejected {
		fmt.Fprintf(w, "      rejected box %d: %s\n", rj.Box, trim(rj.Reason, 120))
	}
	if r.Error != "" {
		fmt.Fprintf(w, "      ERROR %s\n", r.Error)
	}
}

// appUpload makes a photo like the app's upload: upright, the long edge at
// most 1600 px (width-only resize keeps the aspect), JPEG quality 0.85.
func appUpload(raw []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	u := inference.Upright(src, inference.Orientation(raw))
	b := u.Bounds()
	w, h := b.Dx(), b.Dy()
	if long := max(w, h); long > 1600 {
		w, h = w*1600/long, h*1600/long
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), u, b, xdraw.Src, nil)
		u = dst
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, u, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func callGate(url, name string, img []byte) *gateOut {
	g := &gateOut{}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("image", name)
	_, _ = fw.Write(img)
	_ = mw.Close()
	t0 := time.Now()
	resp, err := http.Post(url, mw.FormDataContentType(), &body)
	g.WallMS = time.Since(t0).Milliseconds()
	if err != nil {
		g.Verdict, g.Error = "unavailable", err.Error()
		return g
	}
	defer resp.Body.Close()
	g.Status = resp.StatusCode
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, g); err != nil || resp.StatusCode != 200 {
		g.Verdict, g.Food, g.Error = "unavailable", false, trim(string(raw), 200)
	}
	return g
}

func trim(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// natLess sorts shelf_2 before shelf_10.
func natLess(a, b string) bool {
	na, nb := numTail(a), numTail(b)
	if na >= 0 && nb >= 0 && strings.TrimRight(strings.TrimSuffix(a, filepath.Ext(a)), "0123456789") == strings.TrimRight(strings.TrimSuffix(b, filepath.Ext(b)), "0123456789") {
		return na < nb
	}
	return a < b
}

func numTail(s string) int {
	s = strings.TrimSuffix(s, filepath.Ext(s))
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) {
		return -1
	}
	n := 0
	for _, c := range s[i:] {
		n = n*10 + int(c-'0')
	}
	return n
}

// overlay draws each product group's boxes in one colour, from the 0–1000
// grid boxes (the same numbers the app will draw).
func overlay(raw []byte, inv *inference.Inventory, path string) {
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return
	}
	src = inference.Upright(src, inference.Orientation(raw)) // the boxes are in the upright frame
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	th := max(3, b.Dx()/250)
	for gi, it := range inv.Items {
		c := palette[gi%len(palette)]
		for _, n := range it.Norm {
			r := image.Rect(n[0]*b.Dx()/1000, n[1]*b.Dy()/1000, n[2]*b.Dx()/1000, n[3]*b.Dy()/1000).Add(b.Min)
			for _, edge := range []image.Rectangle{
				image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+th), image.Rect(r.Min.X, r.Max.Y-th, r.Max.X, r.Max.Y),
				image.Rect(r.Min.X, r.Min.Y, r.Min.X+th, r.Max.Y), image.Rect(r.Max.X-th, r.Min.Y, r.Max.X, r.Max.Y)} {
				draw.Draw(dst, edge.Intersect(b), &image.Uniform{c}, image.Point{}, draw.Src)
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	_ = png.Encode(f, dst)
}
