import hashlib, json, numpy as np
from PIL import Image
exec(open("gold.py").read().split("def crop_first")[0])  # pattern()
def crop_first(im):
    w, h = im.size; s = min(w, h); x, y = (w - s) // 2, (h - s) // 2
    return im.crop((x, y, x + s, y + s))
# a PE tensor golden: Pillow bicubic, then (x/255 - mean)/std in float32, NHWC
im = crop_first(pattern(53, 31)).resize((16, 16), Image.BICUBIC)
a = np.asarray(im, dtype=np.uint8).astype(np.float32)
x = ((a / np.float32(255.0)) - np.float32(0.5)) / np.float32(0.5)
pe = hashlib.sha256(x.astype("<f4").tobytes()).hexdigest()
# non-square resizes (one side the same, and both different)
ns = []
for (w, h, ow, oh, f, name) in [(40, 30, 40, 12, Image.BILINEAR, "bilinear"), (40, 30, 17, 30, Image.BICUBIC, "bicubic"), (61, 23, 9, 31, Image.BICUBIC, "bicubic"), (61, 23, 100, 7, Image.BILINEAR, "bilinear")]:
    r = pattern(w, h).resize((ow, oh), f)
    ns.append([w, h, ow, oh, name, hashlib.sha256(r.tobytes()).hexdigest()])
print(json.dumps({"pe": pe, "ns": ns}))
