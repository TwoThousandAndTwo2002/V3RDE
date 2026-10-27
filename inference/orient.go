package inference

// EXIF orientation (X30b). Phone cameras keep the sensor's pixel order and
// write the turn needed for display into the EXIF Orientation tag (0x0112,
// values 1-8, the TIFF/Exif definition): Apple's CGImagePropertyOrientation
// "simply defines symbolic names for those values"
// (developer.apple.com/documentation/imageio/cgimagepropertyorientation);
// Android writes the same tag (ExifInterface.TAG_ORIENTATION). Go's
// image.Decode ignores it (golang.org/issue/4341). Measured 2026-10-26: 11
// of 20 photos from an Android phone had orientation 6 or 3.
//
// The engine turns every photo upright first, so SAM's boxes, Muse's marks
// and the size levels share one frame. The transforms follow the table at
// jpegclub.org/exif_orientation.html.

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
)

// Orientation reads the EXIF Orientation of a JPEG (1 when absent or not a
// JPEG). Only the first APP1 "Exif" segment and IFD0 are read.
func Orientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		if marker == 0xDA || marker == 0xD9 { // start of scan / end: no more headers
			return 1
		}
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if n < 2 || i+2+n > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+n]
		if marker == 0xE1 && len(seg) > 14 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return tiffOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	count := int(bo.Uint16(t[off:]))
	for k := 0; k < count; k++ {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 { // Orientation, SHORT
			v := int(bo.Uint16(t[e+8:]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// Upright applies an EXIF orientation: the result is the image as it is
// meant to be seen. Orientation 1 returns src unchanged.
func Upright(src image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	in := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(in, in.Bounds(), src, b.Min, draw.Src)
	dw, dh := w, h
	if o >= 5 { // 5-8 swap width and height
		dw, dh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	// for each display pixel (x, y), the stored pixel it comes from
	from := func(x, y int) (int, int) {
		switch o {
		case 2:
			return w - 1 - x, y
		case 3:
			return w - 1 - x, h - 1 - y
		case 4:
			return x, h - 1 - y
		case 5:
			return y, x
		case 6:
			return y, h - 1 - x
		case 7:
			return w - 1 - y, h - 1 - x
		default: // 8
			return w - 1 - y, x
		}
	}
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			sx, sy := from(x, y)
			si, di := in.PixOffset(sx, sy), out.PixOffset(x, y)
			copy(out.Pix[di:di+4], in.Pix[si:si+4])
		}
	}
	return out
}
