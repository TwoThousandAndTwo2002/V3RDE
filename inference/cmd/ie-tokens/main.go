// Command ie-tokens measures how many input tokens Muse charges for one photo
// at several sizes, with POST /v1/responses/input_tokens (no model runs).
// If the image tokens stop growing above a size, the API resizes the photo
// down to that size before the model reads it.
//
//	go run ./cmd/ie-tokens -photo ../photos/shelf_14.png
//
// The key is read from -key-file and is never printed.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TwoThousandAndTwo2002/Icarus/inference"
)

const endpoint = "https://api.meta.ai/v1/responses/input_tokens"

func main() {
	home, _ := os.UserHomeDir()
	keyFile := flag.String("key-file", filepath.Join(home, ".config/morpheus/meta_model_api_key"), "Meta Model API key file")
	photo := flag.String("photo", "../photos/shelf_14.png", "photo to measure")
	sizes := flag.String("sizes", "640,1280,1792,2240,2688,3264", "long-side sizes in px (0 or more than the photo = original)")
	flag.Parse()

	kb, err := os.ReadFile(*keyFile)
	if err != nil {
		log.Fatalf("key file %s: %v", *keyFile, err)
	}
	key := strings.TrimSpace(string(kb))
	f, err := os.Open(*photo)
	if err != nil {
		log.Fatal(err)
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		log.Fatalf("decode %s: %v", *photo, err)
	}
	b := src.Bounds()
	fmt.Printf("photo %s  %d x %d\n", filepath.Base(*photo), b.Dx(), b.Dy())

	base, raw, err := count(key, inference.UserMessage(inference.TextPart(prompt)))
	if err != nil {
		log.Fatalf("text only: %v (%s)", err, raw)
	}
	fmt.Printf("text only: %d tokens (subtracted below)\n\n", base)
	fmt.Printf("%-11s %8s %12s %14s %10s\n", "sent", "tokens", "image tok", "28px predict", "px/token")
	for _, s := range strings.Split(*sizes, ",") {
		long, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			log.Fatalf("size %q: %v", s, err)
		}
		img := resize(src, long)
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			log.Fatal(err)
		}
		msg := inference.UserMessage(inference.ImagePart(buf.Bytes(), "image/jpeg"), inference.TextPart(prompt))
		n, raw, err := count(key, msg)
		if err != nil {
			fmt.Printf("%4dx%-6d error: %v %s\n", w, h, err, raw)
			continue
		}
		it := n - base
		pred := ((w + 27) / 28) * ((h + 27) / 28)
		fmt.Printf("%4dx%-6d %8d %12d %14d %10.0f\n", w, h, n, it, pred, float64(w*h)/float64(max(it, 1)))
		time.Sleep(300 * time.Millisecond)
	}
}

const prompt = "List the food items in this photo."

// count returns input_tokens for one request body.
func count(key string, input []any) (int, string, error) {
	body, _ := json.Marshal(map[string]any{"model": inference.DefaultMuse, "input": input})
	req, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return 0, trim(raw), fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, trim(raw), err
	}
	if v, ok := out["input_tokens"].(float64); ok {
		return int(v), "", nil
	}
	return 0, trim(raw), fmt.Errorf("no input_tokens field")
}

func trim(b []byte) string {
	if len(b) > 300 {
		b = b[:300]
	}
	return string(b)
}

// resize scales so the long side is long px (area average). long <= 0 or
// larger than the photo keeps the original size.
func resize(src image.Image, long int) image.Image {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if long <= 0 || long >= max(sw, sh) {
		return src
	}
	var w, h int
	if sw >= sh {
		w, h = long, sh*long/sw
	} else {
		w, h = sw*long/sh, long
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := b.Min.Y+y*sh/h, b.Min.Y+(y+1)*sh/h
		for x := 0; x < w; x++ {
			x0, x1 := b.Min.X+x*sw/w, b.Min.X+(x+1)*sw/w
			var r, g, bl, n uint64
			for yy := y0; yy < max(y1, y0+1); yy++ {
				for xx := x0; xx < max(x1, x0+1); xx++ {
					cr, cg, cb, _ := src.At(xx, yy).RGBA()
					r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
				}
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n>>8), uint8(g/n>>8), uint8(bl/n>>8), 255
		}
	}
	return dst
}
