// Command touchupcompare makes the touch-up v2 comparison (X48f, X48g) on the
// box: for the newest item photos of the analyze photo store, every variant of
// inference/touchupv2.go (original, studio_plain, studio_blur, muse_fixed,
// muse_bigger), with the time, the cost and the structure score of each. An
// item photo with a long edge under -wider-under px is cut again from the
// upload with the -wider-margin margin (X48g option 4). It writes full-size JPEGs, small JPEGs for
// the page, and results.json; it changes no service and no listing.
//
//	touchupcompare -photos /photos -key-file /run/secrets/meta_key -out /out [-n 6] [-concepts food,fruit,...]
//
// -parallel photos run at the same time (default 2: the box has 1.9 GiB);
// inside one photo the Meta calls run at the same time.
// Progress goes to stderr ("PROGRESS k/n"); the last line on stdout is
// "SUMMARY {json}".
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TwoThousandAndTwo2002/Icarus/inference"
)

type photoResult struct {
	N        int                 `json:"n"`
	SHA      string              `json:"sha256_prefix"` // the item photo's sha256, 12 hex (no photo_id)
	Variants []inference.Variant `json:"variants"`
	Error    string              `json:"error,omitempty"`
}

func main() {
	photos := flag.String("photos", "/photos", "the analyze photo store (items/ in it)")
	keyFile := flag.String("key-file", "/run/secrets/meta_key", "the Meta API key file")
	out := flag.String("out", "/out", "the output folder")
	n := flag.Int("n", 6, "the newest n item photos")
	minBytes := flag.Int64("min-bytes", 20000, "skip item photos smaller than this")
	events := flag.String("events", "", "the analyze events folder: the item's name from the scan (X48i); \"\" = none")
	concepts := flag.String("concepts", strings.Join(inference.SubjectConcepts, ","), "SAM concepts for the item, in order")
	widerUnder := flag.Int("wider-under", 500, "cut again from the upload when the item photo's long edge is under this")
	widerMargin := flag.Float64("wider-margin", 0.35, "the margin of that cut (a share of the box)")
	upstream := flag.String("upstream", inference.DefaultUpstream, "POST /v1/responses")
	imageBase := flag.String("image-base", "https://api.meta.ai/v1", "the base of POST .../images/edits")
	imageModel := flag.String("image-model", "muse-image-1.0", "Muse Image model id")
	thumb := flag.Int("thumb", 640, "the long edge of the page images")
	timeout := flag.Duration("timeout", 150*time.Second, "one photo, every call included")
	parallel := flag.Int("parallel", 2, "photos at the same time (memory: about 110 MB each for a 2376 px photo)")
	flag.Parse()
	kb, err := os.ReadFile(*keyFile)
	if err != nil {
		log.Fatal("key: ", err)
	}
	key := strings.TrimSpace(string(kb))
	files, err := filepath.Glob(filepath.Join(*photos, "items", "*.jpg"))
	if err != nil {
		log.Fatal(err)
	}
	type f struct {
		path string
		mod  time.Time
	}
	var list []f
	for _, p := range files {
		if st, err := os.Stat(p); err == nil && st.Size() >= *minBytes {
			list = append(list, f{p, st.ModTime()})
		}
	}
	sort.Slice(list, func(a, b int) bool { return list[a].mod.After(list[b].mod) })
	if len(list) > *n {
		list = list[:*n]
	}
	if len(list) == 0 {
		log.Fatal("no item photos in ", *photos, "/items")
	}
	if err := os.MkdirAll(filepath.Join(*out, "thumb"), 0o755); err != nil {
		log.Fatal(err)
	}
	hc := &http.Client{Timeout: 120 * time.Second, Transport: inference.MetaTransport}
	v := &inference.V2{API: &inference.Client{URL: *upstream, Key: key, Effort: "low", HTTP: hc},
		Muse: inference.DefaultMuse, SAM: inference.DefaultSAM, ImageURL: *imageBase, ImageModel: *imageModel, HTTP: hc}
	res := make([]photoResult, len(list))
	var mu sync.Mutex
	finished := 0
	var wg sync.WaitGroup
	slots := make(chan struct{}, max(1, *parallel))
	t0 := time.Now()
	for i, it := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			r := photoResult{N: i + 1}
			defer func() {
				mu.Lock()
				res[i] = r
				finished++
				fmt.Fprintf(os.Stderr, "PROGRESS %d/%d\n", finished, len(list))
				mu.Unlock()
			}()
			b, err := os.ReadFile(it.path)
			if err != nil {
				r.Error = "read failed"
				return
			}
			sum := sha256.Sum256(b)
			r.SHA = hex.EncodeToString(sum[:6])
			ctx, cancel := context.WithTimeout(context.Background(), *timeout)
			defer cancel()
			src, target, note, name := itemSource(*photos, *events, it.path, b, *widerUnder, *widerMargin)
			cs := strings.Split(*concepts, ",")
			if name != "" { // the scan's name first (X48i)
				cs = append([]string{strings.ToLower(name)}, cs...)
				note += "; name from the scan: \u201c" + name + "\u201d"
			}
			vs, err := v.Compare(ctx, b, src, target, note, cs)
			if err != nil {
				r.Error = err.Error()
				return
			}
			for k := range vs {
				if vs[k].JPEG == nil {
					continue
				}
				name := fmt.Sprintf("%d_%s.jpg", i+1, vs[k].Name)
				if err := os.WriteFile(filepath.Join(*out, name), vs[k].JPEG, 0o644); err != nil {
					vs[k].Error = "write failed"
					continue
				}
				if tb, err := inference.Thumb(vs[k].JPEG, *thumb, 80); err == nil {
					_ = os.WriteFile(filepath.Join(*out, "thumb", name), tb, 0o644)
				}
			}
			r.Variants = vs
		}()
	}
	wg.Wait()
	jb, _ := json.MarshalIndent(map[string]any{"photos": res, "made": time.Now().UTC().Format(time.RFC3339)}, "", " ")
	if err := os.WriteFile(filepath.Join(*out, "results.json"), jb, 0o644); err != nil {
		log.Fatal(err)
	}
	// the summary: the median time and the total cost of each variant
	ms := map[string][]int64{}
	cost := map[string]float64{}
	errs := map[string]int{}
	for _, r := range res {
		for _, x := range r.Variants {
			cost[x.Name] += x.CostUSD // a call that answered is paid, even when its image failed
			if x.Error != "" && x.JPEG == nil {
				errs[x.Name]++
				continue
			}
			ms[x.Name] = append(ms[x.Name], x.MS)
		}
	}
	med := map[string]int64{}
	for k, v := range ms {
		sort.Slice(v, func(a, b int) bool { return v[a] < v[b] })
		med[k] = v[len(v)/2]
	}
	s, _ := json.Marshal(map[string]any{"photos": len(res), "median_ms": med, "cost_usd": cost, "errors": errs,
		"wall_ms": time.Since(t0).Milliseconds()})
	fmt.Println("SUMMARY " + string(s))
}

// itemSource is the item cut again from the upload (X48i): with the item
// photo's margin, or a wider one when the item photo is small; with the
// item's box in the cut and the item's name from the scan. nil when the item
// photo is not a crop or the upload is gone (then the item photo is used).
func itemSource(photos, events, itemPath string, item []byte, under int, margin float64) (image.Image, image.Rectangle, string, string) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(item))
	if err != nil {
		return nil, image.Rectangle{}, "the item photo", ""
	}
	mb, err := os.ReadFile(strings.TrimSuffix(itemPath, ".jpg") + ".json")
	var m inference.ItemPhotoMeta
	if err != nil || json.Unmarshal(mb, &m) != nil || !inference.PhotoIDPattern.MatchString(m.PhotoID) {
		return nil, image.Rectangle{}, "the item photo (no record)", ""
	}
	name := ""
	if events != "" {
		name = inference.ItemName(events, m.RequestID, m.Item)
	}
	orig, err := os.ReadFile(filepath.Join(photos, filepath.Base(m.ImageSHA256)+".jpg"))
	if err != nil {
		return nil, image.Rectangle{}, "the item photo (no upload)", name
	}
	mg, how := inference.CropMargin, "the item cut again from the upload"
	if max(cfg.Width, cfg.Height) < under {
		mg, how = margin, "the item cut again from the upload, wider"
	}
	src, box, err := inference.ItemSource(orig, m, mg)
	if err != nil {
		return nil, image.Rectangle{}, "the item photo (" + err.Error() + ")", name
	}
	b := src.Bounds()
	return src, box, fmt.Sprintf("%s: %dx%d, item box %v (item photo %dx%d)", how, b.Dx(), b.Dy(), box, cfg.Width, cfg.Height), name
}
