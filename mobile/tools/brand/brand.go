// Package main makes the 7 app icon files of V3RDE from the logo tokens (LG1, written new for V3RDE in the
// Hackathon; T9 Nov 1: the mark T1 "V³", the icon approved in the logo mockup
// https://claude.ai/artifact/1B1TJxdtLPX6h1Qig13z1u). The geometry and the colours are read from
// mobile/src/theme.ts (the logo group and the palette), so the app and the icons have one source.
//
// An app icon: a full-bleed accent square (the system cuts the corners), the mark at logo.iconShare of the side,
// centred on its box (no optical shift, T9), face onAccent, depth1 light fg, depth2 dark accentSoft. The Android
// adaptive foreground has the mark box logo.adaptiveMark of the layer (48 of 108 dp: LG2, T9 Nov 2, Android's 48 dp);
// the monochrome layer is the outline of the three V's together, in one colour (T9 Nov 2: "merged stack").
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/image/vector"
)

// Logo is the logo group of theme.ts and the four palette colours an icon uses.
type Logo struct {
	ViewW, ViewH   float64
	V              [][2]float64 // one V
	Depth1, Depth2 float64
	IconShare      float64
	AdaptiveMark   float64
	Accent         color.RGBA // palette.light.accent: the icon background
	Face           color.RGBA // palette.light.onAccent
	Deep1          color.RGBA // palette.light.fg
	Deep2          color.RGBA // palette.dark.accentSoft
}

var (
	logoBlock = regexp.MustCompile(`(?s)export const logo = \{(.*?)\n\} as const;`)
	entry     = regexp.MustCompile(`(?m)^\s*(\w+):\s*("([^"]*)"|[^,\n]+?),?\s*(//.*)?$`)
)

// number reads a token value: a number or "a / b".
func number(s string) (float64, error) {
	if a, b, ok := strings.Cut(s, "/"); ok {
		x, err1 := strconv.ParseFloat(strings.TrimSpace(a), 64)
		y, err2 := strconv.ParseFloat(strings.TrimSpace(b), 64)
		if err1 != nil || err2 != nil || y == 0 {
			return 0, fmt.Errorf("bad ratio %q", s)
		}
		return x / y, nil
	}
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

func hexColour(s string) (color.RGBA, error) {
	if len(s) != 7 || s[0] != '#' {
		return color.RGBA{}, fmt.Errorf("not #RRGGBB: %q", s)
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.RGBA{}, err
	}
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, nil
}

// paletteColour reads palette.<mode>.<key> of theme.ts.
func paletteColour(theme, mode, key string) (color.RGBA, error) {
	m := regexp.MustCompile(`(?s)\n  ` + mode + `: \{(.*?)\n  \}`).FindStringSubmatch(theme)
	if m == nil {
		return color.RGBA{}, fmt.Errorf("palette.%s not found", mode)
	}
	c := regexp.MustCompile(`\b` + key + `: "(#[0-9A-Fa-f]{6})"`).FindStringSubmatch(m[1])
	if c == nil {
		return color.RGBA{}, fmt.Errorf("palette.%s.%s not found", mode, key)
	}
	return hexColour(c[1])
}

// ReadLogo reads the logo tokens and the icon colours from the text of theme.ts.
func ReadLogo(theme string) (Logo, error) {
	var l Logo
	b := logoBlock.FindStringSubmatch(theme)
	if b == nil {
		return l, fmt.Errorf("theme.ts has no logo group")
	}
	t := map[string]string{}
	for _, m := range entry.FindAllStringSubmatch(b[1], -1) {
		if m[3] != "" || strings.HasPrefix(m[2], `"`) {
			t[m[1]] = m[3]
		} else {
			t[m[1]] = strings.TrimSpace(m[2])
		}
	}
	nums := map[string]*float64{"viewW": &l.ViewW, "viewH": &l.ViewH, "depth1": &l.Depth1, "depth2": &l.Depth2,
		"iconShare": &l.IconShare, "adaptiveMark": &l.AdaptiveMark}
	for k, p := range nums {
		v, ok := t[k]
		if !ok {
			return l, fmt.Errorf("logo.%s not found", k)
		}
		x, err := number(v)
		if err != nil {
			return l, fmt.Errorf("logo.%s: %v", k, err)
		}
		*p = x
	}
	for _, pt := range strings.Fields(t["v"]) {
		a, b, ok := strings.Cut(pt, ",")
		x, err1 := strconv.ParseFloat(a, 64)
		y, err2 := strconv.ParseFloat(b, 64)
		if !ok || err1 != nil || err2 != nil {
			return l, fmt.Errorf("logo.v: bad point %q", pt)
		}
		l.V = append(l.V, [2]float64{x, y})
	}
	if len(l.V) < 3 {
		return l, fmt.Errorf("logo.v has %d points", len(l.V))
	}
	var err error
	for _, c := range []struct {
		dst       *color.RGBA
		mode, key string
	}{{&l.Accent, "light", "accent"}, {&l.Face, "light", "onAccent"}, {&l.Deep1, "light", "fg"}, {&l.Deep2, "dark", "accentSoft"}} {
		if *c.dst, err = paletteColour(theme, c.mode, c.key); err != nil {
			return l, err
		}
	}
	return l, nil
}

// polygon fills one V, moved by d path units, onto dst: the mark is h px high, centred on a side x side box.
func (l Logo) polygon(dst draw.Image, side int, h, d float64, c color.Color) {
	k := h / l.ViewH
	ox, oy := (float64(side)-l.ViewW*k)/2, (float64(side)-l.ViewH*k)/2
	z := vector.NewRasterizer(side, side)
	for i, p := range l.V {
		x, y := float32(ox+(p[0]+d)*k), float32(oy+(p[1]+d)*k)
		if i == 0 {
			z.MoveTo(x, y)
		} else {
			z.LineTo(x, y)
		}
	}
	z.ClosePath()
	z.Draw(dst, dst.Bounds(), image.NewUniform(c), image.Point{})
}

// mark draws the three V's back to front; one colour for all three when mono is set.
func (l Logo) mark(dst draw.Image, side int, h float64, mono *color.RGBA) {
	cols := []color.RGBA{l.Deep2, l.Deep1, l.Face}
	if mono != nil {
		cols = []color.RGBA{*mono, *mono, *mono}
	}
	l.polygon(dst, side, h, l.Depth2, cols[0])
	l.polygon(dst, side, h, l.Depth1, cols[1])
	l.polygon(dst, side, h, 0, cols[2])
}

// Icon is a full-bleed app icon of side px (opaque: written as RGB).
func (l Logo) Icon(side int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(img, img.Bounds(), image.NewUniform(l.Accent), image.Point{}, draw.Src)
	l.mark(img, side, l.IconShare*float64(side), nil)
	return img
}

// Background is the Android adaptive background layer: the accent only.
func (l Logo) Background(side int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(img, img.Bounds(), image.NewUniform(l.Accent), image.Point{}, draw.Src)
	return img
}

// Foreground is the Android adaptive foreground layer: the mark on transparent, its box adaptiveMark of the layer.
func (l Logo) Foreground(side int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	l.mark(img, side, l.AdaptiveMark*float64(side), nil)
	return img
}

// Monochrome is the Android themed-icon layer: the outline of the three V's together, in the face colour.
func (l Logo) Monochrome(side int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	l.mark(img, side, l.AdaptiveMark*float64(side), &l.Face)
	return img
}

// Files are the 7 icon files (path under mobile/ -> PNG bytes). splash-icon.png is the same file as icon.png.
func (l Logo) Files() (map[string][]byte, error) {
	enc := func(img image.Image) ([]byte, error) {
		var b bytes.Buffer
		err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&b, img)
		return b.Bytes(), err
	}
	icon, err := enc(l.Icon(1024))
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{"assets/icon.png": icon, "assets/splash-icon.png": icon}
	for path, img := range map[string]image.Image{
		"assets/favicon.png":                 l.Icon(48),
		"public/apple-touch-icon.png":        l.Icon(180),
		"assets/android-icon-background.png": l.Background(1024),
		"assets/android-icon-foreground.png": l.Foreground(1024),
		"assets/android-icon-monochrome.png": l.Monochrome(1024),
	} {
		if out[path], err = enc(img); err != nil {
			return nil, err
		}
	}
	return out, nil
}
