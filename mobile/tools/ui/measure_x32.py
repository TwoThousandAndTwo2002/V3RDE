#!/usr/bin/env python3
"""v3rde measure_x32 -- the sizes and colours of the approved X32 mockup, measured from its shots.

Source: artifact https://claude.ai/artifact/6AMxFSoWfFfGMySXPefUKW (X32, approved by T9 on 2026-10-27),
files shots/phone-multi.jpg, phone-single.jpg, phone-busy.jpg (1170 x 2532 px = 390 x 844 pt, 3 px per pt)
and shots/desktop-multi.jpg (2880 x 1800 px = 1440 x 900 pt, 2 px per pt).

Method: each value comes from one scan line (a row or a column) across the element. An edge is a step of
more than `thr` (sum of the three channel steps) between two neighbour pixels. The scan lines are fixed below,
so a rerun gives the same numbers. A border is the band between the two edges around it. Text size: the pixel
height of one glyph divided by the height of the same glyph in the Poppins file (fontTools bounds / unitsPerEm).
The shots are JPEG, so a colour is within a few levels and an edge within about 1 px (0.3 pt on a phone).

usage: measure_x32.py <shots dir> <poppins dir> <out.json>
"""
import json, statistics, sys
from PIL import Image
from fontTools.ttLib import TTFont
from fontTools.pens.boundsPen import BoundsPen

SHOTS, FONTS, OUT = sys.argv[1], sys.argv[2], sys.argv[3]
OUTV, LOG = {}, []


def load(name):
    im = Image.open("%s/%s.jpg" % (SHOTS, name)).convert("RGB")
    return im.load()


def d(a, b):
    return sum(abs(a[i] - b[i]) for i in range(3))


def lum(c):
    return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]


def hexc(c):
    return "#%02X%02X%02X" % tuple(int(round(v)) for v in c)


def edges(px, mode, at, a, b, thr):
    get = (lambda i: px[i, at]) if mode == "row" else (lambda i: px[at, i])
    out, prev = [], get(a)
    for i in range(a + 1, b):
        c = get(i)
        if d(c, prev) > thr:
            out.append(i)
        prev = c
    return out


def first(px, mode, at, a, b, thr):
    e = edges(px, mode, at, a, b, thr)
    assert e, (mode, at, a, b)
    return e[0]


def last(px, mode, at, a, b, thr):
    e = edges(px, mode, at, a, b, thr)
    assert e, (mode, at, a, b)
    return e[-1]


def put(key, px_len, scale, how):
    OUTV[key] = {"pt": round(px_len / scale, 1), "px": px_len, "how": how}


def color(key, px, box, how):
    x0, y0, x1, y1 = box
    cs = [px[x, y] for x in range(x0, x1) for y in range(y0, y1)]
    OUTV[key] = {"color": hexc(tuple(statistics.median(c[i] for c in cs) for i in range(3))), "how": how}


FONT = {}


def glyph_em(weight, ch):
    if weight not in FONT:
        f = TTFont("%s/%s/Poppins_%s.ttf" % (FONTS, weight, weight))
        FONT[weight] = (f, f.getGlyphSet(), f["head"].unitsPerEm)
    f, gs, upm = FONT[weight]
    p = BoundsPen(gs)
    gs[f.getBestCmap()[ord(ch)]].draw(p)
    return (p.bounds[3] - p.bounds[1]) / upm


def text(key, px, box, dark, weight, ch, scale):
    x0, y0, x1, y1 = box
    cols = [any(dark(px[x, y]) for y in range(y0, y1)) for x in range(x0, x1)]
    s = cols.index(True); e = s
    while e < len(cols) and cols[e]:
        e += 1
    pts = [(x, y) for x in range(x0 + s, x0 + e) for y in range(y0, y1) if dark(px[x, y])]
    ys = [y for _, y in pts]
    h = max(ys) - min(ys) + 1
    core = sorted((px[x, y] for x, y in pts), key=lum)[: max(1, len(pts) // 5)]
    OUTV[key] = {"pt": round(h / scale / glyph_em(weight, ch), 1), "glyph": ch, "weight": weight, "glyph_px": h,
                 "color": hexc(tuple(statistics.median(c[i] for c in core) for i in range(3))),
                 "how": "first glyph in box %s; size = %d px / %d / %.4f em" % (box, h, scale, glyph_em(weight, ch))}


# ---------------- phone, review list: phone-multi.jpg (3 px per pt) ----------------
P, S = load("phone-multi"), 3
# Row y=880 crosses card 1 at mid height: card border, card fill, the thumbnail, ... the card's right border.
r = edges(P, "row", 880, 0, 460, 30)          # [border->fill, fill->thumb, (photo), ..., thumb->fill]
card_fill_l = r[0]                            # 51
card_out_l = first(P, "row", 880, 30, 60, 8)  # where the border band starts
thumb_l = r[1]
thumb_r = r[-1]
cr = edges(P, "row", 880, 900, 1170, 30)      # fill->border, border->backdrop
card_fill_r, card_out_r = cr[0], cr[-1]
put("screen.gutter", card_out_l, S, "row 880: backdrop to the card border")
put("card.border_width", card_fill_l - card_out_l, S, "row 880: border band")
put("card.width", card_out_r - card_out_l, S, "row 880: outer left to outer right")
put("card.inset", thumb_l - card_fill_l, S, "row 880: card fill to the thumbnail")
put("thumb.width", thumb_r - thumb_l, S, "row 880: thumbnail left to right")
# Column x=900 crosses card 1 and card 2 through the two fields (right of the text).
c = edges(P, "col", 900, 690, 1120, 30)
# c = [705 card border, 708 card fill, 726,727 field-1 top band, 729,730 field-1 fill, 866,867 bottom band, 869,870 card
#      fill, 894,895 field-2 top band, 897,898 fill, 1034,1035 bottom band, 1037,1038 card fill, 1056 card bottom band,
#      1059 backdrop, 1089 card-2 band, 1092 card-2 fill, ...]: a band runs from its first edge to the next fill edge.
card_top, card_fill_t = c[0], c[1]
f1_top, f1_end = c[2], c[9]
f2_top, f2_end = c[10], c[17]
card_fill_b, card_end = c[18], c[19]
card2_top = c[20]
put("card.height", card_end - card_top, S, "col 900: card top band start to bottom band end")
put("field.height", f1_end - f1_top, S, "col 900: field 1 top band start to bottom band end")
put("field.gap", f2_top - f1_end, S, "col 900: field 1 end to field 2 band start")
put("card.inset_top", f1_top - card_fill_t, S, "col 900: card fill top to field 1 band")
put("card.inset_bottom", card_fill_b - f2_end, S, "col 900: field 2 end to card bottom band")
put("card.gap", card2_top - card_end, S, "col 900: card 1 end to card 2 band")
th = edges(P, "col", 300, 690, 1080, 30)
put("thumb.height", th[-2] - th[1], S, "col 300: thumbnail top to bottom")
color("card.fill", P, (80, 1036, 120, 1046), "patch inside card 1")
color("card.border", P, (card_out_l, 875, card_fill_l, 885), "row 880 border band")
color("field.border", P, (900, f1_top, 901, f1_top + 3), "col 900 field 1 top border")
# Small icon button (the ✕ of card 1): a horizontal chord at y=760 and a vertical chord at x=1080.
h = edges(P, "row", 760, 960, 1125, 8)
hx0, hx1 = h[0], [x for x in h if x < 1110][-1]     # disc left, disc right (edges at 1119+ are the card border)
v = [y for y in edges(P, "col", 1080, 700, 1060, 8) if 740 < y < 860]   # the first disc only
vy0, vy1 = v[0], v[-1]
cx, cy = (hx0 + hx1) / 2, (vy0 + vy1) / 2
rad = ((((hx1 - hx0) / 2) ** 2) + (cy - 760) ** 2) ** 0.5
put("iconbtn.small", round(2 * rad), S, "disc from 2 chords: row 760 [%d,%d], col 1080 [%d,%d]" % (hx0, hx1, vy0, vy1))
color("iconbtn.fill", P, (int(cx) + 20, int(cy) - 25, int(cx) + 30, int(cy) - 15), "inside the disc, off the glyph")
put("iconbtn.gap_to_card_edge", card_fill_r - int(cx + rad), S, "disc right to card fill right (centre + r)")
# Buttons: row y=2400 is the middle of the two pills; column x=690 (red) and x=150 (white).
b = edges(P, "row", 2400, 0, 640, 25)
# b = [48 white pill band, 51 fill, (text), 567 right band, 570 backdrop, 599, 600 red]
lp_l, lp_r, rp_l = b[0], [x for x in b if 560 < x < 590][-1], [x for x in b if x > 590][0]
rp_r = last(P, "row", 2400, 1080, 1170, 25)
put("pill.left_x", lp_l, S, "row 2400: screen to the white pill border")
put("pill.width", lp_r - lp_l, S, "row 2400: white pill outer width")
put("pill.gap", rp_l - lp_r, S, "row 2400: white pill to red pill")
put("pill.primary.width", rp_r - rp_l, S, "row 2400: red pill outer width")
rv = edges(P, "col", 690, 2290, 2500, 40)
put("pill.height", rv[-2] - rv[0], S, "col 690: red pill top to bottom")
put("sheet.bottom_padding", 2532 - rv[-2], S, "col 690: red pill bottom to the screen bottom")
color("pill.primary.fill", P, (640, 2380, 700, 2420), "inside the red pill, left of the text")
color("pill.secondary.fill", P, (90, 2380, 150, 2420), "inside the white pill, left of the text")
# Sheet: top edge at x=585, the handle, and the top-left corner from 6 columns.
sheet_top = 381
assert sheet_top in edges(P, "col", 585, 300, 420, 20)
hd = edges(P, "col", 585, 395, 440, 6)
hw = edges(P, "row", 418, 480, 700, 6)
put("sheet.handle.height", hd[-1] - hd[0], S, "col 585: handle top to bottom")
put("sheet.handle.width", hw[-1] - hw[0], S, "row 418: handle left to right")
put("sheet.handle.top", hd[0] - sheet_top, S, "sheet top to the handle")
corner = {x: first(P, "col", x, 360, 520, 25) - sheet_top for x in (1, 4, 10, 20, 40, 60)}
best = min(range(30, 120), key=lambda r: sum((r - (r * r - (r - x) ** 2) ** 0.5 - o) ** 2 for x, o in corner.items()))
put("sheet.radius", best, S, "least-squares circle fit to the top-left edge, columns %s" % corner)
# Corner radii: the first edge from above in columns near a corner, fitted to a circle (least squares).
def fit(offsets, lo=10, hi=120):
    return min(range(lo, hi), key=lambda r: sum((r - (max(0, r * r - (r - x) ** 2)) ** 0.5 - o) ** 2
                                               for x, o in offsets.items() if x < r))
def white_from(px, x, y0, y1):
    return next(y for y in range(y0, y1) if min(px[x, y]) >= 248)
# The card border is close to the sheet tint, so the corner is read on the inner white fill (radius - 1 pt border).
cc = {x - card_fill_l: white_from(P, x, 690, 820) - card_fill_t for x in range(card_fill_l, card_fill_l + 60, 3)}
put("card.radius", fit(cc) + (card_fill_l - card_out_l), S, "card 1 fill corner offsets %s, + the border" % cc)
tc = {x - thumb_l: first(P, "col", x, card_fill_t + 5, 800, 60) - th[1] for x in range(thumb_l + 1, thumb_l + 40, 3)}
put("thumb.radius", fit(tc, 6, 80), S, "thumbnail top-left corner, offsets %s" % tc)
# Text.
dark = lambda c: lum(c) < 110
text("type.title", P, (40, 470, 120, 540), dark, "600SemiBold", "6", S)
text("type.body.muted", P, (40, 560, 110, 610), lambda c: lum(c) < 150 and max(c) - min(c) < 40, "400Regular", "C", S)
text("type.field", P, (430, 770, 470, 830), dark, "400Regular", "R", S)
text("type.button", P, (200, 2370, 260, 2440), dark, "600SemiBold", "S", S)
text("type.button.on_primary", P, (700, 2370, 760, 2440), lambda c: min(c) > 200, "600SemiBold", "P", S)
mut = lambda c: lum(c) < 150 and max(c) - min(c) < 40
rows = [y for y in range(540, 680) if any(mut(P[x, y]) for x in range(48, 400))]
tops = [rows[0]] + [b for a, b in zip(rows, rows[1:]) if b - a > 6]
put("type.body.line_height", tops[1] - tops[0], S, "muted text: glyph tops of line 1 and line 2 (rows %s)" % tops[:2])
mu = [y for y in range(540, 680) if any(lum(P[x, y]) < 150 and max(P[x, y]) - min(P[x, y]) < 40 for x in range(48, 120))]
put("sheet.top_to_title", 470 - sheet_top + (min(y for y in range(470, 540) if any(dark(P[x, y]) for x in range(48, 90))) - 470), S,
    "sheet top to the top of the title glyph")

# ---------------- phone, one-item form: phone-single.jpg ----------------
G = load("phone-single")
gv = edges(G, "col", 700, 1440, 2280, 30)
put("field.multiline.height", gv[-2] - [y for y in gv if y > 2000][0], S, "col 700: pickup field outer height")
gb = edges(G, "col", 150, 1780, 1960, 8)
y0, y1 = gb[3], gb[9]                      # 1811 into the disc fill, 1920 out of it
left = first(G, "row", 1865, 0, 260, 8)    # disc left edge on its middle row
half, dx0 = (y1 - y0) / 2, 150 - left      # (dx0 - r)^2 + half^2 = r^2
rr = (dx0 * dx0 + half * half) / (2 * dx0)
put("iconbtn.big", round(2 * rr), S, "chord at x=150 [%d,%d] and left edge x=%d on row 1865: r = (dx^2 + h^2) / 2dx" % (y0, y1, left))
text("type.label", G, (60, 1400, 110, 1450), lambda c: lum(c) < 150 and max(c) - min(c) < 40, "400Regular", "T", S)
text("type.sheet_title_form", G, (45, 1290, 90, 1350), dark, "600SemiBold", "Y", S)

# ---------------- phone, small error sheet: phone-busy.jpg ----------------
B = load("phone-busy")
text("type.error", B, (80, 2300, 140, 2360), lambda c: c[0] > 150 and c[1] < 120, "400Regular", "T", S)

# ---------------- desktop, review dialog: desktop-multi.jpg (2 px per pt) ----------------
K, S2 = load("desktop-multi"), 2
dl = first(K, "row", 1400, 500, 700, 20)
dt = first(K, "col", 1400, 60, 200, 20)
db = edges(K, "col", 1100, 1600, 1780, 20)[-2]
put("dialog.left", dl, S2, "row 1400: screen to the dialog")
put("dialog.width", 2880 - 2 * dl, S2, "symmetric: 1440 pt - 2 x left")
put("dialog.top", dt, S2, "col 1400: screen to the dialog")
put("dialog.height", db - dt, S2, "col 1100: dialog top to bottom")
ph = edges(K, "row", 700, 600, 720, 30)[0], edges(K, "row", 700, 1380, 1480, 30)[0]
put("dialog.padding", ph[0] - dl, S2, "row 700: dialog edge to the photo")
put("dialog.photo.width", ph[1] - ph[0], S2, "row 700: photo left to right")
cl = first(K, "row", 450, 1440, 1500, 20) - 2   # the fill edge; the border band is the 2 px before it
put("dialog.column_gap", cl - ph[1], S2, "row 700/450: photo right to the card border")
put("dialog.card.width", edges(K, "row", 450, 2200, 2270, 20)[-1] - cl, S2, "row 450: card outer width (border bands included)")
dp = edges(K, "row", 1585, 600, 700, 25)[0], edges(K, "row", 1585, 1400, 1480, 25)
put("dialog.pill.width", dp[1][1] + 1 - dp[0], S2, "row 1585: white pill outer width")
put("dialog.pill.gap", dp[1][-1] - dp[1][1] - 1, S2, "row 1585: white pill to red pill")
dv = edges(K, "col", 1600, 1520, 1660, 30)
put("dialog.pill.height", dv[3] - dv[1], S2, "col 1600: red pill top to bottom")
db_end = edges(K, "col", 1100, 1600, 1780, 20)[-1]
put("dialog.padding_bottom", db_end - dv[3], S2, "col 1600/1100: red pill bottom to the end of the dialog edge band")

json.dump({"source": __doc__.split("\n")[2:5], "values": OUTV}, open(OUT, "w"), indent=1)
for k, v in OUTV.items():
    val = ("%6.1f pt" % v["pt"]) if "pt" in v else ""
    print("%-28s %-10s %-8s %s" % (k, val, v.get("color", ""), v["how"][:110]))
