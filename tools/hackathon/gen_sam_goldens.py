#!/usr/bin/env python3
"""Goldens for the Go SAM parser + one_bit mask decoder (inference/sam*.go).

Uses Meta's own reference package, meta-sam-parser 0.1.0 (SAM License): its
one_bit ENCODER makes the payloads, its PARSER reads the lines. The Go port
must give the same boxes, confidences and rasters, and refuse what it refuses.

  pip install meta-sam-parser==0.1.0
  python3 tools/hackathon/gen_sam_goldens.py > inference/testdata/sam_goldens.json
"""
import hashlib, json, random
from meta_sam_parser import image_segmentation_format, decode_mask_to_raster, CompletedOutcome, SegmentationMask
from meta_sam_parser._mask_codec import _encode_raster

rng = random.Random(13)


def blob(w, h, kind):
    if kind == "empty":
        return [0] * (w * h)
    if kind == "full":
        return [1] * (w * h)
    if kind == "noise":
        return [rng.randint(0, 1) for _ in range(w * h)]
    cx, cy, rx, ry = w / 2, h / 2, w / 2.6, h / 2.3        # an ellipse, like a real item
    return [1 if ((x + .5 - cx) / rx) ** 2 + ((y + .5 - cy) / ry) ** 2 <= 1 else 0 for y in range(h) for x in range(w)]


masks = []
for w, h, kind in [(1, 1, "full"), (3, 2, "noise"), (17, 9, "blob"), (64, 48, "blob"),
                   (40, 40, "empty"), (33, 21, "full"), (101, 46, "blob"), (12, 30, "noise")]:
    r = blob(w, h, kind)
    p = _encode_raster(r, w, h)
    back = decode_mask_to_raster(SegmentationMask(encoding="one_bit", payload=p, width=w, height=h))
    assert list(back) == r
    masks.append({"w": w, "h": h, "kind": kind, "payload": p,
                  "raster_sha256": hashlib.sha256(bytes(r)).hexdigest(),
                  "raster": "".join(map(str, r)) if w * h <= 64 else None})

# lines exactly as the API writes them: <0f> then records "id<|box;..|><|mask;..|>", comma-separated
W, H = 800, 600
recs, boxes = [], [(353, 382, 453, 427, 0.91), (10, 20, 210, 260, 0.77), (500, 40, 780, 330, None)]
for i, (x1, y1, x2, y2, c) in enumerate(boxes):
    m = masks[[2, 3, 6][i]]
    cf = "" if c is None else ";c=%.2f" % c
    recs.append("%d<|box;x1=%d;y1=%d;x2=%d;y2=%d;w=%d;h=%d%s|><|mask;x=0;y=0;data=%d,%d,%s%s|>" % (
        i, x1, y1, x2, y2, W, H, cf, m["h"], m["w"], m["payload"], cf))
lines = {"three": "<0f>" + ",".join(recs) + "\n", "none": "<0f>\n",
         "bad_mask": "<0f>0<|box;x1=1;y1=1;x2=5;y2=5;w=10;h=10|><|mask;x=0;y=0;data=4,4,!zzzz|>\n"}
parsed = {}
for k, text in lines.items():
    p = image_segmentation_format().create_parser()
    p.push(text)
    res = p.finish(CompletedOutcome()).result
    out = []
    for r in res.records:
        if r.kind == "box":
            out.append({"id": r.object_id, "left": r.left, "top": r.top, "right": r.right, "bottom": r.bottom,
                        "c": r.confidence})
        elif r.kind == "mask":
            out.append({"id": r.object_id, "mask_w": r.mask.width, "mask_h": r.mask.height,
                        "bounds": [r.bounds.left, r.bounds.top, r.bounds.right, r.bounds.bottom]})
    parsed[k] = {"text": text, "records": out, "errors": [d.code for d in res.diagnostics if d.severity == "error"]}
print(json.dumps({"generator": "meta-sam-parser 0.1.0", "masks": masks, "lines": parsed}, indent=1))
