package inference

// SAM 3.1 output: one line per frame, "<Nf>" then records
// "id<|box;x1=..;y1=..;x2=..;y2=..;w=..;h=..[;c=..]|><|mask;x=0;y=0;data=H,W,!payload[;c=..]|>",
// optionally comma-separated (dev.meta.ai/docs/sam/reading-segmentation and
// the grammar of meta-sam-parser 0.1.0, READ 2026-10-26). x2/y2 are
// inclusive (right-1, bottom-1); Instance stores half-open bounds. The mask
// raster is box-local: it covers exactly [Left,Right) x [Top,Bottom).

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// Instance is one segmented object in source pixels.
type Instance struct {
	ID                       string  `json:"id"`
	Left, Top, Right, Bottom int     `json:"-"`
	W, H                     int     `json:"-"`    // source frame size
	Conf                     float64 `json:"conf"` // -1 when the API omitted c
	MaskW, MaskH             int     `json:"-"`
	Payload                  string  `json:"-"`
	MaskOK                   bool    `json:"mask_ok"`           // decoded and canonical
	Concept                  string  `json:"concept,omitempty"` // the SAM concept that found it
}

// Area is the box area in source pixels.
func (i Instance) Area() int { return (i.Right - i.Left) * (i.Bottom - i.Top) }

// Box is [left, top, right, bottom], half-open.
func (i Instance) Box() [4]int { return [4]int{i.Left, i.Top, i.Right, i.Bottom} }

var (
	headerRE = regexp.MustCompile(`^<([0-9]+)f(?:;[^>]*)?>(.*)$`)
	dataRE   = regexp.MustCompile(`^([0-9]+),([0-9]+),([!~][^|]+)$`)
	confRE   = regexp.MustCompile(`^-?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
)

// ParseSAM reads every record of an image response. A record without
// exactly one box and one mask, or with a bad box, is dropped and counted in
// skipped; a record whose mask payload does not decode keeps its box with
// MaskOK false (the reference parser does the same). An empty frame is zero
// instances, not an error.
func ParseSAM(text string) (out []Instance, skipped int) {
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		m := headerRE.FindStringSubmatch(line)
		if m == nil {
			skipped++
			continue
		}
		rest := m[2]
		for rest != "" {
			rest = strings.TrimPrefix(rest, ",")
			j := 0
			for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
				j++
			}
			if j == 0 || !strings.HasPrefix(rest[j:], "<|") {
				skipped++
				break
			}
			id := rest[:j]
			rest = rest[j:]
			var toks [][2]string
			for strings.HasPrefix(rest, "<|") {
				end := strings.Index(rest, "|>")
				if end < 0 {
					rest = ""
					break
				}
				body := rest[2:end]
				name, fields, _ := strings.Cut(body, ";")
				toks = append(toks, [2]string{name, fields})
				rest = rest[end+2:]
			}
			inst, err := record(id, toks)
			if err != nil {
				skipped++
				continue
			}
			out = append(out, inst)
		}
	}
	return out, skipped
}

func fields(s string) map[string]string {
	f := map[string]string{}
	for _, p := range strings.Split(s, ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(p), "=")
		if k = strings.TrimSpace(k); k != "" {
			f[k] = strings.TrimSpace(v)
		}
	}
	return f
}

func conf(f map[string]string) float64 {
	v, ok := f["c"]
	if !ok || !confRE.MatchString(v) {
		return -1
	}
	c, err := strconv.ParseFloat(v, 64)
	if err != nil || c < 0 || c > 1 {
		return -1
	}
	return c
}

func record(id string, toks [][2]string) (Instance, error) {
	var box, mask []string
	for _, t := range toks {
		switch t[0] {
		case "box":
			box = append(box, t[1])
		case "mask":
			mask = append(mask, t[1])
		}
	}
	if len(box) != 1 || len(mask) != 1 {
		return Instance{}, errors.New("sam: record needs one box and one mask")
	}
	bf, mf := fields(box[0]), fields(mask[0])
	n := map[string]int{}
	for _, k := range []string{"x1", "y1", "x2", "y2", "w", "h"} {
		v, err := strconv.Atoi(bf[k])
		if err != nil {
			return Instance{}, errors.New("sam: bad box field " + k)
		}
		n[k] = v
	}
	if mf["x"] != "0" || mf["y"] != "0" {
		return Instance{}, errors.New("sam: mask is not box-local")
	}
	d := dataRE.FindStringSubmatch(mf["data"])
	if d == nil {
		return Instance{}, errors.New("sam: bad mask data")
	}
	mh, _ := strconv.Atoi(d[1])
	mw, _ := strconv.Atoi(d[2])
	inst := Instance{ID: id, Left: n["x1"], Top: n["y1"], Right: n["x2"] + 1, Bottom: n["y2"] + 1,
		W: n["w"], H: n["h"], Conf: conf(bf), MaskW: mw, MaskH: mh, Payload: d[3]}
	if inst.Right <= inst.Left || inst.Bottom <= inst.Top || inst.W <= 0 || inst.H <= 0 {
		return Instance{}, errors.New("sam: empty box")
	}
	if _, err := DecodeOneBit(inst.Payload, mw, mh); err == nil {
		inst.MaskOK = true
	}
	return inst, nil
}
