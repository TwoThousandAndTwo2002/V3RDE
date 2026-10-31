// Command zctatable makes listings/zcta.csv (X49e) from the US Census 2024
// Gazetteer ZCTA file: one line "zip,lat,lng" per ZCTA, from GEOID,
// INTPTLAT and INTPTLONG (the internal point of the area). The zip must have
// the pinned sha256, so the table is always made from the same file.
//
//	go run ./cmd/zctatable -zip 2024_Gaz_zcta_national.zip -out zcta.csv
//
// Source: https://www2.census.gov/geo/docs/maps-data/data/gazetteer/2024_Gazetteer/2024_Gaz_zcta_national.zip
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Sum is the sha256 of the 2024 Gazetteer ZCTA zip (1,013,803 bytes), read on
// 2026-10-31.
const Sum = "7b85c04a131672f58b38a950eb82855d5fd4054f0bd9bc3f69aa28897a714e3d"

var zipRe = regexp.MustCompile(`^\d{5}$`)

func main() {
	in := flag.String("zip", "2024_Gaz_zcta_national.zip", "the Census Gazetteer ZCTA zip")
	out := flag.String("out", "zcta.csv", "the table to write")
	flag.Parse()
	b, err := os.ReadFile(*in)
	if err != nil {
		log.Fatal(err)
	}
	if s := sha256.Sum256(b); hex.EncodeToString(s[:]) != Sum {
		log.Fatalf("%s: sha256 %x, want %s", *in, s, Sum)
	}
	t, err := Table(b)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, t, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: %d zips\n", *out, bytes.Count(t, []byte("\n"))-1)
}

// Table reads the .txt in the zip and returns the CSV.
func Table(zipBytes []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".txt") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return fromText(rc)
	}
	return nil, fmt.Errorf("no .txt in the zip")
}

func fromText(r io.Reader) ([]byte, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	var out bytes.Buffer
	out.WriteString("zip,lat,lng\n")
	col := map[string]int{}
	n := 0
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		if len(col) == 0 {
			for i, name := range f {
				col[name] = i
			}
			for _, need := range []string{"GEOID", "INTPTLAT", "INTPTLONG"} {
				if _, ok := col[need]; !ok {
					return nil, fmt.Errorf("no %s column", need)
				}
			}
			continue
		}
		z, la, lo := f[col["GEOID"]], f[col["INTPTLAT"]], f[col["INTPTLONG"]]
		a, err1 := strconv.ParseFloat(la, 64)
		b, err2 := strconv.ParseFloat(lo, 64)
		if !zipRe.MatchString(z) || err1 != nil || err2 != nil || a < -90 || a > 90 || b < -180 || b > 180 {
			return nil, fmt.Errorf("bad line %q", sc.Text())
		}
		fmt.Fprintf(&out, "%s,%s,%s\n", z, strconv.FormatFloat(a, 'f', -1, 64), strconv.FormatFloat(b, 'f', -1, 64))
		n++
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if n < 30000 {
		return nil, fmt.Errorf("only %d zips", n)
	}
	return out.Bytes(), nil
}
