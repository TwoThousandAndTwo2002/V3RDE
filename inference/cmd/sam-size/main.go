// Command sam-size measures what SAM 3.1 gives at smaller photo sizes: the
// time per call, the upload size, and how many of the boxes found on the
// original photo are found again (IoU >= 0.5 on the 0-1000 grid). It is the
// SAM version of cmd/ie-tokens. SAM is billed per image, not per pixel
// (dev.meta.ai/docs/pricing-rate-limits), so a smaller photo saves time and
// upload, not money; the money comes back if the time allows more (nested)
// SAM calls.
//
//	go run ./cmd/sam-size -photos ../photos -only shelf_22,shelf_14 -sizes orig,jpeg,2048,1536,1024
//
// The key is read from -key-file and is never printed.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	pngenc "image/png"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/TwoThousandAndTwo2002/Icarus/inference"
)

type row struct {
	Photo, Concept, Size string
	W, H, Bytes          int
	MS                   int64
	Instances, Kept      int
	Recall, RecallNamed  float64 // of the original's kept boxes; of those >= 64 px seen
	Error                string
}

func main() {
	home, _ := os.UserHomeDir()
	keyFile := flag.String("key-file", filepath.Join(home, ".config/morpheus/meta_model_api_key"), "Meta Model API key file")
	photos := flag.String("photos", "../photos", "photo folder")
	only := flag.String("only", "shelf_22,shelf_14,shelf_10,shelf_12,shelf_16", "photos to test")
	sizes := flag.String("sizes", "orig,jpeg,2048,1536,1024", "orig = the file as is; jpeg = original pixels as JPEG q92; N = long side N px, JPEG")
	concepts := flag.String("concepts", "food,package", "SAM concepts")
	out := flag.String("out", "../probe_out_sam", "report folder")
	flag.Parse()
	kb, err := os.ReadFile(*keyFile)
	if err != nil {
		log.Fatal(err)
	}
	cl := &inference.Client{URL: inference.DefaultUpstream, Key: strings.TrimSpace(string(kb)), HTTP: &http.Client{Timeout: 120 * time.Second}}
	_ = os.MkdirAll(*out, 0o755)
	var rows []row
	fmt.Printf("%-10s %-8s %-6s %10s %8s %7s %6s %5s %8s %13s\n", "photo", "concept", "size", "sent", "KB", "ms", "inst", "kept", "recall", "recall>=64px")
	for _, name := range strings.Split(*only, ",") {
		path := filepath.Join(*photos, strings.TrimSpace(name)+".png")
		raw, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		src, _, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			log.Fatal(err)
		}
		prep := inference.Prepare(src) // the image Muse reads: what "seen" means
		for _, concept := range strings.Split(*concepts, ",") {
			var ref []inference.Instance
			for _, sz := range strings.Split(*sizes, ",") {
				body, mime, w, h, err := encode(raw, src, sz)
				r := row{Photo: name, Concept: concept, Size: sz, W: w, H: h, Bytes: len(body)}
				if err != nil {
					log.Fatal(err)
				}
				c, err := cl.Do(context.Background(), "sam:"+concept, map[string]any{
					"model":    inference.DefaultSAM,
					"input":    inference.UserMessage(inference.TextPart(concept), inference.ImagePart(body, mime)),
					"metadata": map[string]any{"include_confidence": "true", "mask_encoding": "one_bit"},
				})
				r.MS = c.MS
				if err != nil {
					r.Error = err.Error()
				} else {
					all, _ := inference.ParseSAM(c.Text())
					kept := inference.Dedup(all)
					r.Instances, r.Kept = len(all), len(kept)
					if ref == nil {
						ref = kept
					}
					r.Recall, r.RecallNamed = recall(ref, kept, prep)
				}
				rows = append(rows, r)
				fmt.Printf("%-10s %-8s %-6s %10s %8d %7d %6d %5d %7.0f%% %12.0f%% %s\n", name, concept, sz, fmt.Sprintf("%dx%d", w, h),
					r.Bytes/1024, r.MS, r.Instances, r.Kept, 100*r.Recall, 100*r.RecallNamed, r.Error)
			}
		}
	}
	b, _ := json.MarshalIndent(rows, "", " ")
	p := filepath.Join(*out, "sam_size_"+time.Now().UTC().Format("20060102T150405Z")+".json")
	_ = os.WriteFile(p, b, 0o644)
	fmt.Printf("\n%d SAM calls, $%.4f (SAM is $%.4f per image)\nreport: %s\n", len(rows), float64(len(rows))*inference.PriceSAMPerImage, inference.PriceSAMPerImage, p)
}

// encode makes one test image. Sizes: "orig" = the file as is; "jpeg" =
// original pixels, JPEG q92; "N" = long side N px, JPEG q92; "pngN" = long
// side N px, PNG (lossless); "qQ" = original pixels, JPEG quality Q;
// "qQ@N" = long side N px, JPEG quality Q.
func encode(raw []byte, src image.Image, sz string) ([]byte, string, int, int, error) {
	b := src.Bounds()
	if sz == "orig" {
		return raw, "image/png", b.Dx(), b.Dy(), nil
	}
	quality, long, png := 92, 0, false
	spec := sz
	switch {
	case spec == "jpeg":
	case strings.HasPrefix(spec, "png"):
		png = true
		n, err := strconv.Atoi(strings.TrimPrefix(spec, "png"))
		if err != nil {
			return nil, "", 0, 0, fmt.Errorf("size %q", sz)
		}
		long = n
	case strings.HasPrefix(spec, "q"):
		q, at, _ := strings.Cut(strings.TrimPrefix(spec, "q"), "@")
		var err error
		if quality, err = strconv.Atoi(q); err != nil {
			return nil, "", 0, 0, fmt.Errorf("size %q", sz)
		}
		if at != "" {
			if long, err = strconv.Atoi(at); err != nil {
				return nil, "", 0, 0, fmt.Errorf("size %q", sz)
			}
		}
	default:
		n, err := strconv.Atoi(spec)
		if err != nil {
			return nil, "", 0, 0, fmt.Errorf("size %q", sz)
		}
		long = n
	}
	img := src
	w, h := b.Dx(), b.Dy()
	if long > 0 && max(w, h) > long {
		if w >= h {
			w, h = long, h*long/w
		} else {
			w, h = w*long/h, long
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
		img = dst
	}
	var buf bytes.Buffer
	if png {
		err := pngenc.Encode(&buf, img)
		return buf.Bytes(), "image/png", w, h, err
	}
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	return buf.Bytes(), "image/jpeg", w, h, err
}

// recall: the share of the reference boxes found again (IoU >= 0.5 on the
// 0-1000 grid), over all of them and over those Muse would name (>= 64 px).
func recall(ref, got []inference.Instance, p *inference.Prepared) (all, named float64) {
	var n, hit, nn, nhit int
	for _, r := range ref {
		rn := inference.NormBox(r)
		found := false
		for _, g := range got {
			if gridIoU(rn, inference.NormBox(g)) >= 0.5 {
				found = true
				break
			}
		}
		big := inference.SeenSize(r, p.W, p.H).ShortPx >= 64
		n++
		if big {
			nn++
		}
		if found {
			hit++
			if big {
				nhit++
			}
		}
	}
	if n == 0 {
		return 1, 1
	}
	all = float64(hit) / float64(n)
	named = 1
	if nn > 0 {
		named = float64(nhit) / float64(nn)
	}
	return all, named
}

func gridIoU(a, b inference.Norm) float64 {
	l, t, r, bt := max(a[0], b[0]), max(a[1], b[1]), min(a[2], b[2]), min(a[3], b[3])
	if r <= l || bt <= t {
		return 0
	}
	in := float64((r - l) * (bt - t))
	ua := float64((a[2]-a[0])*(a[3]-a[1]) + (b[2]-b[0])*(b[3]-b[1]))
	return in / (ua - in)
}
