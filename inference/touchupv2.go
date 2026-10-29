package inference

// X48f: the touch-up v2 prototype (T9 2026-10-29: "isolate the item and
// enhance the sharpness ... make it bigger"; "i wanna try 1 and then 2":
// first the hybrid, then code only). It is used by cmd/touchupcompare, which
// makes the side-by-side page for T9; the touch-up service does not change
// until T9 chooses.
//
//	enhance   code only: analyze.Enhance with analyze.AutoLook (no Meta call)
//	smart     Muse Spark chooses the look (analyze.LookPrompt: numbers only),
//	          then the same code makes the edit
//	smart_bg  smart, then Muse Image paints a plain background and only its
//	          background is used: the food pixels come from the smart edit
//	          through a SAM 3.1 mask of "food" (analyze.Composite)
//	current   the touch-up of today (Muse Image "light", post/touchup.go)

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
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// The prompts of the Muse Image calls of the prototype.
const (
	// CurrentPrompt is post.EditPrompts["light"] of today (the "current" column).
	CurrentPrompt = "Improve this food photo for a food-sharing post: correct the exposure, the white balance " +
		"and the sharpness only. Do not add, remove, move, resize or change any food item, package, " +
		"container, label, text or quantity. Do not change the color, the texture or how fresh the food " +
		"looks. Keep the same framing and the same camera angle."
	// BackgroundPrompt: only the background of this edit is used (the food is
	// put back from the real photo), so the edit may not move the food.
	BackgroundPrompt = "Replace everything behind and around the food with a plain, softly lit, light neutral " +
		"surface. Keep the food, its container and its position exactly as they are, and keep the same framing " +
		"and the same camera angle. Do not add anything."
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

// Look asks Muse Spark for the settings of img (over AutoLook).
func (v *V2) Look(ctx context.Context, img image.Image) (analyze.Look, *Call, error) {
	jb, err := small(img, 1024)
	if err != nil {
		return analyze.AutoLook(), nil, err
	}
	c, err := v.API.Do(ctx, "touchup:look", map[string]any{"model": v.Muse,
		"input": UserMessage(TextPart(analyze.LookPrompt), ImagePart(jb, "image/jpeg"))})
	if err != nil {
		return analyze.AutoLook(), c, err
	}
	obj, err := analyze.ExtractJSON(c.Text())
	if err != nil {
		return analyze.AutoLook(), c, fmt.Errorf("look: %w", err)
	}
	return analyze.LookFrom(obj, analyze.AutoLook()), c, nil
}

// FoodMask is the union of SAM's "food" masks on img, as a w*h 0/1 raster of
// img's size, and how many instances it holds.
func (v *V2) FoodMask(ctx context.Context, img image.Image) ([]byte, int, *Call, error) {
	jb, err := small(img, 1536)
	if err != nil {
		return nil, 0, nil, err
	}
	c, err := v.API.Do(ctx, "sam:food", map[string]any{"model": v.SAM,
		"input":    UserMessage(TextPart("food"), ImagePart(jb, "image/jpeg")),
		"metadata": map[string]any{"include_confidence": "true", "mask_encoding": "one_bit"}})
	if err != nil {
		return nil, 0, c, err
	}
	inst, _ := ParseSAM(c.Text())
	b := img.Bounds()
	m, n := MaskRaster(inst, b.Dx(), b.Dy())
	if n == 0 {
		return nil, 0, c, errors.New("sam: no food mask")
	}
	return m, n, c, nil
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

// Edit is one Muse Image edit (the multipart call of post/touchup.go).
func (v *V2) Edit(ctx context.Context, img []byte, prompt string) ([]byte, ImageCall, error) {
	var ic ImageCall
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fields := [][2]string{{"model", v.ImageModel}, {"prompt", prompt}, {"n", "1"},
		{"output_format", "jpeg"}, {"response_format", "b64_json"}}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(img)); err == nil {
		fields = append(fields, [2]string{"size", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height)})
	}
	for _, f := range fields {
		_ = mw.WriteField(f[0], f[1])
	}
	hd := textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="image"; filename="photo.jpg"`)
	hd.Set("Content-Type", "image/jpeg")
	pw, _ := mw.CreatePart(hd)
	_, _ = pw.Write(img)
	_ = mw.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(v.ImageURL, "/")+"/images/edits", &buf)
	if err != nil {
		return nil, ic, err
	}
	req.Header.Set("Authorization", "Bearer "+v.API.Key)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	hc := v.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 120 * time.Second}
	}
	t0 := time.Now()
	resp, err := hc.Do(req)
	ic.MS = time.Since(t0).Milliseconds()
	if err != nil {
		ic.Error = err.Error()
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
		} `json:"data"`
	}
	if json.Unmarshal(raw, &out) != nil || len(out.Data) == 0 {
		ic.Error = "no image in the answer"
		return nil, ic, errors.New(ic.Error)
	}
	b, err := base64.StdEncoding.DecodeString(out.Data[0].B64)
	if err != nil || len(b) == 0 {
		ic.Error = "image is not base64"
		return nil, ic, errors.New(ic.Error)
	}
	return b, ic, nil
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
	JPEG    []byte        `json:"-"`
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

// Compare makes every variant of one photo; the Meta calls run at the same
// time where they can (current with the rest; SAM with the background edit).
func (v *V2) Compare(ctx context.Context, photo []byte, withCurrent, withBG bool) ([]Variant, error) {
	src, _, err := image.Decode(bytes.NewReader(photo))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	out := []Variant{{Name: "original", W: b.Dx(), H: b.Dy(), JPEG: photo}}
	cur := make(chan Variant, 1)
	if withCurrent {
		go func() {
			r := Variant{Name: "current"}
			t0 := time.Now()
			jb, ic, err := v.Edit(ctx, photo, CurrentPrompt)
			r.CostUSD = ic.CostUSD
			if err == nil {
				if im, _, derr := image.Decode(bytes.NewReader(jb)); derr == nil {
					done(&r, im, t0)
				} else {
					r.Error = "the edit is not an image"
				}
			} else {
				r.Error = err.Error()
			}
			r.MS = time.Since(t0).Milliseconds()
			cur <- r
		}()
	}
	// code only
	e := Variant{Name: "enhance"}
	t0 := time.Now()
	auto := analyze.AutoLook()
	done(&e, analyze.Enhance(src, auto), t0)
	e.Look = &auto
	out = append(out, e)
	// smart: the look from Muse Spark
	s := Variant{Name: "smart"}
	t0 = time.Now()
	look, c, err := v.Look(ctx, src)
	if c != nil {
		s.CostUSD += c.Cost()
	}
	if err != nil {
		s.Error = "look: " + err.Error() + " (AutoLook used)"
	}
	smartImg := analyze.Enhance(src, look)
	done(&s, smartImg, t0)
	s.Look = &look
	out = append(out, s)
	if withBG {
		g := Variant{Name: "smart_bg", CostUSD: s.CostUSD}
		type maskRes struct {
			m []byte
			n int
			c *Call
			e error
		}
		mc := make(chan maskRes, 1)
		go func() {
			m, n, c, err := v.FoodMask(ctx, smartImg)
			mc <- maskRes{m, n, c, err}
		}()
		bg, ic, berr := v.Edit(ctx, s.JPEG, BackgroundPrompt)
		mr := <-mc
		g.CostUSD += ic.CostUSD
		if mr.c != nil {
			g.CostUSD += mr.c.Cost()
		}
		var bgImg image.Image
		if berr == nil {
			bgImg, _, berr = image.Decode(bytes.NewReader(bg))
		}
		switch {
		case berr != nil:
			g.Error = "background: " + berr.Error()
		case mr.e != nil:
			g.Error = "mask: " + mr.e.Error()
		default:
			g.Masks = mr.n
			feather := max(2, max(smartImg.Bounds().Dx(), smartImg.Bounds().Dy())/250)
			done(&g, analyze.Composite(smartImg, bgImg, mr.m, feather), time.Now())
		}
		g.MS = time.Since(t0).Milliseconds() // from the start of smart: Spark, the code, then SAM and the edit at the same time
		out = append(out, g)
	}
	if withCurrent {
		out = append(out, <-cur)
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
