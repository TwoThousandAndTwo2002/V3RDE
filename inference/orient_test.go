package inference

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
)

// withOrientation puts an APP1 Exif segment with Orientation = o right after
// the JPEG's SOI marker, in the byte order given ("II" or "MM").
func withOrientation(t *testing.T, jpg []byte, o int, order string) []byte {
	var bo binary.ByteOrder = binary.LittleEndian
	if order == "MM" {
		bo = binary.BigEndian
	}
	tiff := make([]byte, 8+2+12+4)
	copy(tiff, order)
	bo.PutUint16(tiff[2:], 42)
	bo.PutUint32(tiff[4:], 8)
	bo.PutUint16(tiff[8:], 1)          // 1 entry
	bo.PutUint16(tiff[10:], 0x0112)    // Orientation
	bo.PutUint16(tiff[12:], 3)         // SHORT
	bo.PutUint32(tiff[14:], 1)         // count
	bo.PutUint16(tiff[18:], uint16(o)) // value
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(seg)+2))
	out := append([]byte{}, jpg[:2]...)
	out = append(out, app1...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

// marker4 is a 4 x 2 image with one red pixel at stored (0, 0).
func marker4(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestOrientationIsReadInBothByteOrders(t *testing.T) {
	base := marker4(t)
	if Orientation(base) != 1 || Orientation([]byte("not a jpeg")) != 1 {
		t.Fatal("no Exif must read as 1")
	}
	for o := 1; o <= 8; o++ {
		for _, bo := range []string{"II", "MM"} {
			if got := Orientation(withOrientation(t, base, o, bo)); got != o {
				t.Fatalf("%s %d: read %d", bo, o, got)
			}
		}
	}
}

// Where the stored top-left corner lands after Upright, per the Exif table
// (jpegclub.org/exif_orientation.html), on a 40 x 20 stored image.
func TestUprightMovesTheCornerAsTheExifTableSays(t *testing.T) {
	src, _, err := image.Decode(bytes.NewReader(marker4(t)))
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]struct{ w, h, x, y int }{
		1: {40, 20, 2, 2}, 2: {40, 20, 37, 2}, 3: {40, 20, 37, 17}, 4: {40, 20, 2, 17},
		5: {20, 40, 2, 2}, 6: {20, 40, 17, 2}, 7: {20, 40, 17, 37}, 8: {20, 40, 2, 37},
	}
	for o, w := range want {
		u := Upright(src, o)
		b := u.Bounds()
		r, g, _, _ := u.At(w.x, w.y).RGBA()
		if b.Dx() != w.w || b.Dy() != w.h || r>>8 < 200 || g>>8 > 80 {
			t.Fatalf("orientation %d: %dx%d, pixel (%d,%d) = %d,%d", o, b.Dx(), b.Dy(), w.x, w.y, r>>8, g>>8)
		}
	}
}

// A sideways phone photo: SAM gets upright pixels (a new JPEG) and Muse's
// multi-item image is upright.
func TestSidewaysPhotoIsTurnedUprightForSAMAndMuse(t *testing.T) {
	f := &fakeAPI{sam: func(string) (int, string) {
		return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{300, 0, 499, 299})
	}, muse: func(string, map[string]any) map[string]any {
		return textResp("m", itemsText(ent(1, nil, "Soup"), ent(2, nil, "Beans")))
	}}
	e, done := engine(t, f)
	defer done()
	photo := withOrientation(t, testPhoto(t), 6, "MM") // stored 800 x 600, shown 600 x 800
	inv, err := e.Run(context.Background(), photo, "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	var sizes []string
	for _, b := range f.bodies {
		for _, c := range b["input"].([]any)[0].(map[string]any)["content"].([]any) {
			if cm := c.(map[string]any); cm["type"] == "input_image" {
				cfg := decodeDataURL(t, cm["image_url"].(string))
				sizes = append(sizes, fmt.Sprintf("%v:%dx%d", b["model"], cfg.Width, cfg.Height))
			}
		}
	}
	if inv.Orientation != 6 || inv.Width != 600 || inv.Height != 800 || inv.PrepW != 600 {
		t.Fatalf("orientation %d, %dx%d, prep %d", inv.Orientation, inv.Width, inv.Height, inv.PrepW)
	}
	if strings.Join(sizes, ",") != "sam-3.1:600x800,muse-spark-1.3:600x800" {
		t.Fatal(sizes)
	}
}

// One item on a sideways photo: analyze's call gets the original bytes,
// unchanged (R1); SAM gets the upright copy.
func TestSidewaysOneItemKeepsTheOriginalBytesForAnalyze(t *testing.T) {
	var museImg string
	f := &fakeAPI{sam: func(string) (int, string) { return 200, samLine(t, [4]int{100, 100, 399, 499}) },
		muse: func(_ string, body map[string]any) map[string]any {
			for _, c := range body["input"].([]any)[0].(map[string]any)["content"].([]any) {
				if cm := c.(map[string]any); cm["type"] == "input_image" {
					museImg = cm["image_url"].(string)
				}
			}
			b, _ := json.Marshal(contract("Soup"))
			return textResp("m", string(b))
		}}
	e, done := engine(t, f)
	defer done()
	photo := withOrientation(t, testPhoto(t), 3, "II")
	if _, err := e.Run(context.Background(), photo, "image/jpeg", ModeAuto); err != nil {
		t.Fatal(err)
	}
	if museImg != ImagePart(photo, "image/jpeg")["image_url"] {
		t.Fatal("the one-item call must send the original bytes")
	}
}
