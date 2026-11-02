package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func theme(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../src/theme.ts")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func logoOf(t *testing.T, src string) Logo {
	t.Helper()
	l, err := ReadLogo(src)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// The tool reads the approved geometry and colours from theme.ts (one source for the app and the icons).
func TestReadLogoFromTheme(t *testing.T) {
	l := logoOf(t, theme(t))
	if l.ViewW != 130 || l.ViewH != 118 || l.Depth1 != 8 || l.Depth2 != 16 || l.IconShare != 0.46 || math.Abs(l.AdaptiveMark-48.0/108) > 1e-9 {
		t.Fatalf("logo %+v", l)
	}
	want := [][2]float64{{10, 10}, {38, 10}, {60, 62}, {82, 10}, {110, 10}, {74, 90}, {46, 90}}
	if len(l.V) != len(want) {
		t.Fatalf("v %v", l.V)
	}
	for i := range want {
		if l.V[i] != want[i] {
			t.Fatalf("v %v", l.V)
		}
	}
	for name, c := range map[string]color.RGBA{"accent": l.Accent, "face": l.Face, "deep1": l.Deep1, "deep2": l.Deep2} {
		w := map[string]color.RGBA{"accent": {0xBC, 0x20, 0x2C, 255}, "face": {255, 255, 255, 255},
			"deep1": {0x23, 0x23, 0x23, 255}, "deep2": {0x4A, 0x1D, 0x22, 255}}[name]
		if c != w {
			t.Errorf("%s %v, want %v", name, c, w)
		}
	}
	// regression: a theme with no logo group, or a broken value, is an error (no silent default)
	if _, err := ReadLogo(strings.Replace(theme(t), "export const logo = {", "export const logoX = {", 1)); err == nil {
		t.Error("no logo group: no error")
	}
	if _, err := ReadLogo(strings.Replace(theme(t), "iconShare: 0.46", "iconShare: x", 1)); err == nil {
		t.Error("bad iconShare: no error")
	}
}

// ink is the box of the pixels that are not the background (an opaque icon) or not transparent (a layer).
func ink(img image.Image, bg color.RGBA, layer bool) image.Rectangle {
	var r image.Rectangle
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if (layer && c.A > 127) || (!layer && (absd(c.R, bg.R) > 60 || absd(c.G, bg.G) > 60 || absd(c.B, bg.B) > 60)) {
				r = r.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return r
}

func absd(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// The app icon (approved, T9 Nov 1): full bleed accent, the mark at 46 % of the side, centred on its box, face
// onAccent; opaque (RGB). The ink of the three V's spans path y 10..106 and x 10..126 of the 130 x 118 box.
func TestIconGeometryAndColours(t *testing.T) {
	l := logoOf(t, theme(t))
	for _, side := range []int{1024, 180, 48} {
		img := l.Icon(side)
		if !img.Opaque() {
			t.Fatalf("%d: not opaque", side)
		}
		for _, p := range []image.Point{{0, 0}, {side - 1, 0}, {0, side - 1}, {side - 1, side - 1}} {
			if c := img.RGBAAt(p.X, p.Y); c != l.Accent {
				t.Fatalf("%d: corner %v is %v, want the accent (full bleed)", side, p, c)
			}
		}
		h := 0.46 * float64(side)
		k := h / 118
		top, left := (float64(side)-h)/2+10*k, (float64(side)-130*k)/2+10*k
		r := ink(img, l.Accent, false)
		tol := math.Max(1, float64(side)/256)
		if math.Abs(float64(r.Min.Y)-top) > tol || math.Abs(float64(r.Max.Y)-(top+96*k)) > tol || math.Abs(float64(r.Min.X)-left) > tol ||
			math.Abs(float64(r.Max.X)-(left+116*k)) > tol {
			t.Fatalf("%d: ink %v, want x %.1f..%.1f y %.1f..%.1f", side, r, left, left+116*k, top, top+96*k)
		}
		// the face (onAccent) is in the upper middle of the V; depth2 (dark accentSoft) at its lower right
		face := img.RGBAAt(int(left+14*k), int(top+5*k))
		if face != l.Face && side >= 180 {
			t.Fatalf("%d: face %v", side, face)
		}
	}
}

// Android: the background is the accent; the foreground has the mark box 48 of 108 dp (LG2: Android's "at least
// 48x48 dp"; the box is 52.9 x 48 dp), inside the 66 dp safe circle; the monochrome layer is the merged outline in one colour.
func TestAdaptiveLayers(t *testing.T) {
	l := logoOf(t, theme(t))
	bg := l.Background(1024)
	for _, p := range []image.Point{{0, 0}, {512, 512}, {1023, 1023}} {
		if bg.RGBAAt(p.X, p.Y) != l.Accent {
			t.Fatal("background is not the accent")
		}
	}
	if l.AdaptiveMark*108 < 48 || l.AdaptiveMark*108*l.ViewW/l.ViewH > 66 {
		t.Fatalf("the adaptive mark box is %.1f dp high: Android wants at least 48 dp and at most 66 dp", l.AdaptiveMark*108)
	}
	safe := 66.0 / 108 * 512
	for name, img := range map[string]*image.NRGBA{"foreground": l.Foreground(1024), "monochrome": l.Monochrome(1024)} {
		b := img.Bounds()
		n := 0
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				c := img.NRGBAAt(x, y)
				if c.A == 0 {
					continue
				}
				n++
				if math.Hypot(float64(x)+0.5-512, float64(y)+0.5-512) > safe {
					t.Fatalf("%s: pixel (%d,%d) is outside the safe zone", name, x, y)
				}
				if name == "monochrome" && (c.R != 255 || c.G != 255 || c.B != 255) {
					t.Fatalf("monochrome pixel %v is not one colour", c)
				}
			}
		}
		h := 1024 * 48.0 / 108
		r := ink(img, color.RGBA{}, true)
		if math.Abs(float64(r.Dy())-96*h/118) > 2 {
			t.Fatalf("%s: ink height %d, want %.1f", name, r.Dy(), 96*h/118)
		}
		if n == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
	// the merged outline covers all three V's: the depth2 tip (lower right) has ink in the monochrome layer
	k := 1024 * 48.0 / 108 / 118
	ox, oy := (1024-130*k)/2, (1024-118*k)/2
	if c := l.Monochrome(1024).NRGBAAt(int(ox+(60+16)*k), int(oy+(88+16)*k)); c.A < 200 {
		t.Fatalf("the monochrome layer has no depth2 tip: %v", c)
	}
}

// The committed icon files are what the tool makes from theme.ts (pixels, within Tolerance), splash-icon.png is
// byte-equal to icon.png, and each file has its size and colour type (RGB for the opaque files).
func TestCommittedFilesMatchTheTool(t *testing.T) {
	l := logoOf(t, theme(t))
	files, err := l.Files()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		side  int
		alpha bool
	}{"assets/icon.png": {1024, false}, "assets/splash-icon.png": {1024, false}, "assets/favicon.png": {48, false},
		"public/apple-touch-icon.png": {180, false}, "assets/android-icon-background.png": {1024, false},
		"assets/android-icon-foreground.png": {1024, true}, "assets/android-icon-monochrome.png": {1024, true}}
	if len(files) != len(want) {
		t.Fatalf("%d files", len(files))
	}
	disk := map[string][]byte{}
	for n, w := range want {
		b, err := os.ReadFile(filepath.Join("../..", n))
		if err != nil {
			t.Fatal(err)
		}
		disk[n] = b
		cfg, err := png.DecodeConfig(bytes.NewReader(b))
		if err != nil || cfg.Width != w.side || cfg.Height != w.side {
			t.Fatalf("%s: %v %+v", n, err, cfg)
		}
		if ct := b[25]; (w.alpha && ct != 6) || (!w.alpha && ct != 2) { // IHDR colour type: 2 RGB, 6 RGBA
			t.Fatalf("%s: colour type %d", n, ct)
		}
		if d := MaxDiff(b, files[n]); d > Tolerance {
			t.Fatalf("%s differs from the tool (largest channel difference %d): run the tool", n, d)
		}
	}
	if !bytes.Equal(disk["assets/icon.png"], disk["assets/splash-icon.png"]) {
		t.Fatal("splash-icon.png is not icon.png")
	}
	// regression: another share in theme.ts makes another icon (the check would fail)
	l2 := logoOf(t, strings.Replace(theme(t), "iconShare: 0.46", "iconShare: 0.5", 1))
	f2, _ := l2.Files()
	if MaxDiff(disk["assets/icon.png"], f2["assets/icon.png"]) <= Tolerance {
		t.Fatal("a changed iconShare does not change the icon")
	}
}
