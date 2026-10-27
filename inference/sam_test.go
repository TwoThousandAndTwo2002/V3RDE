package inference

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

type goldens struct {
	Masks []struct {
		W, H          int
		Kind, Payload string
		RasterSHA256  string `json:"raster_sha256"`
		Raster        *string
	}
	Lines map[string]struct {
		Text    string
		Records []map[string]any
		Errors  []string
	}
}

func loadGoldens(t *testing.T) goldens {
	b, err := os.ReadFile("testdata/sam_goldens.json")
	if err != nil {
		t.Fatal(err)
	}
	var g goldens
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// Goldens: meta-sam-parser 0.1.0's own encoder and decoder
// (tools/hackathon/gen_sam_goldens.py). Bit-exact rasters, and the Go
// encoder reproduces Meta's payload byte for byte.
func TestOneBitMatchesMetaReference(t *testing.T) {
	g := loadGoldens(t)
	if len(g.Masks) < 8 {
		t.Fatal(len(g.Masks))
	}
	for _, m := range g.Masks {
		r, err := DecodeOneBit(m.Payload, m.W, m.H)
		if err != nil {
			t.Fatalf("%dx%d %s: %v", m.W, m.H, m.Kind, err)
		}
		s := sha256.Sum256(r)
		if hex.EncodeToString(s[:]) != m.RasterSHA256 {
			t.Fatalf("%dx%d %s: raster differs", m.W, m.H, m.Kind)
		}
		if m.Raster != nil {
			for i, c := range *m.Raster {
				if byte(c-'0') != r[i] {
					t.Fatalf("%dx%d pixel %d", m.W, m.H, i)
				}
			}
		}
		if p, err := encodeOneBit(r, m.W, m.H); err != nil || p != m.Payload {
			t.Fatalf("%dx%d %s: Go encoder differs from Meta's", m.W, m.H, m.Kind)
		}
	}
}

func TestOneBitRefusesBadPayloads(t *testing.T) {
	g := loadGoldens(t)
	p := g.Masks[3].Payload
	for _, bad := range []string{"", "~" + p[1:], p[:len(p)-1], p + "0", "!zzzz", p[:8] + "|" + p[9:]} {
		if _, err := DecodeOneBit(bad, g.Masks[3].W, g.Masks[3].H); err == nil {
			t.Fatalf("accepted %q", bad[:min(len(bad), 12)])
		}
	}
	if _, err := DecodeOneBit(p, g.Masks[3].W+1, g.Masks[3].H); err == nil {
		t.Fatal("accepted a payload with the wrong size")
	}
}

func TestParseSAMMatchesMetaParser(t *testing.T) {
	g := loadGoldens(t)
	three := g.Lines["three"]
	got, skipped := ParseSAM(three.Text)
	if skipped != 0 || len(got) != 3 {
		t.Fatalf("%d instances, %d skipped", len(got), skipped)
	}
	i := 0
	for _, r := range three.Records {
		if _, isBox := r["left"]; !isBox {
			continue
		}
		in := got[i]
		c := -1.0
		if r["c"] != nil {
			c = r["c"].(float64)
		}
		if in.ID != r["id"] || float64(in.Left) != r["left"] || float64(in.Top) != r["top"] ||
			float64(in.Right) != r["right"] || float64(in.Bottom) != r["bottom"] || in.Conf != c || !in.MaskOK {
			t.Fatalf("record %d: %+v vs %v", i, in, r)
		}
		i++
	}
	if none, sk := ParseSAM(g.Lines["none"].Text); len(none) != 0 || sk != 0 {
		t.Fatal("an empty frame is zero instances", none, sk)
	}
	bad, _ := ParseSAM(g.Lines["bad_mask"].Text)
	if len(bad) != 1 || bad[0].MaskOK || bad[0].Right != 6 {
		t.Fatalf("bad mask keeps its box: %+v", bad)
	}
}

func TestParseSAMDropsBrokenRecords(t *testing.T) {
	for _, s := range []string{
		"<0f>0<|box;x1=1;y1=1;x2=5;y2=5;w=10;h=10|>",                                 // no mask
		"<0f>0<|box;x1=1;y1=1;x2=5;w=10;h=10|><|mask;x=0;y=0;data=1,1,!00001|>",      // no y2
		"<0f>0<|box;x1=5;y1=1;x2=1;y2=5;w=10;h=10|><|mask;x=0;y=0;data=1,1,!00001|>", // empty box
		"<0f>0<|box;x1=1;y1=1;x2=5;y2=5;w=10;h=10|><|mask;x=2;y=0;data=1,1,!00001|>", // not box-local
		"garbage",
	} {
		if got, sk := ParseSAM(s); len(got) != 0 || sk == 0 {
			t.Fatalf("%q: %v %d", s, got, sk)
		}
	}
}
