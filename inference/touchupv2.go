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
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
	BiggerPrompt = "Make this photo larger and sharper with clean, natural detail. " +
		"Keep every food item, package, label, text, color, quantity and position, the background and the " +
		"framing exactly as they are. Do not add or remove anything."
)

// SSIMMin is the lowest structure score an edit may have (analyze.SSIM).
// X48h: on the X48g page the true edits scored 0.82-0.97 and the edits that
// re-framed the scene (new boxes around a cereal box) 0.12-0.67, two of them
// over the old limit of 0.5; 0.75 separates them.
const SSIMMin = 0.75

// ContainerConcepts are asked with the item (X48h, T9 2026-10-30: "any
// container that food which is ready to eat in should be visible. this
// includes buffet trays and other serving bowls"): a container whose box holds
// at least half of the item's mask is kept with the item.
var ContainerConcepts = []string{"plate", "bowl", "food tray", "takeout container"}

// MuseInputEdge: the source is made this big in code before Muse Image (X48h:
// a 232 px source came back re-framed, with invented neighbours).
const MuseInputEdge = 1024

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

// SubjectMask asks SAM for each concept and each container concept at the
// same time. The item is the first concept (in order) whose masks touch the
// middle of the photo and cover 3% to 92% of it (the item photo is cut around
// the item). A container is added when its box holds at least half of the
// item's mask pixels. It returns the w x h raster, a label ("food + plate"),
// the number of masks and the cost of every SAM call.
func (v *V2) SubjectMask(ctx context.Context, img image.Image, concepts, containers []string, target image.Rectangle) ([]byte, string, int, float64, error) {
	jb, err := small(img, 1536)
	if err != nil {
		return nil, "", 0, 0, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	all := append(append([]string{}, concepts...), containers...)
	type res struct {
		inst []Instance
		cost float64
	}
	got := make([]res, len(all))
	var wg sync.WaitGroup
	for i, c := range all {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call, err := v.API.Do(ctx, "sam:"+c, map[string]any{"model": v.SAM,
				"input":    UserMessage(TextPart(c), ImagePart(jb, "image/jpeg")),
				"metadata": map[string]any{"include_confidence": "true", "mask_encoding": "one_bit"}})
			var r res
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
	boxOf := func(in Instance) image.Rectangle {
		return image.Rect(in.Left*w/in.W, in.Top*h/in.H, in.Right*w/in.W, in.Bottom*h/in.H)
	}
	mid := image.Rect(w/4, h/4, w*3/4, h*3/4)
	area := func(r image.Rectangle) float64 { return float64(r.Dx() * r.Dy()) }
	// pick is the item's instances of one concept. With a target (the item's
	// box from the scan, X48i): the instance that matches it best (IoU >= 0.3),
	// and any other instance whose box is at least 70% inside it (one item in
	// several masks); a neighbour (the tomatoes next to the bananas, the next
	// cereal box) is left out. With no target: every instance that touches
	// the middle (X48g).
	pick := func(insts []Instance) []Instance {
		var keep []Instance
		if target.Empty() {
			for _, in := range insts {
				if in.W > 0 && in.H > 0 && boxOf(in).Overlaps(mid) {
					keep = append(keep, in)
				}
			}
			return keep
		}
		best, bestIoU := -1, 0.0
		for i, in := range insts {
			if in.W <= 0 || in.H <= 0 {
				continue
			}
			bx := boxOf(in)
			inter := area(bx.Intersect(target))
			if iou := inter / (area(bx) + area(target) - inter); iou > bestIoU {
				best, bestIoU = i, iou
			}
		}
		if best < 0 || bestIoU < 0.3 {
			return nil
		}
		keep = append(keep, insts[best])
		for i, in := range insts {
			if i == best || in.W <= 0 || in.H <= 0 {
				continue
			}
			if bx := boxOf(in); area(bx) > 0 && area(bx.Intersect(target)) >= 0.7*area(bx) {
				keep = append(keep, in)
			}
		}
		return keep
	}
	for i := range concepts {
		keep := pick(got[i].inst)
		m, n := MaskRaster(keep, w, h)
		set := 0
		for _, x := range m {
			set += int(x)
		}
		if n == 0 || float64(set) < 0.03*float64(w*h) || float64(set) > 0.92*float64(w*h) {
			continue
		}
		label := concepts[i]
		for k, c := range containers {
			var hold []Instance
			for _, in := range got[len(concepts)+k].inst {
				if in.W <= 0 || in.H <= 0 {
					continue
				}
				r := boxOf(in).Intersect(image.Rect(0, 0, w, h)) // raster coordinates
				inside := 0
				for y := r.Min.Y; y < r.Max.Y; y++ {
					for x := r.Min.X; x < r.Max.X; x++ {
						inside += int(m[y*w+x])
					}
				}
				if 2*inside >= set {
					hold = append(hold, in)
				}
			}
			if cm, cn := MaskRaster(hold, w, h); cn > 0 {
				for j, x := range cm {
					m[j] |= x
				}
				n += cn
				label += " + " + c
			}
		}
		return m, label, n, cost, nil
	}
	return nil, "", 0, cost, errors.New("sam: no mask of the item for " + strings.Join(concepts, ", "))
}

// ItemSource cuts the item again from the uploaded photo (orig) with a
// margin (a share of the box: CropMargin as the item photo, or wider for a
// small one), and gives the item's box in the cut's pixels: the target of
// SubjectMask (X48i). meta is the item photo's record (Kind "crop", Box,
// FrameW/H, the box in SAM's frame of the upright upload).
func ItemSource(orig []byte, meta ItemPhotoMeta, margin float64) (image.Image, image.Rectangle, error) {
	if meta.Kind != "crop" || len(meta.Box) != 4 || meta.FrameW <= 0 || meta.FrameH <= 0 {
		return nil, image.Rectangle{}, errors.New("not a crop with a box")
	}
	src, _, err := image.Decode(bytes.NewReader(orig))
	if err != nil {
		return nil, image.Rectangle{}, err
	}
	src = Upright(src, Orientation(orig))
	bd := src.Bounds()
	sx, sy := float64(bd.Dx())/float64(meta.FrameW), float64(bd.Dy())/float64(meta.FrameH)
	l, t, r, b := float64(meta.Box[0]), float64(meta.Box[1]), float64(meta.Box[2]), float64(meta.Box[3])
	mx, my := margin*(r-l), margin*(b-t)
	cut := image.Rect(int((l-mx)*sx), int((t-my)*sy), int((r+mx)*sx+0.5), int((b+my)*sy+0.5)).Add(bd.Min).Intersect(bd)
	box := image.Rect(int(l*sx), int(t*sy), int(r*sx+0.5), int(b*sy+0.5)).Add(bd.Min).Intersect(cut)
	if cut.Empty() || box.Empty() {
		return nil, image.Rectangle{}, errors.New("crop: empty box")
	}
	out := image.NewRGBA(image.Rect(0, 0, cut.Dx(), cut.Dy()))
	draw.Draw(out, out.Bounds(), src, cut.Min, draw.Src)
	return out, box.Sub(cut.Min), nil
}

// ItemName is the name the scan gave the item: the analyze events of
// request_id (events-*.jsonl in dir, the newest day first), the box whose
// answer item is item. "" when it is not found.
func ItemName(dir, requestID string, item int) string {
	if requestID == "" || item <= 0 {
		return ""
	}
	files, _ := filepath.Glob(filepath.Join(dir, "events-*.jsonl"))
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			if !bytes.Contains(sc.Bytes(), []byte(requestID)) {
				continue
			}
			var ev struct {
				RequestID string     `json:"request_id"`
				Boxes     []EventBox `json:"boxes"`
			}
			if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.RequestID != requestID {
				continue
			}
			for _, b := range ev.Boxes {
				if b.Item == item && b.Name != "" {
					fh.Close()
					return b.Name
				}
			}
		}
		fh.Close()
	}
	return ""
}

// Compare makes the X48h variants of one item photo (T9 2026-10-30: "for
// packaging that is close to others i would like it isolated through
// blurring the background and sharpened / enlarged. so essentially i want
// studio blur and muse bigger"). src is what the edits start from: the item
// photo, or ItemSource of it (note says which); target is the item's box in
// src (empty: not known), so a neighbour is not cut out with the item.
//
//	original     the item photo as it is
//	sharp_blur   the combo: src made MuseInputEdge big in code, Muse Image
//	             BiggerPrompt, SSIM >= SSIMMin; then SAM finds the item and
//	             its container on that edit, the item is cut out and the edit
//	             itself, blurred, goes behind it. A refused edit: the code look.
//	code_blur    the same without Muse Image (StudioLook on src), to compare
//	muse_bigger  the Muse Image edit alone, with its score
func (v *V2) Compare(ctx context.Context, photo []byte, src image.Image, target image.Rectangle, note string, concepts []string) ([]Variant, error) {
	item, _, err := image.Decode(bytes.NewReader(photo))
	if err != nil {
		return nil, err
	}
	if src == nil {
		src, note, target = item, "the item photo", image.Rectangle{}
	}
	ib := item.Bounds()
	out := []Variant{{Name: "original", W: ib.Dx(), H: ib.Dy(), JPEG: photo, Note: note}}
	in := src
	if sb := src.Bounds(); max(sb.Dx(), sb.Dy()) < MuseInputEdge {
		in = analyze.Resize(src, MuseInputEdge)
	}
	inJPEG, err := analyze.JPEG(in, 92)
	if err != nil {
		return nil, err
	}
	t0 := time.Now()
	type edit struct {
		jb []byte
		ic ImageCall
		e  error
	}
	museC := make(chan edit, 1)
	go func() { jb, ic, e := v.EditJSON(ctx, inJPEG, BiggerPrompt); museC <- edit{jb, ic, e} }()
	// code_blur while Muse works: the code look, the item and its container on src
	cb := Variant{Name: "code_blur", Note: note}
	cmask, clabel, cn, ccost, cerr := v.SubjectMask(ctx, src, concepts, ContainerConcepts, target)
	cb.CostUSD, cb.Concept, cb.Masks = ccost, clabel, cn
	codeImg, _ := blurStudio(src, analyze.StudioLook(), cmask, cerr)
	if cerr != nil {
		cb.Error = cerr.Error() + " (no new background; the look only)"
	}
	done(&cb, codeImg, t0)
	// the Muse edit and its check
	e := <-museC
	mb := Variant{Name: "muse_bigger", CostUSD: e.ic.CostUSD, Note: note, Error: e.ic.Error}
	var museImg image.Image
	if e.e == nil {
		im, _, derr := image.Decode(bytes.NewReader(e.jb))
		if derr != nil {
			mb.Error = "the edit is not an image"
		} else {
			s := analyze.SSIM(src, im)
			// the same shape too (X48i review): SSIM scales the edit to src's shape,
			// so an edit that crops or pads must not pass, and the item's box must scale
			sb, eb := src.Bounds(), im.Bounds()
			ra, rb := float64(sb.Dx())/float64(sb.Dy()), float64(eb.Dx())/float64(eb.Dy())
			ok := s >= SSIMMin && math.Abs(ra-rb) <= 0.03*ra
			if !ok && s >= SSIMMin {
				mb.Error = fmt.Sprintf("the edit has another shape (%dx%d for %dx%d)", eb.Dx(), eb.Dy(), sb.Dx(), sb.Dy())
			}
			mb.SSIM, mb.Passed = &s, &ok
			done(&mb, im, t0)
			if ok {
				museImg = im
			}
		}
	}
	mb.MS = e.ic.MS
	// the combo
	sv := Variant{Name: "sharp_blur", CostUSD: mb.CostUSD, Note: note, SSIM: mb.SSIM, Passed: mb.Passed}
	if museImg == nil {
		sv.Error = "the Muse edit was refused or failed: the code look is used"
		sv.Concept, sv.Masks, sv.JPEG, sv.W, sv.H = cb.Concept, cb.Masks, cb.JPEG, cb.W, cb.H
		sv.MS = time.Since(t0).Milliseconds() // the time until the edit was refused, with the code result
	} else {
		// the edit is src at another size (the check passed): the target scales with it
		mbd, sbd := museImg.Bounds(), src.Bounds()
		mt := image.Rect(target.Min.X*mbd.Dx()/sbd.Dx(), target.Min.Y*mbd.Dy()/sbd.Dy(),
			target.Max.X*mbd.Dx()/sbd.Dx(), target.Max.Y*mbd.Dy()/sbd.Dy())
		m, label, n, cost, err := v.SubjectMask(ctx, museImg, concepts, ContainerConcepts, mt)
		sv.CostUSD += cost
		sv.Concept, sv.Masks = label, n
		light := analyze.Look{Levels: 0.5, Saturation: 1.05, Sharpen: 0.3, LongEdge: 1600}
		img, _ := blurStudio(museImg, light, m, err)
		if err != nil {
			sv.Error = err.Error() + " (the sharp edit, no new background)"
		}
		done(&sv, img, t0)
	}
	return append(out, sv, cb, mb), nil
}

// blurStudio is the look on src, cut to the mask (plus 12%) with the photo
// itself blurred behind the item; with no mask (merr), the look only.
func blurStudio(src image.Image, look analyze.Look, mask []byte, merr error) (image.Image, analyze.Look) {
	if merr != nil || mask == nil {
		return analyze.Enhance(src, look), look
	}
	sb := src.Bounds()
	look.Crop = analyze.GridCrop(analyze.MaskBounds(mask, sb.Dx(), sb.Dy()), sb.Dx(), sb.Dy(), 0.12)
	fg := analyze.Enhance(src, look)
	fb := fg.Bounds()
	alpha := analyze.MaskTo(mask, sb.Dx(), sb.Dy(), look.Crop, fb.Dx(), fb.Dy(), max(2, max(fb.Dx(), fb.Dy())/150))
	return analyze.Studio(fg, alpha, "blur"), look
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
