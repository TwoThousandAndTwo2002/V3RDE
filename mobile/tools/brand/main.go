package main

// Usage (from mobile/tools/brand, its own Go module): go run . -theme ../../src/theme.ts -out ../..
// It writes the 7 icon files of Files() under -out; -check compares them with the files there (pixels) and writes
// nothing.

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
)

func main() {
	theme := flag.String("theme", "src/theme.ts", "the theme file with the logo tokens")
	out := flag.String("out", ".", "the mobile/ folder")
	check := flag.Bool("check", false, "compare with the files on disk; write nothing")
	flag.Parse()
	src, err := os.ReadFile(*theme)
	if err != nil {
		fail(err)
	}
	l, err := ReadLogo(string(src))
	if err != nil {
		fail(err)
	}
	files, err := l.Files()
	if err != nil {
		fail(err)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	bad := 0
	for _, n := range names {
		p := filepath.Join(*out, n)
		if *check {
			old, err := os.ReadFile(p)
			if err != nil {
				fail(err)
			}
			if d := MaxDiff(old, files[n]); d > Tolerance {
				fmt.Printf("DIFFERS %s (largest channel difference %d)\n", n, d)
				bad++
			} else {
				fmt.Printf("same    %s\n", n)
			}
			continue
		}
		if err := os.WriteFile(p, files[n], 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("wrote   %s (%d bytes)\n", n, len(files[n]))
	}
	if bad > 0 {
		os.Exit(1)
	}
}

// Tolerance is the largest channel difference two renders may have: the rasteriser accumulates in float32 and
// has an assembly path on some CPUs, so a pixel of an edge can differ by one step.
const Tolerance = 2

// MaxDiff is the largest channel difference of two PNG files (255 when they do not decode or differ in size).
func MaxDiff(a, b []byte) int {
	x, err1 := png.Decode(bytes.NewReader(a))
	y, err2 := png.Decode(bytes.NewReader(b))
	if err1 != nil || err2 != nil || x.Bounds() != y.Bounds() {
		return 255
	}
	return pixelDiff(x, y)
}

func pixelDiff(x, y image.Image) int {
	worst := 0
	r := x.Bounds()
	for j := r.Min.Y; j < r.Max.Y; j++ {
		for i := r.Min.X; i < r.Max.X; i++ {
			a1, a2, a3, a4 := x.At(i, j).RGBA()
			b1, b2, b3, b4 := y.At(i, j).RGBA()
			for k, v := range [4][2]uint32{{a1, b1}, {a2, b2}, {a3, b3}, {a4, b4}} {
				_ = k
				d := int(v[0]>>8) - int(v[1]>>8)
				if d < 0 {
					d = -d
				}
				if d > worst {
					worst = d
				}
			}
		}
	}
	return worst
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "brand:", err)
	os.Exit(1)
}
