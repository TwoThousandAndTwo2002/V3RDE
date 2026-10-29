#!/usr/bin/env python3
"""Compares the edges of two shots of the same state (X32 approved shot vs. the new V3RDE shot) on fixed scan lines.

For each scan line, every edge of the X32 shot (a step > thr between neighbour pixels) is matched to the nearest
edge of the new shot within 18 px; the report gives the shift in pt and the edges with no match.
usage: parity_compare.py <x32 dir> <new dir> <out.json>
"""
import json, sys
from PIL import Image

OLD, NEW, OUT = sys.argv[1], sys.argv[2], sys.argv[3]
LINES = {  # state: (scale, [(mode, at, a, b, thr), ...])
    "phone-multi": (3, [("row", 880, 0, 1170, 30), ("row", 2400, 0, 1170, 25), ("col", 900, 300, 2532, 30),
                        ("col", 300, 300, 1100, 30), ("col", 690, 2200, 2532, 40), ("col", 60, 300, 2532, 30)]),
    "phone-single": (3, [("col", 700, 1100, 2532, 30), ("row", 1865, 0, 1170, 20), ("col", 60, 1100, 2532, 30)]),
    "phone-busy": (3, [("col", 585, 1300, 2532, 25), ("row", 2190, 0, 1170, 25)]),
    "phone-already-listed": (3, [("col", 585, 1300, 2532, 25)]),
    "phone-post-partial": (3, [("col", 900, 300, 2532, 30)]),
    "desktop-multi": (2, [("row", 700, 0, 2880, 30), ("row", 450, 0, 2880, 20), ("col", 1100, 0, 1800, 20),
                          ("col", 1600, 1400, 1800, 30)]),
    "desktop-already-listed": (2, [("row", 900, 0, 2880, 20), ("col", 1440, 600, 1200, 20), ("row", 963, 0, 2880, 25)]),
}


def load(path):
    return Image.open(path).convert("RGB").load()


def edges(px, mode, at, a, b, thr):
    get = (lambda i: px[i, at]) if mode == "row" else (lambda i: px[at, i])
    out, prev = [], get(a)
    for i in range(a + 1, b):
        c = get(i)
        if sum(abs(c[k] - prev[k]) for k in range(3)) > thr:
            if not out or i - out[-1] > 2:   # one edge per step (antialias runs of 1-2 px count once)
                out.append(i)
        prev = c
    return out


report, worst = {}, 0.0
for state, (scale, lines) in LINES.items():
    try:
        po, pn = load("%s/%s.jpg" % (OLD, state)), load("%s/%s.png" % (NEW, state))
    except FileNotFoundError:
        continue
    rows = []
    for mode, at, a, b, thr in lines:
        eo, en = edges(po, mode, at, a, b, thr), edges(pn, mode, at, a, b, thr)
        shifts, missing = [], []
        for e in eo:
            near = min(en, key=lambda x: abs(x - e)) if en else None
            if near is None or abs(near - e) > 18:
                missing.append(round(e / scale, 1))
            else:
                shifts.append(round((near - e) / scale, 1))
        big = [s for s in shifts if abs(s) > 1.0]
        worst = max([worst] + [abs(s) for s in shifts])
        rows.append({"line": "%s %d" % (mode, at), "x32_edges": len(eo), "new_edges": len(en), "matched": len(shifts),
                     "max_shift_pt": max([abs(s) for s in shifts], default=0), "shifts_over_1pt": big, "x32_unmatched_pt": missing})
    report[state] = rows
json.dump(report, open(OUT, "w"), indent=1)
for st, rows in report.items():
    for r in rows:
        print("%-24s %-9s matched %3d/%-3d max %4.1f pt  over1: %s  unmatched: %s" % (
            st, r["line"], r["matched"], r["x32_edges"], r["max_shift_pt"], r["shifts_over_1pt"][:8], r["x32_unmatched_pt"][:8]))
print("largest matched shift: %.1f pt" % worst)
