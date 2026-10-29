package analyze

// X48f: the code edit of touch-up v2 (enhance.go).

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"testing"
)

func flat(w, h int, f func(x, y int) color.RGBA) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.SetRGBA(x, y, f(x, y))
		}
	}
	return m
}

func mean(m *image.RGBA) (r, g, b float64) {
	n := 0.0
	for y := m.Rect.Min.Y; y < m.Rect.Max.Y; y++ {
		for x := m.Rect.Min.X; x < m.Rect.Max.X; x++ {
			c := m.RGBAAt(x, y)
			r, g, b, n = r+float64(c.R), g+float64(c.G), b+float64(c.B), n+1
		}
	}
	return r / n, g / n, b / n
}

// The crop is on the 0-1000 grid; the result is LongEdge long, at most 2x the
// crop; no crop and no LongEdge keep the size.
func TestEnhanceSizeAndCrop(t *testing.T) {
	src := flat(400, 300, func(x, y int) color.RGBA { return color.RGBA{uint8(x / 2), uint8(y / 2), 100, 255} })
	l := Look{Crop: [4]int{250, 250, 750, 750}, LongEdge: 400, Saturation: 1}
	if b := Enhance(src, l).Bounds(); b.Dx() != 400 || b.Dy() != 300 {
		t.Fatalf("crop 200x150 to 400: %v", b)
	}
	l.LongEdge = 10000
	if b := Enhance(src, l).Bounds(); b.Dx() != 400 || b.Dy() != 300 {
		t.Fatalf("at most 2x: %v", b)
	}
	if b := Enhance(src, Look{Saturation: 1}).Bounds(); b.Dx() != 400 || b.Dy() != 300 {
		t.Fatalf("no change of size: %v", b)
	}
	// a crop smaller than a fifth of a side is not used
	if r := CropRect(src.Bounds(), [4]int{0, 0, 100, 1000}); r != src.Bounds() {
		t.Fatalf("thin crop: %v", r)
	}
	// down: 400 -> 100 keeps the mean colour (no aliasing to another colour)
	small := Enhance(src, Look{LongEdge: 100, Saturation: 1})
	r0, g0, _ := mean(src)
	r1, g1, _ := mean(small)
	if small.Bounds().Dx() != 100 || math.Abs(r0-r1) > 2 || math.Abs(g0-g1) > 2 {
		t.Fatalf("down: %v %.1f/%.1f %.1f/%.1f", small.Bounds(), r0, r1, g0, g1)
	}
}

// A blue cast on a grey photo: white balance brings the channels together;
// warmth makes red go up and blue go down.
func TestEnhanceWhiteBalanceAndWarmth(t *testing.T) {
	src := flat(200, 200, func(x, y int) color.RGBA {
		v := uint8(90 + (x+y)%40)
		return color.RGBA{v, v + 10, v + 40, 255}
	})
	r0, _, b0 := mean(src)
	r1, _, b1 := mean(Enhance(src, Look{WhiteBalance: 1, Saturation: 1}))
	if b1-r1 > (b0-r0)/3 {
		t.Fatalf("cast not removed: before %.1f, after %.1f", b0-r0, b1-r1)
	}
	r2, _, b2 := mean(Enhance(src, Look{Warmth: 0.2, Saturation: 1}))
	if r2 <= r0 || b2 >= b0 {
		t.Fatalf("warmth: r %.1f -> %.1f, b %.1f -> %.1f", r0, r2, b0, b2)
	}
}

// Levels stretch a flat photo; exposure +0.5 makes it brighter; saturation 1
// and no other setting change nothing.
func TestEnhanceLevelsExposure(t *testing.T) {
	src := flat(200, 100, func(x, y int) color.RGBA { v := uint8(100 + x/4); return color.RGBA{v, v, v, 255} })
	out := Enhance(src, Look{Levels: 1, Saturation: 1})
	lo, hi := out.RGBAAt(0, 50).R, out.RGBAAt(199, 50).R
	if lo > 10 || hi < 245 {
		t.Fatalf("levels: %d..%d", lo, hi)
	}
	r0, _, _ := mean(src)
	r1, _, _ := mean(Enhance(src, Look{Exposure: 0.5, Saturation: 1}))
	if r1 < r0+10 {
		t.Fatalf("exposure: %.1f -> %.1f", r0, r1)
	}
	same := Enhance(src, Look{Saturation: 1})
	for x := 0; x < 200; x += 37 {
		if a, b := src.RGBAAt(x, 50), same.RGBAAt(x, 50); a != b {
			t.Fatalf("no setting changed pixel %d: %v %v", x, a, b)
		}
	}
}

// The unsharp mask raises the contrast at an edge.
func TestEnhanceSharpen(t *testing.T) {
	src := flat(100, 50, func(x, y int) color.RGBA {
		if x < 50 {
			return color.RGBA{80, 80, 80, 255}
		}
		return color.RGBA{160, 160, 160, 255}
	})
	out := Enhance(src, Look{Sharpen: 1.5, Saturation: 1})
	if a, b := out.RGBAAt(48, 25).R, out.RGBAAt(51, 25).R; !(a < 80 && b > 160) {
		t.Fatalf("edge: %d %d", a, b)
	}
}

// The model's numbers are clamped; a wrong or small crop keeps the base.
func TestLookFrom(t *testing.T) {
	base := AutoLook()
	l := LookFrom(map[string]any{"exposure": 3.0, "warmth": -1.0, "saturation": 1.2, "sharpen": "a lot",
		"crop": []any{100.0, 50.0, 900.0, 950.0}}, base)
	if l.Exposure != 0.5 || l.Warmth != -0.2 || l.Saturation != 1.2 || l.Sharpen != base.Sharpen ||
		l.Crop != [4]int{100, 50, 900, 950} || l.LongEdge != base.LongEdge {
		t.Fatalf("%+v", l)
	}
	for _, c := range []any{[]any{0.0, 0.0, 100.0, 1000.0}, []any{-5.0, 0.0, 1000.0, 1000.0}, []any{1.0, 2.0}, "all"} {
		if l := LookFrom(map[string]any{"crop": c}, base); l.Crop != base.Crop {
			t.Fatalf("crop %v used: %v", c, l.Crop)
		}
	}
}

// Composite: the food pixels from fg, the rest from bg (resized), a soft edge.
func TestComposite(t *testing.T) {
	fg := flat(100, 40, func(x, y int) color.RGBA { return color.RGBA{200, 0, 0, 255} })
	bg := flat(50, 20, func(x, y int) color.RGBA { return color.RGBA{0, 0, 200, 255} })
	alpha := make([]byte, 100*40)
	for y := 0; y < 40; y++ {
		for x := 0; x < 50; x++ {
			alpha[y*100+x] = 1
		}
	}
	out := Composite(fg, bg, alpha, 4)
	if c := out.RGBAAt(10, 20); c.R < 195 || c.B > 5 {
		t.Fatalf("food: %v", c)
	}
	if c := out.RGBAAt(90, 20); c.B < 195 || c.R > 5 {
		t.Fatalf("background: %v", c)
	}
	if c := out.RGBAAt(50, 20); c.R < 40 || c.B < 40 {
		t.Fatalf("edge not soft: %v", c)
	}
	// a JPEG (YCbCr) source gives the same result as its RGBA copy
	b, err := JPEG(fg, 90)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := dec.(*image.YCbCr); !ok {
		t.Skip("not YCbCr")
	}
	e1 := Enhance(dec, AutoLook())
	rgba := image.NewRGBA(dec.Bounds())
	for y := 0; y < 40; y++ {
		for x := 0; x < 100; x++ {
			rgba.Set(x, y, dec.At(x, y))
		}
	}
	e2 := Enhance(rgba, AutoLook())
	if e1.Bounds() != e2.Bounds() || e1.RGBAAt(30, 30) != e2.RGBAAt(30, 30) {
		t.Fatalf("YCbCr path: %v %v", e1.RGBAAt(30, 30), e2.RGBAAt(30, 30))
	}
}
