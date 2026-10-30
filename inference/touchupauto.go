package inference

// X48o (T9 2026-10-30: "Auto (the scene rule, X48m) it is lets lock this in
// the code"): the touch-up that the service runs. It is the "auto" column of
// the X48m page, without the other columns: the Muse Image bigger edit
// (checked: SSIM >= SSIMMin and the same shape), then the item's mask chosen
// by the scene rule (Auto: words, or the box when the words mask spills or
// finds nothing), then the studio blur. Only the masks that the rule needs
// are asked for.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// AutoResult is one touch-up.
type AutoResult struct {
	JPEG    []byte  `json:"-"`
	W, H    int     `json:"-"`
	Mask    string  `json:"mask"`     // which mask and why (Auto's reason)
	SSIM    float64 `json:"ssim"`     // the Muse edit's score (0: no edit)
	Muse    bool    `json:"muse"`     // the Muse edit was used (false: the code look on the photo)
	CostUSD float64 `json:"cost_usd"` // every Meta call of this touch-up
	Note    string  `json:"note"`     // what the edit started from
}

// TouchItem is what a touch-up starts from (LoadItem).
type TouchItem struct {
	Src      image.Image     // the item, cut again from the upload (or the item photo)
	Target   image.Rectangle // the item's box in Src (empty: not known)
	Concepts []string        // the scan's name of the item first, then SubjectConcepts
	Note     string
}

// LoadItem reads an item photo's record (<itemDir>/<id>.json) and cuts the
// item again from the upload in photosDir (ItemSource): with the item photo's
// margin, or a wider one when the item photo's long edge is under `under` px;
// the scan's name of the item comes from the analyze events in eventsDir
// ("" = none) of the record's day and the next day (ItemScanNear). When the record or the upload is not there, the item photo is
// used as it is (no box).
func LoadItem(photosDir, eventsDir, itemDir, id string, item []byte, under int, margin float64) TouchItem {
	it := TouchItem{Concepts: SubjectConcepts, Note: "the item photo"}
	if im, _, err := image.Decode(bytes.NewReader(item)); err == nil {
		it.Src = im
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(item))
	if err != nil || !PhotoIDPattern.MatchString(id) {
		return it
	}
	mb, err := os.ReadFile(filepath.Join(itemDir, id+".json"))
	var m ItemPhotoMeta
	if err != nil || json.Unmarshal(mb, &m) != nil || m.PhotoID != id {
		it.Note = "the item photo (no record)"
		return it
	}
	name := ""
	if eventsDir != "" {
		name, _ = ItemScanNear(eventsDir, m.RequestID, m.Item, m.TS)
	}
	if name != "" {
		it.Concepts = append([]string{strings.ToLower(name)}, SubjectConcepts...)
	}
	orig, err := os.ReadFile(filepath.Join(photosDir, filepath.Base(m.ImageSHA256)+".jpg"))
	if err != nil {
		it.Note = "the item photo (no upload)"
		return it
	}
	mg, how := CropMargin, "the item cut again from the upload"
	if max(cfg.Width, cfg.Height) < under {
		mg, how = margin, "the item cut again from the upload, wider"
	}
	src, box, _, err := ItemSource(orig, m, mg)
	if err != nil {
		it.Note = "the item photo (" + err.Error() + ")"
		return it
	}
	b := src.Bounds()
	it.Src, it.Target = src, box
	it.Note = fmt.Sprintf("%s: %dx%d, item box %v (item photo %dx%d)", how, b.Dx(), b.Dy(), box, cfg.Width, cfg.Height)
	if name != "" {
		it.Note += "; name from the scan: “" + name + "”"
	}
	return it
}

// Touchup makes the auto touch-up of it. An error only when there is no
// picture at all; a refused or failed Muse edit gives the code look on the
// photo (Muse false), as the X48m page did.
func (v *V2) Touchup(ctx context.Context, it TouchItem) (AutoResult, error) {
	var r AutoResult
	src := it.Src
	if src == nil {
		return r, errors.New("touchup: no picture")
	}
	r.Note = it.Note
	in := src
	if sb := src.Bounds(); max(sb.Dx(), sb.Dy()) < MuseInputEdge {
		in = analyze.Resize(src, MuseInputEdge)
	}
	inJPEG, err := analyze.JPEG(in, 92)
	if err != nil {
		return r, err
	}
	base, look := src, analyze.StudioLook()
	jb, ic, eerr := v.EditJSON(ctx, inJPEG, BiggerPrompt)
	r.CostUSD += ic.CostUSD
	if eerr == nil {
		if im, _, derr := image.Decode(bytes.NewReader(jb)); derr == nil {
			s := analyze.SSIM(src, im)
			sb, eb := src.Bounds(), im.Bounds()
			ra, rb := float64(sb.Dx())/float64(sb.Dy()), float64(eb.Dx())/float64(eb.Dy())
			r.SSIM = s
			if s >= SSIMMin && math.Abs(ra-rb) <= 0.03*ra {
				base, look, r.Muse = im, analyze.Look{Levels: 0.5, Saturation: 1.05, Sharpen: 0.3, LongEdge: 1600}, true
			}
		}
	}
	bb, sbd := base.Bounds(), src.Bounds()
	t := image.Rect(it.Target.Min.X*bb.Dx()/sbd.Dx(), it.Target.Min.Y*bb.Dy()/sbd.Dy(),
		it.Target.Max.X*bb.Dx()/sbd.Dx(), it.Target.Max.Y*bb.Dy()/sbd.Dy())
	// the words mask first; the box only when the rule needs it
	wm, wlabel, _, wcost, spill, werr := v.subjectMask(ctx, base, it.Concepts, ContainerConcepts, t)
	r.CostUSD += wcost
	mask, merr := wm, werr
	words, box := Variant{Concept: wlabel}, Variant{Concept: "box"}
	var berr error = errors.New("not asked")
	if !t.Empty() && (werr != nil || spill > SpillMax) {
		bm, _, bcost, err := v.BoxMask(ctx, base, t)
		r.CostUSD += bcost
		berr = err
		if err == nil {
			label, _, ccost := v.WithContainers(ctx, base, bm)
			r.CostUSD += ccost
			box.Concept = "box" + label
			mask, merr = bm, nil
		}
	}
	a := Auto(words, box, werr, berr, spill, !t.Empty())
	if strings.HasPrefix(a.Concept, "words") {
		mask, merr = wm, werr
	}
	r.Mask = a.Concept
	img, _ := blurStudio(base, look, mask, merr)
	if merr != nil {
		r.Mask += " (no new background)"
	}
	out, err := analyze.JPEG(img, 90)
	if err != nil {
		return r, err
	}
	r.JPEG, r.W, r.H = out, img.Bounds().Dx(), img.Bounds().Dy()
	if !r.Muse {
		r.Mask = "the Muse edit was refused or failed: the code look on the photo; " + r.Mask
	}
	return r, nil
}

// EncodeOneBit is the one_bit encoder, for the tests of other packages (a
// fake SAM answer); the service never encodes.
func EncodeOneBit(raster []byte, w, h int) (string, error) { return encodeOneBit(raster, w, h) }
