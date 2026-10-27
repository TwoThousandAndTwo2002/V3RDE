package inference

// What Muse actually reads. dev.meta.ai/docs/image-understanding (READ
// 2026-10-26): "Image cost scales with resolution", "the model may resize
// images", and "Coordinates are relative to the image the model actually
// received" on "a normalized 0–1000 grid ... (0, 0) is the top-left corner;
// (1000, 1000) is the bottom-right". Measured the same day with
// POST /v1/responses/input_tokens (cmd/ie-tokens, shelf_14 at six sizes):
// image tokens = ceil(w/28) x ceil(h/28) + 2, and they stop at 4,096 + 2
// from 2240 px up. So the engine resizes a photo itself to the 4,096-token
// limit before a multi-item call: the marks it draws and the sizes it
// measures are then in the image Muse reads, and every run is repeatable.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"strconv"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	TokenPx        = 28   // one image token = 28 x 28 px (measured 2026-10-26)
	MaxImageTokens = 4096 // image tokens stop here (measured 2026-10-26)
	jpegQuality    = 92
)

// ImageTokens is the measured image-token count of a w x h image, without
// the 2 fixed tokens and before the cap.
func ImageTokens(w, h int) int { return ((w + TokenPx - 1) / TokenPx) * ((h + TokenPx - 1) / TokenPx) }

// Prepared is the photo as Muse will read it.
type Prepared struct {
	Img     image.Image
	W, H    int
	Scale   float64 // prepared px / original px (1 = not resized)
	Resized bool
}

// Prepare fits src inside MaxImageTokens (Catmull-Rom, aspect kept). A
// photo that already fits is not changed.
func Prepare(src image.Image) *Prepared {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if ImageTokens(w, h) <= MaxImageTokens {
		return &Prepared{Img: src, W: w, H: h, Scale: 1}
	}
	s := math.Sqrt(float64(MaxImageTokens*TokenPx*TokenPx) / float64(w*h))
	nw, nh := int(float64(w)*s), int(float64(h)*s)
	for ImageTokens(nw, nh) > MaxImageTokens {
		s *= 0.995
		nw, nh = int(float64(w)*s), int(float64(h)*s)
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	return &Prepared{Img: dst, W: nw, H: nh, Scale: float64(nw) / float64(w), Resized: true}
}

// JPEG encodes an image for the API.
func JPEG(img image.Image) ([]byte, error) { return jpegQ(img, jpegQuality) }

// samJPEGQuality: SAM gets a new JPEG only when the photo had to be turned
// upright; the highest quality keeps the second compression small (D22).
const samJPEGQuality = 98

func jpegQ(img image.Image, q int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Norm is a box on Muse's 0–1000 grid: [x1, y1, x2, y2].
type Norm [4]int

// NormBox maps a SAM box (pixels in SAM's W x H frame) to the 0–1000 grid.
// The grid does not depend on the resolution, so the same numbers are true
// for the original and the prepared photo.
func NormBox(b Instance) Norm {
	w, h := float64(b.W), float64(b.H)
	if w <= 0 || h <= 0 {
		return Norm{}
	}
	r := func(v, d float64) int { return int(math.Round(v / d * 1000)) }
	return Norm{r(float64(b.Left), w), r(float64(b.Top), h), r(float64(b.Right), w), r(float64(b.Bottom), h)}
}

// Seen is how large a box is in the image Muse reads.
type Seen struct {
	ShortPx float64 `json:"short_px"` // short side, prepared px
	AreaPct float64 `json:"area_pct"` // box area / photo area, percent
	Tokens  float64 `json:"tokens"`   // box area in image tokens
}

// SeenSize measures one box in a prepared photo of pw x ph.
func SeenSize(b Instance, pw, ph int) Seen {
	if b.W <= 0 || b.H <= 0 {
		return Seen{}
	}
	bw := float64(b.Right-b.Left) * float64(pw) / float64(b.W)
	bh := float64(b.Bottom-b.Top) * float64(ph) / float64(b.H)
	return Seen{ShortPx: math.Round(math.Min(bw, bh)*10) / 10,
		AreaPct: math.Round(bw*bh/float64(pw*ph)*100*1000) / 1000,
		Tokens:  math.Round(bw*bh/(TokenPx*TokenPx)*10) / 10}
}

// markColors: high-contrast label fills; the number is white on each.
var markColors = []color.RGBA{{220, 20, 60, 255}, {0, 90, 200, 255}, {0, 140, 70, 255}, {150, 40, 170, 255},
	{200, 90, 0, 255}, {0, 120, 140, 255}, {90, 90, 90, 255}, {170, 30, 100, 255}}

// DrawMarks returns a copy of the prepared photo with each box outlined and
// its number (1-based, in the given order) in a filled tag at its top-left
// corner: Set-of-Mark prompting (Yang et al. 2023, arXiv 2310.11441), which
// pairs each region with a visible number the model can cite. Meta's
// documentation does not describe drawn marks; the probe measures them.
func DrawMarks(p *Prepared, boxes []Instance) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, p.W, p.H))
	xdraw.Draw(dst, dst.Bounds(), p.Img, p.Img.Bounds().Min, xdraw.Src)
	short := min(p.W, p.H)
	line := max(2, short/400)
	scale := max(1, short/350) // basicfont is 7x13 px; scaled up on large photos
	for i, b := range boxes {
		c := markColors[i%len(markColors)]
		sx, sy := float64(p.W)/float64(b.W), float64(p.H)/float64(b.H)
		r := image.Rect(int(float64(b.Left)*sx), int(float64(b.Top)*sy),
			int(float64(b.Right)*sx), int(float64(b.Bottom)*sy)).Intersect(dst.Bounds())
		outline(dst, r, line, c)
		tag(dst, r.Min, strconv.Itoa(i+1), scale, c)
	}
	return dst
}

func outline(dst *image.RGBA, r image.Rectangle, t int, c color.RGBA) {
	u := image.NewUniform(c)
	for _, s := range []image.Rectangle{
		{r.Min, image.Pt(r.Max.X, r.Min.Y+t)}, {image.Pt(r.Min.X, r.Max.Y-t), r.Max},
		{r.Min, image.Pt(r.Min.X+t, r.Max.Y)}, {image.Pt(r.Max.X-t, r.Min.Y), r.Max},
	} {
		xdraw.Draw(dst, s.Intersect(dst.Bounds()), u, image.Point{}, xdraw.Src)
	}
}

// tag draws the number in white on a filled box, scale x the 7x13 font,
// inside the photo.
func tag(dst *image.RGBA, at image.Point, s string, scale int, c color.RGBA) {
	face := basicfont.Face7x13
	pad := 2
	small := image.NewRGBA(image.Rect(0, 0, len(s)*7+2*pad, 13+2*pad))
	xdraw.Draw(small, small.Bounds(), image.NewUniform(c), image.Point{}, xdraw.Src)
	d := &font.Drawer{Dst: small, Src: image.White, Face: face, Dot: fixed.P(pad, pad+face.Ascent)}
	d.DrawString(s)
	w, h := small.Bounds().Dx()*scale, small.Bounds().Dy()*scale
	x := min(max(at.X, 0), dst.Bounds().Dx()-w)
	y := min(max(at.Y, 0), dst.Bounds().Dy()-h)
	xdraw.NearestNeighbor.Scale(dst, image.Rect(x, y, x+w, y+h), small, small.Bounds(), xdraw.Src, nil)
}

// String is for logs.
func (p *Prepared) String() string {
	return fmt.Sprintf("%dx%d (scale %.3f, %d tokens)", p.W, p.H, p.Scale, ImageTokens(p.W, p.H))
}
