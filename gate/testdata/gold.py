import hashlib, io, json
from PIL import Image
def pattern(w, h):
    # a pattern with edges and gradients (not the old tests' pattern)
    px = bytearray()
    for y in range(h):
        for x in range(w):
            px += bytes([(x * 37 + y * 11) % 256, (x * x + 3 * y) % 256, ((x ^ y) * 29 + 7) % 256])
    return Image.frombytes("RGB", (w, h), bytes(px))
def crop_first(im):
    w, h = im.size; s = min(w, h); x, y = (w - s) // 2, (h - s) // 2
    return im.crop((x, y, x + s, y + s))
out = {"pillow": Image.__version__ if hasattr(Image, "__version__") else "", "bilinear": [], "bicubic": []}
import PIL; out["pillow"] = PIL.__version__
for (w, h, s) in [(53, 31, 16), (31, 53, 20), (7, 9, 24), (1, 1, 5), (800, 600, 384), (300, 200, 192), (13, 13, 13)]:
    im = crop_first(pattern(w, h))
    for name, f in (("bilinear", Image.BILINEAR), ("bicubic", Image.BICUBIC)):
        r = im.resize((s, s), f)
        out[name].append([w, h, s, hashlib.sha256(r.tobytes()).hexdigest()])
# JPEG decode goldens: Pillow encodes, Pillow decodes; Go (turbojpeg) must equal the decode
jp = []
for (w, h, sub, prog, q) in [(64, 48, 2, False, 90), (48, 40, 1, True, 75), (33, 17, 0, False, 95)]:
    b = io.BytesIO(); pattern(w, h).save(b, "JPEG", quality=q, subsampling=sub, progressive=prog)
    raw = b.getvalue()
    rgb = Image.open(io.BytesIO(raw)).convert("RGB").tobytes()
    import base64
    jp.append({"w": w, "h": h, "sub": sub, "progressive": prog, "b64": base64.b64encode(raw).decode(), "sha": hashlib.sha256(rgb).hexdigest()})
out["jpeg"] = jp
json.dump(out, open("gold.json", "w"), indent=1)
print(out["pillow"], len(out["bilinear"]), len(jp))
from PIL import features; print("libjpeg_turbo", features.version("libjpeg_turbo"))
