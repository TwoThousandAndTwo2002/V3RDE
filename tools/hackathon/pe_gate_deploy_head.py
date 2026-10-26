#!/usr/bin/env python3
"""Hackathon #3a: the deployed preprocess for the PE-Core gate, and its head.

WHY. PE-Core was trained with timm's preprocess (resize the short side to
384, then centre-crop; bicubic; (x-0.5)/0.5). The server gate crops the
centre square FIRST and then resizes (the phone path does the same), and Go
must match Python bit for bit. So ONE preprocess is fixed for the server and
the phone, and the head is fitted on exactly that:

  crop-first centre square (origin by integer division) -> Pillow BICUBIC to
  384 x 384 -> float32 (x/255 - 0.5)/0.5, NHWC -> the fp16-weight .tflite
  (LiteRT) -> L2-normalised 384-d embedding -> logistic head.

Outputs (results/):  the head metrics next to the timm-preprocess head (#1, #2);
  Go goldens: sha256 of Pillow bicubic on the gate test pattern; the LiteRT
  reference output of the fp16w model on the deterministic test input.
The head JSON (with its preprocess) goes to $ICARUS_GATE_DATA, untracked.

  ICARUS_GATE_DATA=<gold-set dir> python3 tools/hackathon/pe_gate_deploy_head.py
"""
import hashlib, json, os, sys, time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import pe_gate_bench as B

SIDE = 384
MEAN, STD = 0.5, 0.5
F16 = os.path.join(B.MODELS, "gate_pe_s16_f16w_384.tflite")
NPZ = os.path.join(B.GRID, "pe_s16_deploy_bicubic_cropfirst_v1.npz")
HEAD = os.path.join(B.MODELS, "gate_head_pe_s16_384_deploy.json")
LITERT_REF = os.path.join(B.MODELS, "litert_ref_pe_s16_f16w.json")
TIMM = {"thr": 0.287, "hel_catch": 0.839, "hel_frt": 0.023, "auroc": 0.9801, "oracle_catch": 0.863}


def crop_box(w, h):
    s = min(w, h)
    x, y = (w - s) // 2, (h - s) // 2
    return (x, y, x + s, y + s)


def tensor(img):
    """The deployed preprocess. Must equal gate/preprocess.go TensorFor(...)."""
    import numpy as np
    from PIL import Image
    im = img.convert("RGB")
    im = im.crop(crop_box(*im.size)).resize((SIDE, SIDE), Image.BICUBIC)
    a = np.asarray(im, dtype=np.float32) / 255.0
    return ((a - MEAN) / STD)[None, ...].astype(np.float32)          # NHWC


def pattern(w, h):
    return bytes(((i * 31 + 7) % 256) for i in range(w * h * 3))


def goldens():
    from PIL import Image
    out = []
    for w, h, s in ((41, 29, 16), (29, 41, 16), (9, 7, 16), (640, 480, 384)):
        im = Image.frombytes("RGB", (w, h), pattern(w, h))
        r = im.crop(crop_box(w, h)).resize((s, s), Image.BICUBIC)
        out.append({"w": w, "h": h, "side": s, "sha256": hashlib.sha256(r.tobytes()).hexdigest()})
    return out


def main():
    import numpy as np, PIL
    from PIL import Image
    from ai_edge_litert.interpreter import Interpreter
    for p in (B.LABELS, B.SPLIT, F16):
        if not os.path.exists(p):
            B.die("missing %s" % p)
    bar = B.Bar(4)

    bar.step("embed (deploy path)")
    it = Interpreter(model_path=F16, num_threads=4); it.allocate_tensors()
    i, o = it.get_input_details()[0], it.get_output_details()[0]
    if list(i["shape"]) != [1, SIDE, SIDE, 3]:
        B.die("model input %s, expected [1,%d,%d,3]" % (list(i["shape"]), SIDE, SIDE))
    if not os.path.exists(NPZ):
        sids, E, kept, t0 = B.sample_ids(), [], [], time.time()
        for k, s in enumerate(sids):
            try:
                x = tensor(Image.open(os.path.join(B.IMAGES, s + ".jpg")))
            except Exception:
                continue
            it.set_tensor(i["index"], x); it.invoke()
            E.append(it.get_tensor(o["index"]).ravel().copy()); kept.append(s)
            if k % 25 == 0:
                bar.draw("embed (deploy path)", "%d/%d %.0f/s" % (k + 1, len(sids), (k + 1) / max(time.time() - t0, 1e-9)))
        E = np.stack(E)
        n = np.linalg.norm(E, axis=1)
        if not np.allclose(n, 1.0, atol=1e-2):
            B.die("embeddings not unit norm (%.4f..%.4f)" % (n.min(), n.max()))
        np.savez(NPZ, sample_ids=np.array(kept), embeddings=E, seconds=time.time() - t0)

    bar.step("fit head")
    truth, (dev_ids, hel_ids) = B.load_truth(), B.load_split()
    r = B.fit_cell(NPZ, truth, dev_ids, hel_ids)
    head = {"schema": 1, "encoder": "PE-Core-S16-384/timm:vit_pe_core_small_patch16_384.fb", "precision": "float16w",
            "input_side": SIDE, "embedding_dim": len(r["coef"]), "normalised_input": True,
            "preprocess": {"geometry": "crop_first_centre_square", "resample": "pillow_bicubic",
                           "mean": [MEAN] * 3, "std": [STD] * 3, "layout": "NHWC"},
            "threshold": r["thr"], "intercept": r["intercept"], "coef": r["coef"],
            "model_sha256": hashlib.sha256(open(F16, "rb").read()).hexdigest(),
            "accepted_on": {"protocol": "OOF sweep on dev (n=%d), applied once to heldout (n=%d)" % (r["n_dev"], r["n_hel"]),
                            "heldout_catch": r["hel_catch"], "heldout_false_retake": r["hel_frt"],
                            "heldout_auroc": r["auroc"], "ceiling_false_retake": B.CEILING,
                            "oracle_catch_at_ceiling": r["oracle_catch"]}}
    json.dump(head, open(HEAD, "w"), indent=1)

    bar.step("go goldens")
    gold = goldens()
    n = 1
    for d in i["shape"]:
        n *= int(d)
    x = (np.arange(n, dtype=np.int64) * 7919 % 255).astype(np.float32).reshape(tuple(i["shape"])) / 255.0
    it.set_tensor(i["index"], x); it.invoke()
    y = it.get_tensor(o["index"]).ravel()
    json.dump({"argmax": int(np.argmax(y)), "y": [float(v) for v in y]}, open(LITERT_REF, "w"))

    bar.step("report")
    stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    ok = r["hel_frt"] <= B.CEILING and r["hel_catch"] >= 0.817
    rep = {"preprocess": head["preprocess"], "pillow": PIL.__version__, "model": os.path.basename(F16),
           "model_sha256": head["model_sha256"],
           "head": {"file": os.path.basename(HEAD), "sha256": hashlib.sha256(open(HEAD, "rb").read()).hexdigest(),
                    "threshold": r["thr"], "dim": len(r["coef"])},
           "heldout": {"deploy": {k: r[k] for k in ("thr", "hel_catch", "hel_frt", "auroc", "oracle_catch")}, "timm": TIMM},
           "pass": ok, "go_goldens_pillow_bicubic": gold,
           "litert_ref": {"file": os.path.basename(LITERT_REF), "argmax": int(np.argmax(y))}}
    L = ["", "=== PE-Core-S16 deployed preprocess + head ===",
         "preprocess: crop-first centre square -> Pillow bicubic 384 -> (x/255-0.5)/0.5, NHWC; model fp16w .tflite",
         "", "%-28s %7s %7s %8s %8s %8s" % ("head", "AUROC", "thr", "helFRT", "helCatch", "oracle"),
         "%-28s %7.4f %7.3f %8.3f %8.3f %8.3f" % ("timm preprocess (#1, #2)", TIMM["auroc"], TIMM["thr"], TIMM["hel_frt"], TIMM["hel_catch"], TIMM["oracle_catch"]),
         "%-28s %7.4f %7.3f %8.3f %8.3f %8.3f" % ("deploy preprocess (this)", r["auroc"], r["thr"], r["hel_frt"], r["hel_catch"], r["oracle_catch"]),
         "bar: false retake <= 3%%, catch >= 0.817 (old gate)  ->  %s" % ("PASS" if ok else "FAIL"),
         "", "Go goldens (Pillow %s bicubic, crop-first):" % PIL.__version__]
    L += ["  %dx%d -> %d  %s" % (g["w"], g["h"], g["side"], g["sha256"]) for g in gold]
    L += ["LiteRT ref: argmax %d, %d values -> %s (untracked)" % (int(np.argmax(y)), len(y), os.path.basename(LITERT_REF))]
    txt = "\n".join(L)
    print(txt)
    open(os.path.join(HERE, "results", "deploy_head_%s.txt" % stamp), "w").write(txt + "\n")
    json.dump(rep, open(os.path.join(HERE, "results", "deploy_head_%s.json" % stamp), "w"), indent=1)
    print("\nsaved: results/deploy_head_%s.{txt,json}" % stamp)


if __name__ == "__main__":
    main()
