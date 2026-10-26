#!/usr/bin/env python3
"""X28 bench: Meta PE-Core gate candidates vs MobileCLIP2-S0, same protocol.

WHY. MobileCLIP2 weights are apple-amlr (research only), so the gate needs a
replacement with rights for a product (T9 2026-10-26). Data: $ICARUS_GATE_DATA (untracked). Candidates are Meta
Perception Encoder, Apache-2.0, sized near today's 46 MB fp32 gate:
  pe_s16  vit_pe_core_small_patch16_384.fb  fp16  (~22M params, ~44 MB fp16)
  pe_t16  vit_pe_core_tiny_patch16_384.fb   fp32  (~6M params,  ~24 MB fp32)
  base    MobileCLIP2-S0 fp32 @192 (cached prior-product embeddings, the shipped gate)

PROTOCOL = the prior product's gate grid, copied below unchanged: 5-fold OOF on
DEV, threshold = highest whose OOF false-retake <= 3%, applied ONCE to
HELDOUT; oracle catch reported as the upper bound. Each encoder gets its own
head. Preprocessing comes from timm's own data config (prior-product lesson: never
retype normalisation).

COST, measured per candidate in a fresh subprocess on CPU with 1 thread
(the box runs 1 interpreter x 1 thread): ms/img and peak RSS. Mac numbers
are RELATIVE; X28 measures the box with the .tflite.

  ICARUS_GATE_DATA=<gold-set dir> python3 tools/hackathon/pe_gate_bench.py
"""
import argparse, json, os, resource, subprocess, sys, time

HERE = os.path.dirname(os.path.abspath(__file__))

# ---- protocol, copied from the prior product's gate grid (unchanged logic) ----
MODELS = os.path.expanduser(os.environ.get("ICARUS_GATE_DATA", "~/icarus_data/gate_models"))
GRID = os.path.join(MODELS, "grid")
IMAGES = os.path.join(MODELS, "gate")
LABELS = os.path.join(MODELS, "gate_gold.jsonl")
SPLIT = os.path.join(MODELS, "gate_split.json")
CEILING, SEED, FOLDS = 0.03, 13, 5


def pick_device(precision):
    """Apple Silicon's GPU via torch mps, which is several times faster than
    CPU here. int8 stays on CPU: torch's quantized kernels are CPU-only, and
    silently falling back would make the int8 cells measure something other
    than what they claim.

    NOTE ON ms/img: this is Mac time, not phone time. It is a RELATIVE number
    for comparing cells, never an estimate of on-device latency -- that has to
    be measured on the phone with the .tflite.
    """
    import torch
    if precision == "int8":
        return "cpu"
    try:
        if torch.backends.mps.is_available():
            return "mps"
    except Exception:
        pass
    return "cpu"


def die(m):
    sys.stdout.write("\nABORT: %s\n" % m)
    sys.exit(1)


class Bar:
    def __init__(self, total, width=28):
        self.total, self.width, self.n, self.t0 = max(total, 1), width, 0, time.time()

    def draw(self, msg, extra=""):
        k = int(self.n * self.width / self.total)
        sys.stdout.write("\r[%s%s] %d/%d %-24s %-28s" % (
            "#" * k, " " * (self.width - k), self.n, self.total, msg, extra))
        sys.stdout.flush()

    def step(self, msg, extra=""):
        self.n += 1
        self.draw(msg, extra)

    def line(self, msg):
        sys.stdout.write("\r%-92s\r    %s\n" % ("", msg))
        sys.stdout.flush()


def load_truth():
    t = {}
    for line in open(LABELS, encoding="utf-8", errors="replace"):
        try:
            g = json.loads(line)
        except ValueError:
            continue
        if g.get("sample_id"):
            t[g["sample_id"]] = (str(g.get("choice", "")).lower() == "food")
    return t


def load_split():
    s = json.load(open(SPLIT))
    if isinstance(s, dict):
        for dk, hk in (("dev", "heldout"), ("DEV", "HELDOUT")):
            if dk in s and hk in s:
                return set(map(str, s[dk])), set(map(str, s[hk]))
        vals = {str(v).lower() for v in s.values()}
        if vals and vals <= {"dev", "heldout"}:
            return ({k for k, v in s.items() if str(v).lower() == "dev"},
                    {k for k, v in s.items() if str(v).lower() == "heldout"})
    die("gate_split.json shape not recognised -- refusing to guess a split")


def sample_ids():
    if not os.path.isdir(IMAGES):
        die("no images at %s -- extract gate_images*.tar there first" % IMAGES)
    out = []
    for f in sorted(os.listdir(IMAGES)):
        if f.endswith(".jpg"):
            out.append(f[:-4])
    return out


def rates(p, y, t):
    import numpy as np
    refuse = p <= t
    nf = ~y
    return (float((refuse & nf).sum()) / max(int(nf.sum()), 1),
            float((refuse & y).sum()) / max(int(y.sum()), 1))


def fit_cell(npz, truth, dev_ids, hel_ids):
    import numpy as np
    from sklearn.linear_model import LogisticRegression
    from sklearn.model_selection import StratifiedKFold
    from sklearn.metrics import roc_auc_score

    z = np.load(npz, allow_pickle=True)
    ids = [str(s) for s in z["sample_ids"]]
    E, idx = z["embeddings"], None
    idx = {s: i for i, s in enumerate(ids)}
    secs = float(z["seconds"]) if "seconds" in z else 0.0

    d = [s for s in ids if s in truth and s in dev_ids]
    h = [s for s in ids if s in truth and s in hel_ids]
    Xd, yd = E[[idx[s] for s in d]], np.array([truth[s] for s in d])
    Xh, yh = E[[idx[s] for s in h]], np.array([truth[s] for s in h])

    oof = np.zeros(len(yd))
    for tr, te in StratifiedKFold(FOLDS, shuffle=True,
                                  random_state=SEED).split(Xd, yd):
        m = LogisticRegression(max_iter=5000, C=30.0).fit(Xd[tr], yd[tr])
        oof[te] = m.predict_proba(Xd[te])[:, 1]

    grid = [i / 1000.0 for i in range(1, 1000)]
    thr = 0.0
    for t in grid:
        if rates(oof, yd, t)[1] <= CEILING:
            thr = t

    final = LogisticRegression(max_iter=5000, C=30.0).fit(Xd, yd)
    ph = final.predict_proba(Xh)[:, 1]
    hc, hf = rates(ph, yh, thr)

    oracle_c = 0.0
    for t in grid:
        c, f = rates(ph, yh, t)
        if f <= CEILING:
            oracle_c = c

    return {"thr": thr, "hel_catch": hc, "hel_frt": hf,
            "oracle_catch": oracle_c,
            "auroc": float(roc_auc_score(yh, ph)),
            "ms_per_img": 1000.0 * secs / max(len(ids), 1),
            "coef": [float(x) for x in final.coef_[0]],
            "intercept": float(final.intercept_[0]),
            "n_dev": len(d), "n_hel": len(h)}



CANDS = {
    "pe_s16": ("vit_pe_core_small_patch16_384.fb", "float16"),
    "pe_t16": ("vit_pe_core_tiny_patch16_384.fb", "float32"),
}
BASE_NPZ = os.path.join(GRID, "s0_float32_192_v2.npz")
OUT = os.path.join(HERE, "results")


def build(name):
    import timm, torch
    m = timm.create_model(name, pretrained=True, num_classes=0).eval()
    cfg = timm.data.resolve_data_config({}, model=m)
    tf = timm.data.create_transform(**cfg)          # the library's own preprocess

    class Norm(torch.nn.Module):
        def __init__(self, net):
            super().__init__(); self.net = net
        def forward(self, x):
            f = self.net(x)
            return f / f.norm(dim=-1, keepdim=True)
    params = sum(p.numel() for p in m.parameters())
    return Norm(m).eval(), tf, cfg, params


def embed(key, sids, bar):
    import numpy as np, torch
    from PIL import Image
    name, prec = CANDS[key]
    npz = os.path.join(GRID, "%s_%s_v1.npz" % (key, prec))
    if os.path.exists(npz):
        bar.line("cached: %s" % os.path.basename(npz)); return npz
    net, tf, cfg, _ = build(name)
    dev = pick_device("float32")
    dt = torch.float16 if prec == "float16" else torch.float32
    if prec == "float16" and dev == "cpu":
        die("float16 needs mps here; CPU half kernels would measure something else")
    net = net.to(dev).to(dt)
    embs, kept, B, t0 = [], [], 32, time.time()
    for i in range(0, len(sids), B):
        ims, ok = [], []
        for s in sids[i:i + B]:
            try:
                ims.append(tf(Image.open(os.path.join(IMAGES, s + ".jpg")).convert("RGB"))); ok.append(s)
            except Exception:
                pass
        if not ims:
            continue
        with torch.no_grad():
            f = net(torch.stack(ims).to(dev).to(dt))
        embs.append(f.float().cpu().numpy()); kept.extend(ok)
        bar.draw("%s %s %s" % (key, prec, dev), "%d/%d" % (min(i + B, len(sids)), len(sids)))
    E = np.concatenate(embs, 0)
    n = np.linalg.norm(E, axis=1)
    if not np.allclose(n, 1.0, atol=1e-2):
        die("%s: embeddings not unit norm (%.4f..%.4f)" % (key, n.min(), n.max()))
    np.savez(npz, sample_ids=np.array(kept), embeddings=E, seconds=time.time() - t0,
             input_size=np.array(cfg["input_size"]))
    return npz


def cpu_probe(key, n=32):
    """Fresh process: fp32 weights on CPU, 1 thread, n images one at a time."""
    import torch
    from PIL import Image
    torch.set_num_threads(1)
    name, prec = CANDS[key]
    net, tf, cfg, params = build(name)
    sids = sample_ids()[:n]
    xs = [tf(Image.open(os.path.join(IMAGES, s + ".jpg")).convert("RGB")).unsqueeze(0) for s in sids]
    with torch.no_grad():
        net(xs[0])                                   # warm-up
        t0 = time.time()
        for x in xs:
            net(x)
        ms = 1000.0 * (time.time() - t0) / len(xs)
    rss = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    rss_mib = rss / (1024 * 1024) if sys.platform == "darwin" else rss / 1024
    print(json.dumps({"key": key, "params": params, "input": list(cfg["input_size"]),
                      "cpu1_ms": ms, "peak_rss_mib": rss_mib,
                      "mb_fp32": params * 4 / 1e6, "mb_fp16": params * 2 / 1e6}))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--probe")
    ap.add_argument("--only", default="pe_s16,pe_t16")
    a = ap.parse_args()
    if a.probe:
        return cpu_probe(a.probe)
    keys = [k for k in a.only.split(",") if k in CANDS]
    for p in (LABELS, SPLIT, BASE_NPZ):
        if not os.path.exists(p):
            die("missing %s" % p)
    sids, truth = sample_ids(), load_truth()
    dev_ids, hel_ids = load_split()
    bar = Bar(1 + 3 * len(keys))
    bar.line("%d images, dev=%d heldout=%d" % (len(sids), len(dev_ids), len(hel_ids)))
    rows = {}
    bar.step("base: fit")
    rows["base"] = fit_cell(BASE_NPZ, truth, dev_ids, hel_ids)
    rows["base"].update({"params": 11.4e6, "mb_fp32": 46.2, "file": "gate_s0_f32_192.tflite (measured)"})
    for k in keys:
        bar.step("%s: embed" % k)
        npz = embed(k, sids, bar)
        bar.step("%s: fit" % k)
        rows[k] = fit_cell(npz, truth, dev_ids, hel_ids)
        bar.step("%s: cpu probe" % k)
        p = subprocess.run([sys.executable, __file__, "--probe", k], capture_output=True, text=True)
        try:
            rows[k].update(json.loads(p.stdout.strip().splitlines()[-1]))
        except Exception:
            bar.line("%s probe failed: %s" % (k, (p.stderr or "")[-300:]))
    os.makedirs(OUT, exist_ok=True)
    stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    L = ["", "=== X28 GATE BENCH -- heldout, ceiling false-retake <= 3% ===",
         "%-7s %6s %6s %7s %6s %7s %8s %7s %8s %9s %8s" % ("model", "params", "MB", "AUROC", "thr",
         "helFRT", "helCatch", "oracle", "cpu1ms", "peakMiB", "ceiling")]
    for k, r in rows.items():
        mb = r.get("mb_fp32") if k != "pe_s16" else r.get("mb_fp16")
        L.append("%-7s %5.1fM %6.1f %7.4f %6.3f %7.3f %8.3f %7.3f %8s %9s %8s" % (
            k, r.get("params", 0) / 1e6, mb or 0, r["auroc"], r["thr"], r["hel_frt"], r["hel_catch"],
            r["oracle_catch"], "%.1f" % r["cpu1_ms"] if "cpu1_ms" in r else "-",
            "%.0f" % r["peak_rss_mib"] if "peak_rss_mib" in r else "-",
            "ok" if r["hel_frt"] <= CEILING else "OVER"))
    L.append("MB: base = measured .tflite fp32; pe_s16 = params x 2 (fp16); pe_t16 = params x 4 (fp32).")
    L.append("cpu1ms / peakMiB: Mac CPU, 1 thread, torch fp32 -- RELATIVE only; the box is measured in X28.")
    txt = "\n".join(L)
    print(txt)
    open(os.path.join(OUT, "bench_%s.txt" % stamp), "w").write(txt + "\n")
    for k, r in rows.items():
        r.pop("coef", None) if k == "base" else None
    json.dump(rows, open(os.path.join(OUT, "bench_%s.json" % stamp), "w"), indent=1)
    print("\nsaved: %s/bench_%s.{txt,json}" % (OUT, stamp))


if __name__ == "__main__":
    main()
