package analyze

// X48g: the studio modes and the structure check (studio.go).

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// A blob of food in the middle; the rest is a cluttered pattern.
func scene(w, h int) (*image.RGBA, []byte) {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	alpha := make([]byte, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := float64(x-w/2)/float64(w/4), float64(y-h/2)/float64(h/4)
			if dx*dx+dy*dy < 1 {
				m.SetRGBA(x, y, color.RGBA{200, uint8(80 + x%40), 30, 255})
				alpha[y*w+x] = 1
			} else {
				m.SetRGBA(x, y, color.RGBA{uint8((x * 7) % 256), uint8((y * 13) % 256), uint8((x * y) % 256), 255})
			}
		}
	}
	return m, alpha
}

// The food pixels stay; the clutter is gone (plain: light and smooth; blur:
// no fine pattern); the shadow is under the food, not above it.
func TestStudioKeepsFood(t *testing.T) {
	fg, alpha := scene(300, 200)
	for _, style := range []string{"plain", "blur"} {
		out := Studio(fg, alpha, style)
		if out.Bounds() != fg.Bounds() {
			t.Fatalf("%s: size %v", style, out.Bounds())
		}
		if a, b := fg.RGBAAt(150, 100), out.RGBAAt(150, 100); a != b {
			t.Errorf("%s: a food pixel changed: %v -> %v", style, a, b)
		}
		// the clutter: neighbours differ a lot in fg, little in out
		d0, d1 := 0, 0
		for x := 5; x < 60; x++ {
			a, b := fg.RGBAAt(x, 10), fg.RGBAAt(x+1, 10)
			c, d := out.RGBAAt(x, 10), out.RGBAAt(x+1, 10)
			d0 += abs(int(a.R)-int(b.R)) + abs(int(a.G)-int(b.G))
			d1 += abs(int(c.R)-int(d.R)) + abs(int(c.G)-int(d.G))
		}
		if d1*5 > d0 {
			t.Errorf("%s: the background is not smooth: %d vs %d", style, d1, d0)
		}
	}
	p := Studio(fg, alpha, "plain")
	above, below := p.RGBAAt(150, 45), p.RGBAAt(150, 155)
	if below.R >= above.R {
		t.Errorf("no shadow under the food: above %v below %v", above, below)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// SSIM: 1 for the same photo, high for a light edit, low for another picture.
func TestSSIM(t *testing.T) {
	a, _ := scene(300, 200)
	if s := SSIM(a, a); math.Abs(s-1) > 1e-6 {
		t.Fatalf("same: %f", s)
	}
	light := Enhance(a, Look{Exposure: 0.2, Saturation: 1.1, Sharpen: 0.5})
	other := flat(300, 200, func(x, y int) color.RGBA { return color.RGBA{uint8(y), uint8(x / 2), 90, 255} })
	sl, so := SSIM(a, light), SSIM(a, other)
	if sl < 0.7 || so > 0.3 || sl <= so {
		t.Fatalf("light edit %.3f, other picture %.3f", sl, so)
	}
	// another shape is scaled to a's shape first
	if s := SSIM(a, Enhance(a, Look{LongEdge: 600, Saturation: 1})); s < 0.9 {
		t.Fatalf("2x copy: %.3f", s)
	}
}

// Clarity raises the local contrast of a soft step; the mask helpers.
func TestClarityAndMasks(t *testing.T) {
	src := flat(200, 100, func(x, y int) color.RGBA {
		v := uint8(100 + 40/(1+math.Exp(-float64(x-100)/2)))
		return color.RGBA{v, v, v, 255}
	})
	out := Enhance(src, Look{Clarity: 0.8, Saturation: 1})
	if d0, d1 := int(src.RGBAAt(105, 50).R)-int(src.RGBAAt(95, 50).R), int(out.RGBAAt(105, 50).R)-int(out.RGBAAt(95, 50).R); d1 <= d0 {
		t.Fatalf("clarity: %d -> %d", d0, d1)
	}
	_, alpha := scene(300, 200)
	r := MaskBounds(alpha, 300, 200)
	if r.Min.X < 70 || r.Min.X > 80 || r.Max.X < 220 || r.Max.X > 230 || MaskBounds(make([]byte, 4), 2, 2) != (image.Rectangle{}) {
		t.Fatalf("bounds %v", r)
	}
	g := GridCrop(r, 300, 200, 0.1)
	if g[0] >= r.Min.X*1000/300 || g[2] <= r.Max.X*1000/300 {
		t.Fatalf("grid %v for %v", g, r)
	}
	m := MaskTo(alpha, 300, 200, g, 600, 400, 0)
	if m[200*600+300] != 1 || m[2*600+2] != 0 {
		t.Fatalf("MaskTo centre %d corner %d", m[200*600+300], m[2*600+2])
	}
	grown := MaskTo(alpha, 300, 200, g, 600, 400, 8)
	n0, n1 := 0, 0
	for i := range m {
		n0 += int(m[i])
		n1 += int(grown[i])
	}
	if n1 <= n0 {
		t.Fatalf("grow: %d -> %d", n0, n1)
	}
}
