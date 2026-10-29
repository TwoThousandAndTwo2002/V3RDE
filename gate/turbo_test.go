//go:build turbojpeg

package gate

import (
	"encoding/base64"
	"testing"
)

func TestJPEGDecodeIsPillows(t *testing.T) {
	for _, g := range jpegGoldens {
		b, err := base64.StdEncoding.DecodeString(g.b64)
		if err != nil {
			t.Fatal(err)
		}
		rgb, w, h, format, err := Decode(b)
		if err != nil || format != "jpeg" || w != g.w || h != g.h {
			t.Fatalf("%s: %dx%d %s %v", g.name, w, h, format, err)
		}
		if got := sumOf(rgb); got != g.sha {
			t.Errorf("%s: decode %s, Pillow %s", g.name, got[:12], g.sha[:12])
		}
	}
}

func TestJPEGGarbageIsAnError(t *testing.T) {
	for _, b := range [][]byte{{0xFF, 0xD8, 0xFF, 0x00, 0x01}, append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, 64)...)} {
		if _, _, _, _, err := Decode(b); err == nil {
			t.Errorf("%x: no error", b[:5])
		}
	}
}
