#!/usr/bin/env python3
"""Computes accuracy from per-box hand labels. Every accuracy number in the
docs comes from this script's output (T9, 2026-10-27: no number is reported
before its labels are recorded and counted).

  python3 tools/hackathon/score_labels.py <labels.json>

Formats:
  x30b: {"rows": [{"photo", "n", "original": {label, short_px, ...}|null, "app": {...}|null}]}
  v3:   {"photos": {photo: [{"box", "short_px", "names", "correct": {"M","O","T": bool}|null}]}}
Correct share = correct / (correct + wrong); unreadable boxes are not counted.
Size bins use the short side in the image Muse read (px); boxes with no size
(not-food boxes) count only in the total.
"""
import json, sys

BINS = [(0, 48), (48, 64), (64, 96), (96, 10**9)]


def pct(c, n):
    return "%d/%d = %.1f%%" % (c, n, 100.0 * c / n) if n else "0/0"


def score(items):
    """items: list of (label, short_px) with label correct|wrong|unreadable."""
    c = sum(1 for l, _ in items if l == "correct")
    w = sum(1 for l, _ in items if l == "wrong")
    u = sum(1 for l, _ in items if l == "unreadable")
    bins = []
    for lo, hi in BINS:
        g = [l for l, px in items if px is not None and px >= 0 and lo <= px < hi and l != "unreadable"]
        bins.append(("%d-%s px" % (lo, hi if hi < 10**9 else "up"), pct(sum(1 for l in g if l == "correct"), len(g))))
    return {"correct": pct(c, c + w), "unreadable": u, "by_short_side": bins}


def main(path):
    d = json.load(open(path))
    out = {"file": path.split("/")[-1]}
    if "rows" in d:
        for side in ("original", "app"):
            items = [(r[side]["label"], r[side]["short_px"]) for r in d["rows"] if r.get(side)]
            out[side] = score(items)
        per = {}
        for r in d["rows"]:
            p = per.setdefault(r["photo"], {"original": [0, 0, 0], "app": [0, 0, 0]})
            for side in ("original", "app"):
                if r.get(side):
                    p[side][("correct", "wrong", "unreadable").index(r[side]["label"])] += 1
        out["per_photo"] = {k: {s: "%d correct, %d wrong, %d unreadable" % tuple(v[s]) for s in v} for k, v in sorted(per.items())}
    else:
        for k, name in (("M", "marks:low"), ("O", "marksonly:low"), ("T", "marksonly:2pass:minimal:low")):
            items = []
            for rows in d["photos"].values():
                for r in rows:
                    if r["correct"] is None:
                        items.append(("unreadable", r["short_px"]))
                    elif k in r["names"]:
                        items.append(("correct" if r["correct"][k] else "wrong", r["short_px"]))
            out[name] = score(items)
    json.dump(out, sys.stdout, indent=1)
    print()


if __name__ == "__main__":
    main(sys.argv[1])
