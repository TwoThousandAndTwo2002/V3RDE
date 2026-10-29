package gate

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif" // registers GIF for image.Decode
	_ "image/jpeg"
	_ "image/png"
)

// The geometry and the resamples a head can declare.
const (
	GeometryCropFirst = "crop_first_centre_square"
	ResampleBilinear  = "pillow_bilinear"
	ResampleBicubic   = "pillow_bicubic"
)

// PreprocessSpec is the "preprocess" block of a head (entry 3).
type PreprocessSpec struct {
	Geometry string     `json:"geometry"`
	Resample string     `json:"resample"`
	Mean     [3]float32 `json:"mean"`
	Std      [3]float32 `json:"std"`
	Layout   string     `json:"layout"`
}

// Validate refuses a preprocess this gate cannot do exactly.
func (p PreprocessSpec) Validate() error {
	switch {
	case p.Geometry != GeometryCropFirst:
		return fmt.Errorf("preprocess geometry %q is not %q", p.Geometry, GeometryCropFirst)
	case p.Resample != ResampleBilinear && p.Resample != ResampleBicubic:
		return fmt.Errorf("preprocess resample %q is not supported", p.Resample)
	case p.Layout != "" && p.Layout != "NHWC":
		return fmt.Errorf("preprocess layout %q is not NHWC", p.Layout)
	}
	for c, s := range p.Std {
		if s == 0 {
			return fmt.Errorf("preprocess std[%d] is 0", c)
		}
	}
	return nil
}

// CropBox is the centred square of side min(w, h); its origin uses integer
// division, as the phone's preprocess does.
func CropBox(w, h int) image.Rectangle {
	side := min(w, h)
	x, y := (w-side)/2, (h-side)/2
	return image.Rect(x, y, x+side, y+side)
}

// Decode reads a JPEG, PNG or GIF into packed RGB (alpha dropped, as
// Pillow's convert("RGB") does for opaque images). With the turbojpeg tag a
// JPEG goes through libjpeg-turbo (decode_turbo.go), as in Pillow.
func Decode(b []byte) (rgb []uint8, w, h int, format string, err error) {
	if isJPEG(b) {
		rgb, w, h, err = decodeJPEG(b)
		return rgb, w, h, "jpeg", err
	}
	img, format, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, 0, 0, "", fmt.Errorf("decode: %w", err)
	}
	r := img.Bounds()
	w, h = r.Dx(), r.Dy()
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(rgba, rgba.Bounds(), img, r.Min, draw.Src)
	rgb = make([]uint8, 0, w*h*3)
	for i := 0; i < len(rgba.Pix); i += 4 {
		rgb = append(rgb, rgba.Pix[i], rgba.Pix[i+1], rgba.Pix[i+2])
	}
	return rgb, w, h, format, nil
}

// Crop copies the rectangle r out of packed RGB of width w.
func Crop(rgb []uint8, w int, r image.Rectangle) []uint8 {
	row := r.Dx() * 3
	out := make([]uint8, 0, row*r.Dy())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		start := (y*w + r.Min.X) * 3
		out = append(out, rgb[start:start+row]...)
	}
	return out
}

// The resize is Pillow's 8-bit resample (libImaging/Resample.c): a
// separable filter whose support grows with the down-scale factor, integer
// weights with 22 fraction bits, a horizontal pass and then a vertical pass,
// each rounded and clipped to 0..255 (read in the Pillow 12.3.0 source:
// PRECISION_BITS (32 - 8 - 2), bicubic a = -0.5, and the filter argument
// (x + xmin - center + 0.5) * inv_filterscale). The goldens in the tests are
// Pillow 12.3.0's own output.
const fracBits = 32 - 8 - 2

// filter is one resample kernel and its support at scale 1.
type filter struct {
	f       func(x float64) float64
	support float64
}

var (
	// bilinear is Pillow's triangle filter.
	bilinear = filter{support: 1, f: func(x float64) float64 {
		if x < 0 {
			x = -x
		}
		if x < 1 {
			return 1 - x
		}
		return 0
	}}
	// bicubic is Pillow's cubic filter with a = -0.5.
	bicubic = filter{support: 2, f: func(x float64) float64 {
		const a = -0.5
		if x < 0 {
			x = -x
		}
		switch {
		case x < 1:
			return ((a+2)*x-(a+3))*x*x + 1
		case x < 2:
			return (((x-5)*x+8)*x - 4) * a
		}
		return 0
	}}
)

// weights is, for each output pixel, the first input pixel and the integer
// weights of the input pixels it reads.
func weights(in, out int, flt filter) (first []int, k [][]int32) {
	scale := float64(in) / float64(out)
	filterScale := max(scale, 1)
	support := flt.support * filterScale
	inv := 1.0 / filterScale // multiply by 1/scale, as Pillow does (x/s and x*(1/s) can differ in the last bit)
	first = make([]int, out)
	k = make([][]int32, out)
	for o := range out {
		center := (float64(o) + 0.5) * scale
		lo := max(int(center-support+0.5), 0)
		hi := min(int(center+support+0.5), in)
		w := make([]float64, hi-lo)
		total := 0.0
		for i := range w {
			w[i] = flt.f((float64(i+lo) - center + 0.5) * inv)
			total += w[i]
		}
		ki := make([]int32, len(w))
		for i, v := range w {
			if total != 0 {
				v /= total
			}
			if v < 0 {
				ki[i] = int32(v*(1<<fracBits) - 0.5)
			} else {
				ki[i] = int32(v*(1<<fracBits) + 0.5)
			}
		}
		first[o], k[o] = lo, ki
	}
	return first, k
}

// clip8 is the rounded fixed-point sum as one byte.
func clip8(v int64) uint8 {
	v >>= fracBits
	return uint8(min(max(v, 0), 255))
}

// resample resizes packed RGB w x h to ow x oh with flt (a pass is skipped
// when that side does not change, as in Pillow).
func resample(rgb []uint8, w, h, ow, oh int, flt filter) []uint8 {
	src, sw := rgb, w
	if ow != w {
		first, k := weights(w, ow, flt)
		tmp := make([]uint8, ow*h*3)
		for y := range h {
			for o := range ow {
				for c := range 3 {
					sum := int64(1) << (fracBits - 1)
					for i, wt := range k[o] {
						sum += int64(src[(y*sw+first[o]+i)*3+c]) * int64(wt)
					}
					tmp[(y*ow+o)*3+c] = clip8(sum)
				}
			}
		}
		src, sw = tmp, ow
	}
	if oh != h {
		first, k := weights(h, oh, flt)
		dst := make([]uint8, sw*oh*3)
		for o := range oh {
			for x := range sw {
				for c := range 3 {
					sum := int64(1) << (fracBits - 1)
					for i, wt := range k[o] {
						sum += int64(src[((first[o]+i)*sw+x)*3+c]) * int64(wt)
					}
					dst[(o*sw+x)*3+c] = clip8(sum)
				}
			}
		}
		src = dst
	}
	return src
}

// ResizeBilinear resizes packed RGB like Pillow's BILINEAR.
func ResizeBilinear(rgb []uint8, w, h, ow, oh int) []uint8 {
	return resample(rgb, w, h, ow, oh, bilinear)
}

// ResizeBicubic resizes packed RGB like Pillow's BICUBIC.
func ResizeBicubic(rgb []uint8, w, h, ow, oh int) []uint8 {
	return resample(rgb, w, h, ow, oh, bicubic)
}

// Tensor is the first gate's input: the centred square FIRST, one bilinear
// resize to side x side, NHWC float32 in [0, 1].
func Tensor(rgb []uint8, w, h, side int) []float32 {
	box := CropBox(w, h)
	px := ResizeBilinear(Crop(rgb, w, box), box.Dx(), box.Dy(), side, side)
	x := make([]float32, len(px))
	for i, v := range px {
		x[i] = float32(v) / 255.0
	}
	return x
}

// TensorFor is the input for a head's preprocess: the centred square FIRST,
// one resize to side x side, NHWC float32, then (x/255 - mean)/std per
// channel, with the same float32 steps in the same order as the Python
// reference (tools/hackathon/pe_gate_deploy_head.py).
func TensorFor(rgb []uint8, w, h, side int, p PreprocessSpec) []float32 {
	box := CropBox(w, h)
	sq := Crop(rgb, w, box)
	var px []uint8
	if p.Resample == ResampleBicubic {
		px = ResizeBicubic(sq, box.Dx(), box.Dy(), side, side)
	} else {
		px = ResizeBilinear(sq, box.Dx(), box.Dy(), side, side)
	}
	x := make([]float32, len(px))
	for i, v := range px {
		c := i % 3
		x[i] = (float32(v)/255.0 - p.Mean[c]) / p.Std[c]
	}
	return x
}
