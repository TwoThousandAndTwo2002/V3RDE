package analyze

// X48f (touch-up v2, T9 2026-10-29: "isolate the item and enhance the
// sharpness ... make it bigger"): a photo edit in code. Every step changes
// only the light, the colour balance, the sharpness, the framing and the size
// of the real pixels; nothing is painted. The same steps run with fixed
// settings (AutoLook, "enhance") or with settings that Muse Spark chose for
// the photo (LookPrompt + LookFrom, "smart"). Composite puts the real food
// pixels (a SAM mask) over a background from another image ("smart+bg").
//
// The steps, in order: crop (0-1000 grid), resize (Catmull-Rom; at most 2x
// up), auto levels (luminance percentiles), white balance (grey world on the
// mid-tones) and warmth, exposure (in linear light), saturation, unsharp mask.
// Each step runs over the rows in parallel (one goroutine per CPU).

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"runtime"
	"sort"
	"sync"
)

// Look is the settings of one edit.
type Look struct {
	Crop         [4]int  `json:"crop"`          // [left, top, right, bottom] on the 0-1000 grid; all 0 = the whole photo
	Exposure     float64 `json:"exposure"`      // stops, -1 .. 1
	Warmth       float64 `json:"warmth"`        // -0.3 .. 0.3 (+ = warmer)
	Saturation   float64 `json:"saturation"`    // 0.8 .. 1.4 (1 = no change)
	Sharpen      float64 `json:"sharpen"`       // 0 .. 2 (unsharp mask amount)
	Levels       float64 `json:"levels"`        // 0 .. 1 (auto levels strength)
	WhiteBalance float64 `json:"white_balance"` // 0 .. 1 (grey world strength)
	LongEdge     int     `json:"long_edge"`     // the long edge of the result in px (at most 2x the crop)
}

// AutoLook is the fixed look of the code-only mode.
func AutoLook() Look {
	return Look{Exposure: 0, Warmth: 0.03, Saturation: 1.1, Sharpen: 0.8, Levels: 0.8, WhiteBalance: 0.5, LongEdge: 1600}
}

// LookPrompt asks Muse Spark for the settings of one food photo. Only
// numbers come back; the code does the edit, so nothing can be invented.
const LookPrompt = `This is a photo of food offered on a food-sharing app. Choose the settings of a photo edit that makes the food look clear and appealing, without changing the food. Return ONLY valid JSON:
{"crop": [left, top, right, bottom] (the food with a small margin; a 0-1000 grid where (0, 0) is the top-left corner and (1000, 1000) the bottom-right; [0, 0, 1000, 1000] keeps the whole photo),
"exposure": -0.5 to 0.5 (stops; + is brighter),
"warmth": -0.2 to 0.2 (+ is warmer),
"saturation": 0.9 to 1.25 (1 is no change),
"sharpen": 0 to 1.5,
"levels": 0 to 1 (how much to stretch the contrast),
"white_balance": 0 to 1 (how much to remove a colour cast)}
Keep every value small unless the photo clearly needs it.`

// LookFrom reads the model's answer over base: each number is clamped to its
// range; a missing or wrong value keeps base. A crop smaller than 20% of a
// side, or not inside the grid, keeps base.Crop.
func LookFrom(obj map[string]any, base Look) Look {
	l := base
	num := func(k string, lo, hi float64, dst *float64) {
		if v, ok := obj[k].(float64); ok && !math.IsNaN(v) && !math.IsInf(v, 0) {
			*dst = math.Max(lo, math.Min(hi, v))
		}
	}
	num("exposure", -0.5, 0.5, &l.Exposure)
	num("warmth", -0.2, 0.2, &l.Warmth)
	num("saturation", 0.9, 1.25, &l.Saturation)
	num("sharpen", 0, 1.5, &l.Sharpen)
	num("levels", 0, 1, &l.Levels)
	num("white_balance", 0, 1, &l.WhiteBalance)
	if c, ok := obj["crop"].([]any); ok && len(c) == 4 {
		var b [4]int
		good := true
		for i, v := range c {
			f, isNum := v.(float64)
			if !isNum || f < 0 || f > 1000 {
				good = false
				break
			}
			b[i] = int(math.Round(f))
		}
		if good && b[2]-b[0] >= 200 && b[3]-b[1] >= 200 {
			l.Crop = b
		}
	}
	return l
}

// planes is an image as three float rows-major planes (sRGB, 0..1).
type planes struct {
	w, h int
	c    [3][]float32
}

// rows runs f over [0, h) in parallel chunks.
func rows(h int, f func(y0, y1 int)) {
	n := runtime.NumCPU()
	if n > h {
		n = h
	}
	if n <= 1 {
		f(0, h)
		return
	}
	var wg sync.WaitGroup
	step := (h + n - 1) / n
	for y0 := 0; y0 < h; y0 += step {
		y1 := min(h, y0+step)
		wg.Add(1)
		go func() { defer wg.Done(); f(y0, y1) }()
	}
	wg.Wait()
}

func toPlanes(img image.Image, r image.Rectangle) *planes {
	p := &planes{w: r.Dx(), h: r.Dy()}
	for k := range p.c {
		p.c[k] = make([]float32, p.w*p.h)
	}
	if yc, ok := img.(*image.YCbCr); ok { // a decoded JPEG: no interface call per pixel
		rows(p.h, func(y0, y1 int) {
			for y := y0; y < y1; y++ {
				for x := 0; x < p.w; x++ {
					yi, ci := yc.YOffset(r.Min.X+x, r.Min.Y+y), yc.COffset(r.Min.X+x, r.Min.Y+y)
					cr, cg, cb := color.YCbCrToRGB(yc.Y[yi], yc.Cb[ci], yc.Cr[ci])
					i := y*p.w + x
					p.c[0][i], p.c[1][i], p.c[2][i] = float32(cr)/255, float32(cg)/255, float32(cb)/255
				}
			}
		})
		return p
	}
	rows(p.h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < p.w; x++ {
				cr, cg, cb, _ := img.At(r.Min.X+x, r.Min.Y+y).RGBA()
				i := y*p.w + x
				p.c[0][i], p.c[1][i], p.c[2][i] = float32(cr)/65535, float32(cg)/65535, float32(cb)/65535
			}
		}
	})
	return p
}

func (p *planes) image() *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, p.w, p.h))
	q := func(v float32) uint8 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint8(v*255 + 0.5)
	}
	rows(p.h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < p.w; x++ {
				i := y*p.w + x
				out.SetRGBA(x, y, color.RGBA{q(p.c[0][i]), q(p.c[1][i]), q(p.c[2][i]), 255})
			}
		}
	})
	return out
}

// catmullRom is the cubic kernel (B=0, C=0.5).
func catmullRom(x float64) float64 {
	x = math.Abs(x)
	switch {
	case x < 1:
		return 1.5*x*x*x - 2.5*x*x + 1
	case x < 2:
		return -0.5*x*x*x + 2.5*x*x - 4*x + 2
	}
	return 0
}

// weights are the taps of one output position along one axis.
type taps struct {
	first int
	w     []float32
}

func kernelTaps(in, out int) []taps {
	scale := float64(out) / float64(in)
	support := 2.0
	stretch := 1.0
	if scale < 1 { // down: widen the kernel (no aliasing)
		stretch = 1 / scale
	}
	t := make([]taps, out)
	for o := range t {
		centre := (float64(o)+0.5)/scale - 0.5
		lo := int(math.Floor(centre - support*stretch))
		hi := int(math.Ceil(centre + support*stretch))
		var ws []float32
		sum := 0.0
		for i := lo; i <= hi; i++ {
			v := catmullRom((float64(i) - centre) / stretch)
			ws = append(ws, float32(v))
			sum += v
		}
		for i := range ws {
			ws[i] = float32(float64(ws[i]) / sum)
		}
		t[o] = taps{first: lo, w: ws}
	}
	return t
}

func clampIdx(i, n int) int {
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

// resize is a separable Catmull-Rom resample to w x h.
func (p *planes) resize(w, h int) *planes {
	if w == p.w && h == p.h {
		return p
	}
	tx, ty := kernelTaps(p.w, w), kernelTaps(p.h, h)
	out := &planes{w: w, h: h}
	for k := 0; k < 3; k++ { // one channel at a time: one middle plane is alive, not three (memory, X48f review)
		out.c[k] = make([]float32, w*h)
		src, m, o := p.c[k], make([]float32, w*p.h), out.c[k]
		rows(p.h, func(y0, y1 int) {
			for y := y0; y < y1; y++ {
				for x := 0; x < w; x++ {
					var s float32
					for j, wt := range tx[x].w {
						s += wt * src[y*p.w+clampIdx(tx[x].first+j, p.w)]
					}
					m[y*w+x] = s
				}
			}
		})
		rows(h, func(y0, y1 int) {
			for y := y0; y < y1; y++ {
				for x := 0; x < w; x++ {
					var s float32
					for j, wt := range ty[y].w {
						s += wt * m[clampIdx(ty[y].first+j, p.h)*w+x]
					}
					o[y*w+x] = s
				}
			}
		})
		p.c[k] = nil // the source channel is done
	}
	return out
}

func luma(r, g, b float32) float32 { return 0.299*r + 0.587*g + 0.114*b }

// levels stretches the luminance from its 0.5% / 99.5% points to 0..1, by strength.
func (p *planes) levels(strength float64) {
	if strength <= 0 {
		return
	}
	n := p.w * p.h
	step := max(1, n/20000) // a sample is enough for the percentiles
	var ys []float64
	for i := 0; i < n; i += step {
		ys = append(ys, float64(luma(p.c[0][i], p.c[1][i], p.c[2][i])))
	}
	sort.Float64s(ys)
	lo, hi := ys[len(ys)*5/1000], ys[len(ys)-1-len(ys)*5/1000]
	if hi-lo < 0.05 {
		return
	}
	lo, hi = lo*strength, 1-(1-hi)*strength
	a, b := float32(1/(hi-lo)), float32(-lo/(hi-lo))
	for k := 0; k < 3; k++ {
		c := p.c[k]
		rows(p.h, func(y0, y1 int) {
			for i := y0 * p.w; i < y1*p.w; i++ {
				c[i] = a*c[i] + b
			}
		})
	}
}

// balance: grey world on the mid-tones (by strength), then warmth.
func (p *planes) balance(strength, warmth float64) {
	var sum [3]float64
	cnt := 0
	n := p.w * p.h
	step := max(1, n/50000)
	for i := 0; i < n; i += step {
		y := luma(p.c[0][i], p.c[1][i], p.c[2][i])
		if y < 0.1 || y > 0.9 {
			continue
		}
		for k := 0; k < 3; k++ {
			sum[k] += float64(p.c[k][i])
		}
		cnt++
	}
	gain := [3]float64{1, 1, 1}
	if cnt > 100 && strength > 0 {
		grey := (sum[0] + sum[1] + sum[2]) / 3
		for k := 0; k < 3; k++ {
			g := 1 + strength*(grey/math.Max(sum[k], 1e-6)-1)
			gain[k] = math.Max(0.8, math.Min(1.25, g))
		}
	}
	gain[0] *= 1 + warmth*0.5
	gain[2] *= 1 - warmth*0.5
	for k := 0; k < 3; k++ {
		c, g := p.c[k], float32(gain[k])
		if g == 1 {
			continue
		}
		rows(p.h, func(y0, y1 int) {
			for i := y0 * p.w; i < y1*p.w; i++ {
				c[i] *= g
			}
		})
	}
}

// exposure multiplies the linear light by 2^stops, then sets the saturation.
func (p *planes) tone(stops, sat float64) {
	gain := math.Pow(2, stops)
	s := float32(sat)
	// the exposure as a table over 0..1 (4096 steps, linear between): no pow per pixel
	const n = 4096
	var lut [n + 2]float32
	for i := range lut {
		x := math.Min(1, float64(i)/n)
		lut[i] = float32(math.Pow(math.Pow(x, 2.2)*gain, 1/2.2))
	}
	expose := func(x float32) float32 {
		if x <= 0 {
			return x * float32(gain)
		}
		if x >= 1 {
			return x * lut[n]
		}
		f := x * n
		i := int(f)
		return lut[i] + (f-float32(i))*(lut[i+1]-lut[i])
	}
	rows(p.h, func(y0, y1 int) {
		for i := y0 * p.w; i < y1*p.w; i++ {
			var v [3]float32
			for k := 0; k < 3; k++ {
				x := p.c[k][i]
				if gain != 1 {
					x = expose(x)
				}
				v[k] = x
			}
			y := luma(v[0], v[1], v[2])
			for k := 0; k < 3; k++ {
				p.c[k][i] = y + s*(v[k]-y)
			}
		}
	})
}

// blur is a separable Gaussian of one plane.
func blur(src []float32, w, h int, sigma float64) []float32 {
	r := int(math.Ceil(sigma * 3))
	k := make([]float32, 2*r+1)
	sum := float32(0)
	for i := -r; i <= r; i++ {
		k[i+r] = float32(math.Exp(-float64(i*i) / (2 * sigma * sigma)))
		sum += k[i+r]
	}
	for i := range k {
		k[i] /= sum
	}
	mid, out := make([]float32, w*h), make([]float32, w*h)
	rows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				var s float32
				for j := -r; j <= r; j++ {
					s += k[j+r] * src[y*w+clampIdx(x+j, w)]
				}
				mid[y*w+x] = s
			}
		}
	})
	rows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				var s float32
				for j := -r; j <= r; j++ {
					s += k[j+r] * mid[clampIdx(y+j, h)*w+x]
				}
				out[y*w+x] = s
			}
		}
	})
	return out
}

// sharpen is an unsharp mask (radius from the size of the image).
func (p *planes) sharpen(amount float64) {
	if amount <= 0 {
		return
	}
	sigma := math.Max(0.8, float64(max(p.w, p.h))/1000)
	a := float32(amount)
	for k := 0; k < 3; k++ {
		c := p.c[k]
		b := blur(c, p.w, p.h, sigma)
		rows(p.h, func(y0, y1 int) {
			for i := y0 * p.w; i < y1*p.w; i++ {
				c[i] += a * (c[i] - b[i])
			}
		})
	}
}

// CropRect is l.Crop on img's bounds (the whole image when the crop is unset).
func CropRect(b image.Rectangle, crop [4]int) image.Rectangle {
	if crop == [4]int{} {
		return b
	}
	w, h := b.Dx(), b.Dy()
	r := image.Rect(b.Min.X+crop[0]*w/1000, b.Min.Y+crop[1]*h/1000, b.Min.X+crop[2]*w/1000, b.Min.Y+crop[3]*h/1000).Intersect(b)
	if r.Dx() < w/5 || r.Dy() < h/5 {
		return b
	}
	return r
}

// Enhance runs the look on img.
func Enhance(img image.Image, l Look) *image.RGBA {
	r := CropRect(img.Bounds(), l.Crop)
	p := toPlanes(img, r)
	if l.LongEdge > 0 {
		long := max(p.w, p.h)
		scale := math.Min(2, float64(l.LongEdge)/float64(long))
		w, h := max(1, int(math.Round(float64(p.w)*scale))), max(1, int(math.Round(float64(p.h)*scale)))
		p = p.resize(w, h)
	}
	p.levels(l.Levels)
	p.balance(l.WhiteBalance, l.Warmth)
	p.tone(l.Exposure, l.Saturation)
	p.sharpen(l.Sharpen)
	return p.image()
}

// Composite puts fg over bg where alpha is set: alpha is a w*h raster of fg
// (0 or 1, 1 = the food), feathered by a box blur of radius feather px; bg is
// resized to fg's size.
func Composite(fg *image.RGBA, bg image.Image, alpha []byte, feather int) *image.RGBA {
	w, h := fg.Bounds().Dx(), fg.Bounds().Dy()
	bp := toPlanes(bg, bg.Bounds()).resize(w, h)
	a := make([]float32, w*h)
	for i, v := range alpha {
		if i < len(a) && v != 0 {
			a[i] = 1
		}
	}
	if feather > 0 {
		a = blur(a, w, h, float64(feather)/2)
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	rows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				i := y*w + x
				f := fg.RGBAAt(fg.Bounds().Min.X+x, fg.Bounds().Min.Y+y)
				t := a[i]
				mix := func(fv uint8, bv float32) uint8 {
					v := t*float32(fv)/255 + (1-t)*bv
					return uint8(math.Max(0, math.Min(255, float64(v*255+0.5))))
				}
				out.SetRGBA(x, y, color.RGBA{mix(f.R, bp.c[0][i]), mix(f.G, bp.c[1][i]), mix(f.B, bp.c[2][i]), 255})
			}
		}
	})
	return out
}

// JPEG encodes img at quality q.
func JPEG(img image.Image, q int) ([]byte, error) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: q}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
