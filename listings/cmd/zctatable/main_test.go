package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func zipOf(t *testing.T, text string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, _ := zw.Create("2024_Gaz_zcta_national.txt")
	w.Write([]byte(text))
	zw.Close()
	return b.Bytes()
}

func TestTableReadsTheGazetteerColumns(t *testing.T) {
	var txt strings.Builder
	txt.WriteString("GEOID\tALAND\tAWATER\tALAND_SQMI\tAWATER_SQMI\tINTPTLAT\tINTPTLONG                 \n")
	for i := 0; i < 30000; i++ {
		fmt.Fprintf(&txt, "%05d\t1\t2\t3\t4\t40.5\t-74.25        \n", i)
	}
	out, err := Table(zipOf(t, txt.String()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "zip,lat,lng\n00000,40.5,-74.25\n00001,40.5,-74.25\n") {
		t.Fatalf("%.80s", out)
	}
	if _, err := Table(zipOf(t, "GEOID\tINTPTLAT\tINTPTLONG\n10001\t40.7\t-74\n")); err == nil || !strings.Contains(err.Error(), "only 1 zips") {
		t.Fatalf("a short file: %v", err)
	}
	if _, err := Table(zipOf(t, "GEOID\tLAT\n")); err == nil || !strings.Contains(err.Error(), "INTPTLAT") {
		t.Fatalf("no column: %v", err)
	}
	if _, err := Table(zipOf(t, "GEOID\tINTPTLAT\tINTPTLONG\n1000\t40\t-74\n")); err == nil || !strings.Contains(err.Error(), "bad line") {
		t.Fatalf("bad zip: %v", err)
	}
}
