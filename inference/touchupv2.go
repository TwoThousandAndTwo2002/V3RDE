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
	m, label, n, cost, _, err := v.subjectMask(ctx, img, concepts, containers, target)
	return m, label, n, cost, err
}

// subjectMask is SubjectMask and the spill: the share of the item's mask
// pixels that were outside the item's box + 5% and were cut away (X48m: a
// mask that spills is a pile next to another pile; 0 with no target).
func (v *V2) subjectMask(ctx context.Context, img image.Image, concepts, containers []string, target image.Rectangle) ([]byte, string, int, float64, float64, error) {
	jb, err := small(img, 1536)
	if err != nil {
		return nil, "", 0, 0, 0, err
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
	// pick is the item's instances of one concept: one rule for every kind of
	// item (X48j, T9: "these rules have to be as general as possible"). With
	// a target (the item's box from the scan): every instance whose box is at
	// least 70% inside the target (the pieces of a pile, the parts of a meal),
	// and the instance that matches the target best when its IoU is at least
	// 0.3 (one item in one mask). With no target: every instance that touches
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
			if area(bx) > 0 && inter >= 0.7*area(bx) {
				keep = append(keep, in)
			}
		}
		if best >= 0 && bestIoU >= 0.3 && !(area(boxOf(insts[best])) > 0 && area(boxOf(insts[best]).Intersect(target)) >= 0.7*area(boxOf(insts[best]))) {
			keep = append(keep, insts[best])
		}
		return keep
	}
	// grow is the target with 5% of its size on each side: the food mask is
	// cut to it, so a neighbour that SAM put in the same mask is cut away. A
	// container is not cut (the tray around a meal can be wider than the food).
	grow := image.Rectangle{}
	if !target.Empty() {
		gx, gy := target.Dx()/20, target.Dy()/20
		grow = image.Rect(target.Min.X-gx, target.Min.Y-gy, target.Max.X+gx, target.Max.Y+gy).Intersect(image.Rect(0, 0, w, h))
	}
	var log []string
	for i := range concepts {
		keep := pick(got[i].inst)
		m, n := MaskRaster(keep, w, h)
		log = append(log, fmt.Sprintf("%s %d/%d", concepts[i], n, len(got[i].inst)))
		set, cut := 0, 0
		if grow.Empty() {
			for _, x := range m {
				set += int(x)
			}
			if n == 0 || float64(set) < 0.03*float64(w*h) || float64(set) > 0.92*float64(w*h) {
				continue
			}
		} else {
			inT := 0
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					if m[y*w+x] == 0 {
						continue
					}
					if !(image.Point{x, y}).In(grow) {
						m[y*w+x] = 0
						cut++
						continue
					}
					set++
					if (image.Point{x, y}).In(target) {
						inT++
					}
				}
			}
			// the item must fill at least 30% of its box (a few stray pieces are not the item)
			if n == 0 || float64(inT) < 0.3*area(target) {
				continue
			}
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
		spill := 0.0
		if set+cut > 0 {
			spill = float64(cut) / float64(set+cut)
		}
		return m, label + " (SAM kept/found: " + strings.Join(log, ", ") + ")", n, cost, spill, nil
	}
	return nil, "", 0, cost, 0, errors.New("sam: no mask of the item (SAM kept/found: " + strings.Join(log, ", ") + ")")
}

// ItemSource cuts the item again from the uploaded photo (orig) with a
// margin (a share of the box: CropMargin as the item photo, or wider for a
// small one), and gives the item's box in the cut's pixels: the target of
// SubjectMask (X48i). meta is the item photo's record (Kind "crop", Box,
// FrameW/H, the box in SAM's frame of the upright upload).
func ItemSource(orig []byte, meta ItemPhotoMeta, margin float64, others ...[4]int) (image.Image, image.Rectangle, []image.Rectangle, error) {
	if meta.Kind != "crop" || len(meta.Box) != 4 || meta.FrameW <= 0 || meta.FrameH <= 0 {
		return nil, image.Rectangle{}, nil, errors.New("not a crop with a box")
	}
	src, _, err := image.Decode(bytes.NewReader(orig))
	if err != nil {
		return nil, image.Rectangle{}, nil, err
	}
	src = Upright(src, Orientation(orig))
	bd := src.Bounds()
	sx, sy := float64(bd.Dx())/float64(meta.FrameW), float64(bd.Dy())/float64(meta.FrameH)
	l, t, r, b := float64(meta.Box[0]), float64(meta.Box[1]), float64(meta.Box[2]), float64(meta.Box[3])
	mx, my := margin*(r-l), margin*(b-t)
	cut := image.Rect(int((l-mx)*sx), int((t-my)*sy), int((r+mx)*sx+0.5), int((b+my)*sy+0.5)).Add(bd.Min).Intersect(bd)
	box := image.Rect(int(l*sx), int(t*sy), int(r*sx+0.5), int(b*sy+0.5)).Add(bd.Min).Intersect(cut)
	if cut.Empty() || box.Empty() {
		return nil, image.Rectangle{}, nil, errors.New("crop: empty box")
	}
	out := image.NewRGBA(image.Rect(0, 0, cut.Dx(), cut.Dy()))
	draw.Draw(out, out.Bounds(), src, cut.Min, draw.Src)
	// X48k: the scan's other boxes (the same SAM frame) in the cut's pixels
	var os2 []image.Rectangle
	for _, o := range others {
		r := image.Rect(int(float64(o[0])*sx), int(float64(o[1])*sy), int(float64(o[2])*sx+0.5), int(float64(o[3])*sy+0.5)).Add(bd.Min).Intersect(cut)
		if !r.Empty() {
			os2 = append(os2, r.Sub(cut.Min))
		}
	}
	return out, box.Sub(cut.Min), os2, nil
}

// ItemName is the name the scan gave the item (ItemScan).
func ItemName(dir, requestID string, item int) string {
	name, _ := ItemScan(dir, requestID, item)
	return name
}

// ItemScan reads the analyze events of request_id (events-*.jsonl in dir,
// the newest day first): the name of the box whose answer item is item, and
// the boxes of every other object of that scan (X48k: the "not this" points;
// any outcome, a not-food box is a neighbour too). "" and nil when not found.
func ItemScan(dir, requestID string, item int) (string, [][4]int) {
	files, _ := filepath.Glob(filepath.Join(dir, "events-*.jsonl"))
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	return itemScan(files, requestID, item)
}

// ItemScanNear is ItemScan for a scan made at ts (RFC 3339, the item photo
// record's time): it reads only that UTC day's events and the next day's
// (X48o review: the service must not read the whole event history per job).
// A ts that does not parse finds nothing.
func ItemScanNear(dir, requestID string, item int, ts string) (string, [][4]int) {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return "", nil
	}
	t = t.UTC()
	var files []string
	for _, d := range []time.Time{t.AddDate(0, 0, 1), t} {
		f := filepath.Join(dir, "events-"+d.Format("20060102")+".jsonl")
		if _, err := os.Stat(f); err == nil {
			files = append(files, f)
		}
	}
	return itemScan(files, requestID, item)
}

func itemScan(files []string, requestID string, item int) (string, [][4]int) {
	if requestID == "" || item <= 0 {
		return "", nil
	}
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
			found, name := false, ""
			var others [][4]int
			for _, b := range ev.Boxes {
				if b.Item == item {
					found = true
					if name == "" {
						name = b.Name
					}
				} else {
					others = append(others, b.Box)
				}
			}
			if !found { // another event of the same request (X48k review): look on
				continue
			}
			fh.Close()
			return name, others
		}
		fh.Close()
	}
	return "", nil
}

// X48k (T9 2026-10-30, after a review of how others do it): the
// established way (Grounded-SAM) gives each detector box to SAM as a box
// prompt and keeps the one mask; our scan is the detector. SAM-PD
// (arxiv.org/abs/2403.04194) adds a second step: one positive point inside
// the first mask and negative points sampled from the other objects. The
// service takes one kind of prompt per request, so the two steps are two
// requests (docs/sources/X48k.md).

// samPrompt sends one SAM request with a box or point prompt (prompt is the
// token text) on img made at most 1536 px; the instances are in that JPEG's
// frame (MaskRaster scales them).
func (v *V2) samPrompt(ctx context.Context, jb []byte, prompt, purpose string) ([]Instance, float64, error) {
	// X48l: without mask_encoding one_bit, a box or point prompt answers in the
	// lossless "~" encoding, which DecodeOneBit does not read (box debug run,
	// 2026-10-30): the mask was dropped. With it the answer is "!" (one_bit).
	c, err := v.API.Do(ctx, purpose, map[string]any{"model": v.SAM,
		"input":    UserMessage(TextPart(prompt), ImagePart(jb, "image/jpeg")),
		"metadata": map[string]any{"mask_encoding": "one_bit"}})
	cost := 0.0
	if c != nil {
		cost = c.Cost()
	}
	if err != nil {
		return nil, cost, err
	}
	inst, _ := ParseSAM(c.Text())
	return inst, cost, nil
}

// promptFrame is img as the JPEG SAM gets and the scale from img's pixels to it.
func promptFrame(img image.Image) ([]byte, int, int, float64, error) {
	jb, err := small(img, 1536)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(jb))
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return jb, cfg.Width, cfg.Height, float64(cfg.Width) / float64(img.Bounds().Dx()), nil
}

// BoxMask is step 1: the item's box (in img's pixels) as a SAM box prompt;
// the mask of object 1 as a raster of img's size.
func (v *V2) BoxMask(ctx context.Context, img image.Image, box image.Rectangle) ([]byte, int, float64, error) {
	jb, fw, fh, k, err := promptFrame(img)
	if err != nil {
		return nil, 0, 0, err
	}
	sc := func(x int, lim int) int { return max(0, min(lim-1, int(float64(x)*k))) }
	prompt := fmt.Sprintf("<0f>1<|box;x1=%d;y1=%d;x2=%d;y2=%d;w=%d;h=%d|>",
		sc(box.Min.X, fw), sc(box.Min.Y, fh), sc(box.Max.X-1, fw), sc(box.Max.Y-1, fh), fw, fh)
	inst, cost, err := v.samPrompt(ctx, jb, prompt, "sam:box")
	if err != nil {
		return nil, 0, cost, err
	}
	b := img.Bounds()
	m, n := MaskRaster(inst, b.Dx(), b.Dy())
	if n == 0 {
		return nil, 0, cost, errors.New("sam: no mask for the item's box")
	}
	return m, n, cost, nil
}

// RefineMask is step 2 (SAM-PD): one positive point (the pixel of rough
// nearest its centre) and one negative point at the centre of each other
// object of the scan that is in the photo, except an object whose centre is
// inside the item's own box (target): that is the item's plate or tray, or
// the same item again (X48k review). With no other object there is nothing
// to refine: rough is returned.
func (v *V2) RefineMask(ctx context.Context, img image.Image, rough []byte, target image.Rectangle, others []image.Rectangle) ([]byte, int, int, float64, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	var sx, sy, cnt float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if rough[y*w+x] != 0 {
				sx, sy, cnt = sx+float64(x), sy+float64(y), cnt+1
			}
		}
	}
	if cnt == 0 {
		return nil, 0, 0, 0, errors.New("refine: the first mask is empty")
	}
	cx, cy := sx/cnt, sy/cnt
	px, py, bestD := 0, 0, math.MaxFloat64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if rough[y*w+x] != 0 {
				if d := (float64(x)-cx)*(float64(x)-cx) + (float64(y)-cy)*(float64(y)-cy); d < bestD {
					px, py, bestD = x, y, d
				}
			}
		}
	}
	var negs []image.Point
	for _, o := range others {
		c := image.Pt((o.Min.X+o.Max.X)/2, (o.Min.Y+o.Max.Y)/2)
		if c.In(b) && !c.In(target) {
			negs = append(negs, c)
		}
	}
	if len(negs) == 0 {
		return rough, 0, 0, 0, nil
	}
	jb, fw, fh, k, err := promptFrame(img)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	pt := func(p image.Point) string {
		return fmt.Sprintf("<|point;x=%d;y=%d;w=%d;h=%d|>", max(0, min(fw-1, int(float64(p.X)*k))), max(0, min(fh-1, int(float64(p.Y)*k))), fw, fh)
	}
	prompt := "<0f>1" + pt(image.Pt(px, py))
	for _, n := range negs {
		prompt += "-" + pt(n)
	}
	inst, cost, err := v.samPrompt(ctx, jb, prompt, "sam:refine")
	if err != nil {
		return nil, len(negs), 0, cost, err
	}
	m, n := MaskRaster(inst, w, h)
	if n == 0 {
		return nil, len(negs), 0, cost, errors.New("sam: no mask for the refined points")
	}
	return m, len(negs), n, cost, nil
}

// WithContainers adds the containers that hold the food (D216) to m: SAM is
// asked for each container concept at the same time; a container whose box
// holds at least half of the food's pixels is added.
func (v *V2) WithContainers(ctx context.Context, img image.Image, m []byte) (string, int, float64) {
	jb, err := small(img, 1536)
	if err != nil {
		return "", 0, 0
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	set := 0
	for _, x := range m {
		set += int(x)
	}
	got := make([][]Instance, len(ContainerConcepts))
	costs := make([]float64, len(ContainerConcepts))
	var wg sync.WaitGroup
	for i, c := range ContainerConcepts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call, err := v.API.Do(ctx, "sam:"+c, map[string]any{"model": v.SAM,
				"input":    UserMessage(TextPart(c), ImagePart(jb, "image/jpeg")),
				"metadata": map[string]any{"include_confidence": "true", "mask_encoding": "one_bit"}})
			if call != nil {
				costs[i] = call.Cost()
			}
			if err == nil {
				got[i], _ = ParseSAM(call.Text())
			}
		}()
	}
	wg.Wait()
	label, n, cost := "", 0, 0.0
	for i, c := range ContainerConcepts {
		cost += costs[i]
		var hold []Instance
		for _, in := range got[i] {
			if in.W <= 0 || in.H <= 0 {
				continue
			}
			r := image.Rect(in.Left*w/in.W, in.Top*h/in.H, in.Right*w/in.W, in.Bottom*h/in.H).Intersect(image.Rect(0, 0, w, h))
			inside := 0
			for y := r.Min.Y; y < r.Max.Y; y++ {
				for x := r.Min.X; x < r.Max.X; x++ {
					inside += int(m[y*w+x])
				}
			}
			if set > 0 && 2*inside >= set {
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
	return label, n, cost
}

// Compare makes the X48k variants of one item photo. src is what the edits
// start from (the item photo, or ItemSource of it; note says which); target
// is the item's box in src and others the boxes of the scan's other objects
// in src (both empty when not known). Every variant is the combo T9 chose
// (X48h: Muse Image bigger + studio blur); only the mask differs:
//
//	original      the item photo as it is
//	words         the X48j mask: SAM concepts, the pieces inside the box
//	box           step 1: the item's box as a SAM box prompt (Grounded-SAM)
//	box_refine    step 2: + one positive point and negative points on the
//	              other objects (SAM-PD); = box when there is no other object
//	muse_bigger   the Muse Image edit alone, with its score
func (v *V2) Compare(ctx context.Context, photo []byte, src image.Image, target image.Rectangle, others []image.Rectangle, note string, concepts []string) ([]Variant, error) {
	item, _, err := image.Decode(bytes.NewReader(photo))
	if err != nil {
		return nil, err
	}
	if src == nil {
		src, note, target, others = item, "the item photo", image.Rectangle{}, nil
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
	jb, ic, eerr := v.EditJSON(ctx, inJPEG, BiggerPrompt)
	mb := Variant{Name: "muse_bigger", CostUSD: ic.CostUSD, Note: note, Error: ic.Error, MS: ic.MS}
	base, look := src, analyze.StudioLook()
	if eerr == nil {
		im, _, derr := image.Decode(bytes.NewReader(jb))
		if derr != nil {
			mb.Error = "the edit is not an image"
		} else {
			s := analyze.SSIM(src, im)
			// the same shape too (X48i review): an edit that crops or pads must not pass
			sb, eb := src.Bounds(), im.Bounds()
			ra, rb := float64(sb.Dx())/float64(sb.Dy()), float64(eb.Dx())/float64(eb.Dy())
			ok := s >= SSIMMin && math.Abs(ra-rb) <= 0.03*ra
			if !ok && s >= SSIMMin {
				mb.Error = fmt.Sprintf("the edit has another shape (%dx%d for %dx%d)", eb.Dx(), eb.Dy(), sb.Dx(), sb.Dy())
			}
			mb.SSIM, mb.Passed = &s, &ok
			done(&mb, im, t0)
			mb.MS = ic.MS
			if ok {
				base, look = im, analyze.Look{Levels: 0.5, Saturation: 1.05, Sharpen: 0.3, LongEdge: 1600}
			}
		}
	}
	refused := base == src
	// the boxes in base's pixels (the edit is src at another size)
	bb, sbd := base.Bounds(), src.Bounds()
	scale := func(r image.Rectangle) image.Rectangle {
		return image.Rect(r.Min.X*bb.Dx()/sbd.Dx(), r.Min.Y*bb.Dy()/sbd.Dy(), r.Max.X*bb.Dx()/sbd.Dx(), r.Max.Y*bb.Dy()/sbd.Dy())
	}
	t := scale(target)
	var os2 []image.Rectangle
	for _, o := range others {
		os2 = append(os2, scale(o))
	}
	mk := func(name string) Variant {
		r := Variant{Name: name, CostUSD: mb.CostUSD, Note: note, SSIM: mb.SSIM, Passed: mb.Passed}
		if refused {
			r.Error = "the Muse edit was refused or failed: the code look on the photo"
		}
		return r
	}
	finish := func(r *Variant, m []byte, merr error) {
		img, _ := blurStudio(base, look, m, merr)
		if merr != nil {
			r.Error = strings.TrimSpace(r.Error + " " + merr.Error() + " (no new background)")
		}
		done(r, img, t0)
	}
	vs := [3]Variant{mk("words"), mk("box"), mk("box_refine")}
	var wordsSpill float64
	var wordsErr, boxErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // words: the X48j mask (containers included)
		defer wg.Done()
		m, label, n, cost, spill, err := v.subjectMask(ctx, base, concepts, ContainerConcepts, t)
		wordsSpill, wordsErr = spill, err
		vs[0].CostUSD += cost
		vs[0].Concept, vs[0].Masks = label, n
		finish(&vs[0], m, err)
	}()
	go func() { // box, then the refine step on its mask
		defer wg.Done()
		if t.Empty() {
			for k := 1; k < 3; k++ {
				vs[k].Error = strings.TrimSpace(vs[k].Error + " no item box from the scan (a whole-photo item)")
				finish(&vs[k], nil, errors.New("no box"))
			}
			boxErr = errors.New("no box")
			return
		}
		m, n, cost, err := v.BoxMask(ctx, base, t)
		boxErr = err
		vs[1].CostUSD += cost
		vs[2].CostUSD += cost
		var rm []byte
		var rerr error
		negs, rn := 0, 0
		if err == nil {
			var rc float64
			rm, negs, rn, rc, rerr = v.RefineMask(ctx, base, m, t, os2)
			vs[2].CostUSD += rc
		} else {
			rerr = err
		}
		for k, mm := range [][]byte{m, rm} {
			if k == 1 && err == nil && rerr == nil && negs == 0 { // nothing to refine: the box result, paid once
				vs[2].JPEG, vs[2].W, vs[2].H, vs[2].Masks, vs[2].MS = vs[1].JPEG, vs[1].W, vs[1].H, vs[1].Masks, vs[1].MS
				vs[2].CostUSD, vs[2].Error = vs[1].CostUSD, vs[1].Error
				vs[2].Concept = "no other object in the scan: the box mask" + strings.TrimPrefix(vs[1].Concept, "box")
				continue
			}
			e := err
			if k == 1 {
				e = rerr
			}
			if e == nil && mm != nil {
				cp := append([]byte(nil), mm...)
				label, cn, cc := v.WithContainers(ctx, base, cp)
				vs[1+k].CostUSD += cc
				if k == 0 {
					vs[1].Concept, vs[1].Masks = "box"+label, n+cn
				} else {
					vs[2].Concept, vs[2].Masks = fmt.Sprintf("box + 1 point, %d not-this point(s)%s", negs, label), rn+cn
					if negs == 0 {
						vs[2].Concept = "no other object in the scan: the box mask" + label
					}
				}
				mm = cp
			}
			finish(&vs[1+k], mm, e)
		}
	}()
	wg.Wait()
	auto := Auto(vs[0], vs[1], wordsErr, boxErr, wordsSpill, !t.Empty())
	return append(append(append(out, auto), vs[:]...), mb), nil
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

// SpillMax: a words mask with more than this share of its pixels outside the
// item's box + 5% spilled into a neighbour (X48m).
const SpillMax = 0.03

// Auto is the mask T9 chose for each kind of scene (X48m, T9 2026-10-30: "for
// row 1 food in a plate of serving tray choose Mask from words, for rows 2-3
// choose Mask from the box (step 1), for times like in rows 4-6 use Mask from
// words. so it should be based on scene"), as one rule with no list of foods:
//
//	no item box from the scan (a whole-photo item: a meal in its tray)  words
//	the words mask stays inside the item's box (a package, a plate)     words
//	the words mask spills past the box, or words found nothing (a pile
//	next to another pile)                                               box
//
// When the chosen mask failed and the other did not, the other is used.
func Auto(words, box Variant, wordsErr, boxErr error, spill float64, hasBox bool) Variant {
	pick, why := words, "words: "
	switch {
	case !hasBox:
		why += "no item box from the scan (a whole-photo item)"
	case wordsErr != nil && boxErr == nil:
		pick, why = box, "box: the words found no mask of the item"
	case wordsErr == nil && spill > SpillMax && boxErr == nil:
		pick, why = box, fmt.Sprintf("box: the words mask spilled %.0f%% past the item's box (a pile next to another)", 100*spill)
	case wordsErr == nil && spill > SpillMax:
		why += fmt.Sprintf("the words mask spilled %.0f%%, and the box gave no mask", 100*spill)
	case wordsErr == nil:
		why += fmt.Sprintf("the words mask stays in the item's box (spill %.1f%%)", 100*spill)
	default:
		why += "no mask from the words or the box"
	}
	a := pick
	a.Name, a.Concept = "auto", why+"; "+pick.Concept
	return a
}
