//go:build turbojpeg

package gate

// A JPEG goes through libjpeg-turbo's TurboJPEG API: the library Pillow
// decodes with (libjpeg defaults: the accurate integer IDCT and fancy chroma
// upsampling, RGB out; flags 0 = no TJFLAG_FASTDCT, no TJFLAG_FASTUPSAMPLE).
// Go's image/jpeg gives a few levels per pixel more or less, which moved p by
// up to 0.044 in the first gate's parity run.

/*
#cgo LDFLAGS: -lturbojpeg
#include <stdlib.h>
#include <turbojpeg.h>
*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"unsafe"
)

// JPEGDecoder names the JPEG path in /healthz.
const JPEGDecoder = "libjpeg-turbo"

// maxPixels is the largest JPEG decoded (50 megapixels; X51: the same MaxPixels as the other formats).
const maxPixels = MaxPixels

func isJPEG(b []byte) bool { return len(b) > 3 && bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}) }

func decodeJPEG(b []byte) (rgb []uint8, w, h int, err error) {
	dec := C.tjInitDecompress()
	if dec == nil {
		return nil, 0, 0, errors.New("tjInitDecompress failed")
	}
	defer C.tjDestroy(dec)
	src := C.CBytes(b) // a C copy: TurboJPEG may keep the pointer during the call
	defer C.free(src)
	fail := func() error { return fmt.Errorf("decode: %s", C.GoString(C.tjGetErrorStr2(dec))) }
	var cw, ch, sub, cs C.int
	if C.tjDecompressHeader3(dec, (*C.uchar)(src), C.ulong(len(b)), &cw, &ch, &sub, &cs) != 0 {
		return nil, 0, 0, fail()
	}
	w, h = int(cw), int(ch)
	if w <= 0 || h <= 0 || w*h > maxPixels {
		return nil, 0, 0, fmt.Errorf("decode: bad size %dx%d", w, h)
	}
	dst := C.malloc(C.size_t(w * h * 3))
	defer C.free(dst)
	if C.tjDecompress2(dec, (*C.uchar)(src), C.ulong(len(b)), (*C.uchar)(dst), cw, 0, ch, C.TJPF_RGB, 0) != 0 {
		return nil, 0, 0, fail()
	}
	return C.GoBytes(unsafe.Pointer(dst), C.int(w*h*3)), w, h, nil
}
