package inference

// X48f / X48g: the touch-up v2 prototype (T9 2026-10-29: "isolate the item and
// enhance the sharpness ... make it bigger"). It is used by cmd/touchupcompare,
// which makes the side-by-side page for T9; the touch-up service does not
// change until T9 chooses. X48g (after the X48f page) keeps two kinds:
//
//	studio_*      code only: StudioLook, the item cut out with a SAM mask, and a
//	              background made in code (analyze.Studio); no pixel is painted
//	muse_*        Muse Image called as the Meta cookbook does (JSON, tools off,
//	              "keep ..." prompt), checked in code with analyze.SSIM

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// ImageCallPrice is the price of one Muse Image edit (post.PriceImagePerImage).
const ImageCallPrice = 0.01

// V2 holds the clients of the prototype.
type V2 struct {
	API        *Client // Muse Spark and SAM 3.1 (POST /v1/responses)
	Muse, SAM  string  // model ids
	ImageURL   string  // POST .../images/edits base, e.g. https://api.meta.ai/v1
	ImageModel string  // muse-image-1.0
	HTTP       *http.Client
}

// small is img as a JPEG with the long edge at most long px (for the model
// calls: fewer tokens, the same look).
func small(img image.Image, long int) ([]byte, error) {
	b := img.Bounds()
	if max(b.Dx(), b.Dy()) > long {
		img = analyze.Enhance(img, analyze.Look{LongEdge: long, Saturation: 1})
	}
	return analyze.JPEG(img, 88)
}

// MaskRaster draws the decoded box-local masks of inst (each in its own
// W x H frame) on a w x h raster; it returns the raster and the number of
// masks drawn.
func MaskRaster(inst []Instance, w, h int) ([]byte, int) {
	out := make([]byte, w*h)
	n := 0
	for _, in := range inst {
		if !in.MaskOK || in.W <= 0 || in.H <= 0 {
			continue
		}
		bits, err := DecodeOneBit(in.Payload, in.MaskW, in.MaskH)
		if err != nil {
			continue
		}
		bw, bh := in.Right-in.Left, in.Bottom-in.Top
		// the box in the raster's pixels
		x0, x1 := in.Left*w/in.W, (in.Right*w+in.W-1)/in.W
		y0, y1 := in.Top*h/in.H, (in.Bottom*h+in.H-1)/in.H
		for y := max(0, y0); y < min(h, y1); y++ {
			fy := (float64(y)+0.5)*float64(in.H)/float64(h) - float64(in.Top) // in frame px, box-local
			my := int(fy * float64(in.MaskH) / float64(bh))
			if my < 0 || my >= in.MaskH {
				continue
			}
			for x := max(0, x0); x < min(w, x1); x++ {
				fx := (float64(x)+0.5)*float64(in.W)/float64(w) - float64(in.Left)
				mx := int(fx * float64(in.MaskW) / float64(bw))
				if mx >= 0 && mx < in.MaskW && bits[my*in.MaskW+mx] != 0 {
					out[y*w+x] = 1
				}
			}
		}
		n++
	}
	return out, n
}

// ImageCall is one Muse Image call for the events line and the page.
type ImageCall struct {
	Status  int     `json:"status"`
	MS      int64   `json:"ms"`
	CostUSD float64 `json:"cost_usd"`
	Error   string  `json:"error,omitempty"`
}

// Variant is one column of the page.
type Variant struct {
	Name    string        `json:"name"`
	MS      int64         `json:"ms"`
	CostUSD float64       `json:"cost_usd"`
	W       int           `json:"w"`
	H       int           `json:"h"`
	Error   string        `json:"error,omitempty"`
	Look    *analyze.Look `json:"look,omitempty"`
	Masks   int           `json:"masks,omitempty"`
	// X48g
	Concept string   `json:"concept,omitempty"` // the SAM concept of the mask
	SSIM    *float64 `json:"ssim,omitempty"`    // the structure score against the source
	Passed  *bool    `json:"passed,omitempty"`  // SSIM >= SSIMMin
	Note    string   `json:"note,omitempty"`    // what the edit started from
	JPEG    []byte   `json:"-"`
}

func done(v *Variant, img image.Image, t0 time.Time) {
	v.MS = time.Since(t0).Milliseconds()
	b, err := analyze.JPEG(img, 90)
	if err != nil {
		v.Error = err.Error()
		return
	}
	v.JPEG, v.W, v.H = b, img.Bounds().Dx(), img.Bounds().Dy()
}

// X48g (T9 2026-10-30, after the X48f page): the Muse Image call of X48f made
// a new picture (an unrelated meal), so it is called as the Meta cookbook
// does: JSON with an "images" array, the search and shell tools off, and a
// prompt that names only the change and ends with what must stay. Every edit
// is compared with its source in code (analyze.SSIM); a low score is refused.

// The X48g prompts: the change, then what stays (docs/sources/X48g.md).
const (
	FixPrompt = "Correct the exposure and the white balance of this photo and make it sharper. " +
		"Keep every food item, package, container, label, text, quantity and position, the background, " +
		"the framing and the camera angle exactly as they are. Do not add or remove anything."
	BiggerPrompt = "Make this photo larger and sharper with clean, natural detail. " +
		"Keep every food item, package, label, text, color, quantity and position, the background and the " +
		"framing exactly as they are. Do not add or remove anything."
)

// SSIMMin is the lowest structure score an edit may have (analyze.SSIM): a
// new picture scores far lower (X48g tests); the page shows every score.
const SSIMMin = 0.5

// SubjectConcepts are the SAM concepts tried for the item, in this order,
// at the same time; the app adds the item's title first.
var SubjectConcepts = []string{"food", "fruit", "packaged food", "product"}

// EditJSON is one Muse Image edit in the JSON form (dev.meta.ai/docs
// /image-generation): images = [{image_url: a data URL}], the tools off. A 400
// that names an optional field is tried once more without those fields.
func (v *V2) EditJSON(ctx context.Context, img []byte, prompt string) ([]byte, ImageCall, error) {
	body := map[string]any{"model": v.ImageModel, "prompt": prompt, "n": 1,
		"images":          []any{map[string]any{"image_url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(img)}},
		"response_format": "b64_json", "output_format": "jpeg",
		"tool_enablement": map[string]any{"enable_image_search": false, "enable_web_search": false, "enable_shell": false}}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(img)); err == nil {
		body["size"] = fmt.Sprintf("%dx%d", cfg.Width, cfg.Height)
	}
	b, ic, err := v.postEdit(ctx, body)
	if err != nil && ic.Status == http.StatusBadRequest {
		for _, k := range []string{"size", "response_format", "output_format", "n"} {
			delete(body, k)
		}
		var ic2 ImageCall
		b, ic2, err = v.postEdit(ctx, body)
		ic2.MS += ic.MS
		ic2.Error = strings.TrimSpace(ic2.Error + " (first try: " + ic.Error + ")")
		if err == nil {
			ic2.Error = "first try: " + ic.Error
		}
		ic = ic2
	}
	return b, ic, err
}

func (v *V2) postEdit(ctx context.Context, body map[string]any) ([]byte, ImageCall, error) {
	var ic ImageCall
	jb, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(v.ImageURL, "/")+"/images/edits", bytes.NewReader(jb))
	if err != nil {
		return nil, ic, err
	}
	req.Header.Set("Authorization", "Bearer "+v.API.Key)
	req.Header.Set("Content-Type", "application/json")
	hc := v.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 120 * time.Second}
	}
	t0 := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		ic.MS, ic.Error = time.Since(t0).Milliseconds(), err.Error()
		return nil, ic, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 40<<20))
	ic.MS, ic.Status = time.Since(t0).Milliseconds(), resp.StatusCode
	if err != nil || resp.StatusCode/100 != 2 {
		ic.Error = fmt.Sprintf("HTTP %d %.200s", resp.StatusCode, raw)
		return nil, ic, errors.New(ic.Error)
	}
	ic.CostUSD = ImageCallPrice
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &out) != nil || len(out.Data) == 0 {
		ic.Error = "no image in the answer"
		return nil, ic, errors.New(ic.Error)
	}
	if out.Data[0].B64 != "" {
		b, err := base64.StdEncoding.DecodeString(out.Data[0].B64)
		if err != nil || len(b) == 0 {
			ic.Error = "image is not base64"
			return nil, ic, errors.New(ic.Error)
		}
		return b, ic, nil
	}
	if out.Data[0].URL == "" {
		ic.Error = "no image in the answer"
		return nil, ic, errors.New(ic.Error)
	}
	greq, err := http.NewRequestWithContext(ctx, http.MethodGet, out.Data[0].URL, nil)
	if err != nil {
		ic.Error = "bad image url"
		return nil, ic, err
	}
	gr, err := hc.Do(greq)
	if err != nil {
		ic.Error = "image url: " + err.Error()
		return nil, ic, err
	}
	defer gr.Body.Close()
	b, err := io.ReadAll(io.LimitReader(gr.Body, 40<<20))
	if err != nil || gr.StatusCode != 200 || len(b) == 0 {
		ic.Error = fmt.Sprintf("image url: HTTP %d", gr.StatusCode)
		return nil, ic, errors.New(ic.Error)
	}
	return b, ic, nil
}

// SubjectMask asks SAM for each concept at the same time and keeps the first
// concept (in order) whose masks touch the middle of the photo and cover 3% to
// 92% of it: the item photo is cut around the item, so the item is in the
// middle. It returns the w x h raster of that concept's masks that touch the
// middle, the concept, the number of masks and the cost of every SAM call.
func (v *V2) SubjectMask(ctx context.Context, img image.Image, concepts []string) ([]byte, string, int, float64, error) {
	jb, err := small(img, 1536)
	if err != nil {
		return nil, "", 0, 0, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	type res struct {
		inst []Instance
		cost float64
		err  error
	}
	got := make([]res, len(concepts))
	var wg sync.WaitGroup
	for i, c := range concepts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call, err := v.API.Do(ctx, "sam:"+c, map[string]any{"model": v.SAM,
				"input":    UserMessage(TextPart(c), ImagePart(jb, "image/jpeg")),
				"metadata": map[string]any{"include_confidence": "true", "mask_encoding": "one_bit"}})
			r := res{err: err}
			if call != nil {
				r.cost = call.Cost()
			}
			if err == nil {
				r.inst, _ = ParseSAM(call.Text())
			}
			got[i] = r
		}()
	}
	wg.Wait()
	cost := 0.0
	for _, r := range got {
		cost += r.cost
	}
	mid := image.Rect(w/4, h/4, w*3/4, h*3/4)
	for i, r := range got {
		var keep []Instance
		for _, in := range r.inst {
			if in.W <= 0 || in.H <= 0 {
				continue
			}
			box := image.Rect(in.Left*w/in.W, in.Top*h/in.H, in.Right*w/in.W, in.Bottom*h/in.H)
			if box.Overlaps(mid) {
				keep = append(keep, in)
			}
		}
		m, n := MaskRaster(keep, w, h)
		if n == 0 {
			continue
		}
		set := 0
		for _, x := range m {
			set += int(x)
		}
		if share := float64(set) / float64(w*h); share >= 0.03 && share <= 0.92 {
			return m, concepts[i], n, cost, nil
		}
	}
	return nil, "", 0, cost, errors.New("sam: no mask of the item for " + strings.Join(concepts, ", "))
}

// WiderSource cuts the item again from the uploaded photo (orig), with a
// wider margin (a share of the box): for an item photo too small to make
// bigger in code. meta is the item photo's record (Kind "crop", Box, FrameW/H).
func WiderSource(orig []byte, meta ItemPhotoMeta, margin float64) (image.Image, error) {
	if meta.Kind != "crop" || len(meta.Box) != 4 || meta.FrameW <= 0 || meta.FrameH <= 0 {
		return nil, errors.New("not a crop with a box")
	}
	src, _, err := image.Decode(bytes.NewReader(orig))
	if err != nil {
		return nil, err
	}
	src = Upright(src, Orientation(orig))
	jb, err := CropJPEG(src, Instance{Left: meta.Box[0], Top: meta.Box[1], Right: meta.Box[2], Bottom: meta.Box[3],
		W: meta.FrameW, H: meta.FrameH}, margin)
	if err != nil {
		return nil, err
	}
	out, _, err := image.Decode(bytes.NewReader(jb))
	return out, err
}

// Compare makes the X48g variants of one item photo. src is what the edits
// start from: the item photo, or WiderSource of it (note says which). SAM and
// the two Muse Image edits run at the same time.
//
//	original      the item photo as it is
//	studio_plain  code: StudioLook, cut to the item, a light surface + shadow
//	studio_blur   code: the same, the photo itself blurred behind the item
//	muse_fixed    Muse Image (FixPrompt), refused when SSIM < SSIMMin
//	muse_bigger   Muse Image (BiggerPrompt) on src, the same check
func (v *V2) Compare(ctx context.Context, photo []byte, src image.Image, note string, concepts []string) ([]Variant, error) {
	item, _, err := image.Decode(bytes.NewReader(photo))
	if err != nil {
		return nil, err
	}
	if src == nil {
		src, note = item, "the item photo"
	}
	ib := item.Bounds()
	out := []Variant{{Name: "original", W: ib.Dx(), H: ib.Dy(), JPEG: photo, Note: note}}
	srcJPEG, err := analyze.JPEG(src, 92)
	if err != nil {
		return nil, err
	}
	t0 := time.Now()
	type edit struct {
		jb []byte
		ic ImageCall
		e  error
	}
	fixC, bigC := make(chan edit, 1), make(chan edit, 1)
	go func() { jb, ic, e := v.EditJSON(ctx, srcJPEG, FixPrompt); fixC <- edit{jb, ic, e} }()
	go func() { jb, ic, e := v.EditJSON(ctx, srcJPEG, BiggerPrompt); bigC <- edit{jb, ic, e} }()
	mask, concept, n, samCost, merr := v.SubjectMask(ctx, src, concepts)
	sb := src.Bounds()
	for _, style := range []string{"plain", "blur"} {
		r := Variant{Name: "studio_" + style, Concept: concept, Masks: n, Note: note}
		if style == "plain" { // one mask for both: its SAM cost is counted once, here
			r.CostUSD = samCost
		}
		look := analyze.StudioLook()
		if merr != nil {
			r.Error = merr.Error() + " (no new background; the look only)"
			done(&r, analyze.Enhance(src, look), t0)
			r.Look = &look
			out = append(out, r)
			continue
		}
		look.Crop = analyze.GridCrop(analyze.MaskBounds(mask, sb.Dx(), sb.Dy()), sb.Dx(), sb.Dy(), 0.12)
		fg := analyze.Enhance(src, look)
		fb := fg.Bounds()
		alpha := analyze.MaskTo(mask, sb.Dx(), sb.Dy(), look.Crop, fb.Dx(), fb.Dy(), max(2, max(fb.Dx(), fb.Dy())/150))
		done(&r, analyze.Studio(fg, alpha, style), t0)
		r.Look = &look
		out = append(out, r)
	}
	for _, x := range []struct {
		name string
		c    chan edit
	}{{"muse_fixed", fixC}, {"muse_bigger", bigC}} {
		e := <-x.c
		r := Variant{Name: x.name, CostUSD: e.ic.CostUSD, Note: note}
		if e.ic.Error != "" {
			r.Error = e.ic.Error
		}
		if e.e == nil {
			im, _, derr := image.Decode(bytes.NewReader(e.jb))
			if derr != nil {
				r.Error = "the edit is not an image"
			} else {
				s := analyze.SSIM(src, im)
				ok := s >= SSIMMin
				r.SSIM, r.Passed = &s, &ok
				done(&r, im, t0)
			}
		}
		r.MS = e.ic.MS
		out = append(out, r)
	}
	return out, nil
}

// Thumb is img (a JPEG) with the long edge at most long px, at quality q.
func Thumb(jb []byte, long, q int) ([]byte, error) {
	im, err := jpeg.Decode(bytes.NewReader(jb))
	if err != nil {
		return nil, err
	}
	b := im.Bounds()
	if max(b.Dx(), b.Dy()) > long {
		im = analyze.Enhance(im, analyze.Look{LongEdge: long, Saturation: 1})
	}
	return analyze.JPEG(im, q)
}
