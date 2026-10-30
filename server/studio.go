package analyze

// X48g (T9 2026-10-30, after the X48f page: "there is not much a difference
// between original, enhance, and smart"): a stronger code edit that keeps
// every real food pixel.
//
//	StudioLook  AutoLook + clarity (local contrast), more sharpness, more colour
//	Studio      the food (a SAM mask) over a new background made in code:
//	            "plain" = a soft light surface with a soft shadow, "blur" = the
//	            photo itself, strongly blurred (a shallow depth of field)
//	SSIM        how much two photos have the same structure (1 = the same), so
//	            a Muse Image "edit" that is a new picture can be refused in code
//
// Nothing here paints food: delivery apps allow a new background, relight,
// crop and sharpen, and ban new or larger food (docs/sources/X48g.md).

import (
	"image"
	"math"
)

// StudioLook is the look of the studio modes.
func StudioLook() Look {
	l := AutoLook()
	l.Levels, l.Sharpen, l.Saturation, l.Clarity = 1, 1.0, 1.15, 0.35
	return l
}

// lowpass is a Gaussian blur of a plane for a large sigma: it shrinks the
// plane by f (box mean), blurs it there, and enlarges it again (bilinear).
func lowpass(src []float32, w, h int, sigma float64) []float32 {
	f := int(sigma / 2)
	if f < 2 {
		return blur(src, w, h, sigma)
	}
	sw, sh := max(1, w/f), max(1, h/f)
	sm := make([]float32, sw*sh)
	rows(sh, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < sw; x++ {
				var s float32
				n := 0
				for yy := y * f; yy < min(h, (y+1)*f); yy++ {
					for xx := x * f; xx < min(w, (x+1)*f); xx++ {
						s += src[yy*w+xx]
						n++
					}
				}
				if n > 0 {
					sm[y*sw+x] = s / float32(n)
				}
			}
		}
	})
	sm = blur(sm, sw, sh, sigma/float64(f))
	out := make([]float32, w*h)
	rows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			fy := math.Max(0, (float64(y)+0.5)/float64(f)-0.5)
			y0s := min(sh-1, int(fy))
			y1s := min(sh-1, y0s+1)
			ty := float32(fy - float64(y0s))
			for x := 0; x < w; x++ {
				fx := math.Max(0, (float64(x)+0.5)/float64(f)-0.5)
				x0s := min(sw-1, int(fx))
				x1s := min(sw-1, x0s+1)
				tx := float32(fx - float64(x0s))
				a := sm[y0s*sw+x0s]*(1-tx) + sm[y0s*sw+x1s]*tx
				b := sm[y1s*sw+x0s]*(1-tx) + sm[y1s*sw+x1s]*tx
				out[y*w+x] = a*(1-ty) + b*ty
			}
		}
	})
	return out
}

// clarity raises the local contrast: an unsharp mask with a large radius
// (1/60 of the long edge).
func (p *planes) clarity(amount float64) {
	if amount <= 0 {
		return
	}
	sigma := math.Max(4, float64(max(p.w, p.h))/60)
	a := float32(amount)
	for k := 0; k < 3; k++ {
		c := p.c[k]
		b := lowpass(c, p.w, p.h, sigma)
		rows(p.h, func(y0, y1 int) {
			for i := y0 * p.w; i < y1*p.w; i++ {
				c[i] += a * (c[i] - b[i])
			}
		})
	}
}

// MaskBounds is the box of the set pixels of a w x h raster (empty when none).
func MaskBounds(alpha []byte, w, h int) image.Rectangle {
	x0, y0, x1, y1 := w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if alpha[y*w+x] != 0 {
				x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
			}
		}
	}
	if x1 < 0 {
		return image.Rectangle{}
	}
	return image.Rect(x0, y0, x1+1, y1+1)
}

// GridCrop is r (in a w x h image) with a margin (a share of its size) on the
// 0-1000 grid of Look.Crop.
func GridCrop(r image.Rectangle, w, h int, margin float64) [4]int {
	mx, my := int(margin*float64(r.Dx())), int(margin*float64(r.Dy()))
	r = image.Rect(r.Min.X-mx, r.Min.Y-my, r.Max.X+mx, r.Max.Y+my).Intersect(image.Rect(0, 0, w, h))
	return [4]int{r.Min.X * 1000 / w, r.Min.Y * 1000 / h, (r.Max.X*1000 + w - 1) / w, (r.Max.Y*1000 + h - 1) / h}
}

// MaskTo is the part crop (on the 0-1000 grid, CropRect rules) of a w x h
// raster, scaled to ow x oh (nearest pixel), then grown by grow px.
func MaskTo(alpha []byte, w, h int, crop [4]int, ow, oh, grow int) []byte {
	r := CropRect(image.Rect(0, 0, w, h), crop)
	out := make([]byte, ow*oh)
	for y := 0; y < oh; y++ {
		sy := r.Min.Y + y*r.Dy()/oh
		for x := 0; x < ow; x++ {
			sx := r.Min.X + x*r.Dx()/ow
			out[y*ow+x] = alpha[sy*w+sx]
		}
	}
	if grow <= 0 {
		return out
	}
	f := make([]float32, len(out))
	for i, v := range out {
		f[i] = float32(v)
	}
	f = blur(f, ow, oh, float64(grow)/2)
	for i, v := range f {
		if v > 0.08 {
			out[i] = 1
		}
	}
	return out
}

// Studio puts fg (its alpha = the food) over a background made from fg:
// style "plain" (a light surface, lighter at the top, and a soft shadow under
// the food) or "blur" (fg strongly blurred and a little darker).
func Studio(fg *image.RGBA, alpha []byte, style string) *image.RGBA {
	w, h := fg.Bounds().Dx(), fg.Bounds().Dy()
	long := float64(max(w, h))
	bg := &planes{w: w, h: h}
	if style == "blur" {
		p := toPlanes(fg, fg.Bounds())
		for k := 0; k < 3; k++ {
			b := lowpass(p.c[k], w, h, long/25)
			for i := range b {
				b[i] *= 0.92
			}
			bg.c[k] = b
		}
	} else {
		top, bottom := [3]float32{0.965, 0.955, 0.94}, [3]float32{0.88, 0.865, 0.85}
		shadow := make([]float32, w*h)
		dy := int(long / 60)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if sy := y - dy; sy >= 0 && alpha[sy*w+x] != 0 {
					shadow[y*w+x] = 1
				}
			}
		}
		shadow = lowpass(shadow, w, h, long/40)
		for k := 0; k < 3; k++ {
			bg.c[k] = make([]float32, w*h)
			for y := 0; y < h; y++ {
				t := float32(y) / float32(max(1, h-1))
				v := top[k]*(1-t) + bottom[k]*t
				for x := 0; x < w; x++ {
					bg.c[k][y*w+x] = v * (1 - 0.35*shadow[y*w+x])
				}
			}
		}
	}
	return Composite(fg, bg.image(), alpha, max(2, int(long/300)))
}

// SSIM is the structural similarity of a and b on their luminance, both
// scaled to a's shape with the long edge 256 px, in 8 x 8 blocks (Wang et
// al. 2004; C1 = (0.01)^2, C2 = (0.03)^2 on 0..1). 1 = the same picture.
func SSIM(a, b image.Image) float64 {
	ab := a.Bounds()
	scale := 256 / float64(max(ab.Dx(), ab.Dy()))
	w, h := max(8, int(float64(ab.Dx())*scale)), max(8, int(float64(ab.Dy())*scale))
	gray := func(img image.Image) []float32 {
		p := toPlanes(img, img.Bounds()).resize(w, h)
		g := make([]float32, w*h)
		for i := range g {
			g[i] = luma(p.c[0][i], p.c[1][i], p.c[2][i])
		}
		return g
	}
	x, y := gray(a), gray(b)
	const c1, c2 = 0.0001, 0.0009
	sum, n := 0.0, 0
	for by := 0; by+8 <= h; by += 8 {
		for bx := 0; bx+8 <= w; bx += 8 {
			var mx, my float64
			for j := 0; j < 8; j++ {
				for i := 0; i < 8; i++ {
					k := (by+j)*w + bx + i
					mx += float64(x[k])
					my += float64(y[k])
				}
			}
			mx, my = mx/64, my/64
			var vx, vy, cov float64
			for j := 0; j < 8; j++ {
				for i := 0; i < 8; i++ {
					k := (by+j)*w + bx + i
					dx, dy := float64(x[k])-mx, float64(y[k])-my
					vx, vy, cov = vx+dx*dx, vy+dy*dy, cov+dx*dy
				}
			}
			vx, vy, cov = vx/63, vy/63, cov/63
			sum += ((2*mx*my + c1) * (2*cov + c2)) / ((mx*mx + my*my + c1) * (vx + vy + c2))
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// Resize is img with its long edge set to long px (Catmull-Rom), with no
// limit on the factor: X48h makes a small source big before Muse Image,
// which then draws the detail (Enhance keeps its 2x limit for code edits).
func Resize(img image.Image, long int) *image.RGBA {
	b := img.Bounds()
	scale := float64(long) / float64(max(b.Dx(), b.Dy()))
	w, h := max(1, int(math.Round(float64(b.Dx())*scale))), max(1, int(math.Round(float64(b.Dy())*scale)))
	return toPlanes(img, b).resize(w, h).image()
}
