#!/usr/bin/env python3
"""Hackathon #2: export the PE-Core-S16 food gate to LiteRT and prove the verdicts.

WHAT. Meta PE-Core-S16-384 (Apache-2.0) won the gate bench (NEW_WORK.md #1).
This makes the file the server (and later the phone) runs:
  1. the head: refit from the cached embeddings, same protocol as #1, written
     to $ICARUS_GATE_DATA (untracked, like every model asset);
  2. LiteRT export, vision tower only, L2-normalised in the graph, NHWC, fp32;
  3. fp16 WEIGHTS (ai_edge_quantizer float casting): compute and I/O stay
     float32, so the server's LiteRT C API call does not change;
  4. VERDICT PARITY on every gold photo: PyTorch fp32 vs the fp16-weight
     .tflite, the same head. Flips must be 0 (the threshold is a cliff, so
     cosine alone is not evidence). Also reported: agreement with the cached
     embeddings the head was fitted on;
  5. COST: LiteRT interpreter, 1 thread, fresh process per model (fp32,
     fp16w, and the old MobileCLIP2 file): median / p95 ms and peak RSS.
     Mac numbers, relative to each other;
  6. the preprocess the server must copy (timm's own data config).

  ICARUS_GATE_DATA=<gold-set dir> python3 tools/hackathon/pe_export_gate.py
"""
import argparse, hashlib, json, os, resource, statistics, subprocess, sys, time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import pe_gate_bench as B          # protocol + paths (ICARUS_GATE_DATA)

NAME = "vit_pe_core_small_patch16_384.fb"
F32 = os.path.join(B.MODELS, "gate_pe_s16_f32_384.tflite")
F16 = os.path.join(B.MODELS, "gate_pe_s16_f16w_384.tflite")
OLD = os.path.join(B.MODELS, "gate_s0_f32_192.tflite")
HEAD = os.path.join(B.MODELS, "gate_head_pe_s16_384.json")
NPZ = os.path.join(B.GRID, "pe_s16_float16_v1.npz")
RECIPE = [{"regex": ".*", "operation": op, "algorithm_key": "float_casting",
           "op_config": {"weight_tensor_config": {"num_bits": 16, "symmetric": True,
                                                  "granularity": "CHANNELWISE", "dtype": "FLOAT"},
                         "compute_precision": "FLOAT", "explicit_dequantize": True,
                         "skip_checks": False, "min_weight_elements": 0}}
          for op in ("FULLY_CONNECTED", "CONV_2D")]


def sha(p):
    h = hashlib.sha256()
    with open(p, "rb") as f:
        for b in iter(lambda: f.read(1 << 20), b""):
            h.update(b)
    return h.hexdigest()


def prob(coef, b, v):
    import numpy as np
    z = b + float(np.dot(np.asarray(coef, dtype=np.float64), np.asarray(v, dtype=np.float64)))
    if z >= 0:
        return 1.0 / (1.0 + np.exp(-z))
    e = np.exp(z)
    return float(e / (1.0 + e))


def interp(path, threads=1):
    from ai_edge_litert.interpreter import Interpreter
    it = Interpreter(model_path=path, num_threads=threads)
    it.allocate_tensors()
    return it, it.get_input_details()[0], it.get_output_details()[0]


def run(it, i, o, x):
    it.set_tensor(i["index"], x)
    it.invoke()
    return it.get_tensor(o["index"]).ravel().copy()


def probe(path):
    """Fresh process: 1 thread, 3 warm-ups, 50 timed invokes on fixed random input."""
    import numpy as np
    it, i, o = interp(path, 1)
    x = np.random.default_rng(13).random(tuple(i["shape"]), dtype=np.float32)
    for _ in range(3):
        run(it, i, o, x)
    ts = []
    for _ in range(50):
        t0 = time.perf_counter(); run(it, i, o, x); ts.append(1000 * (time.perf_counter() - t0))
    rss = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    print(json.dumps({"model": os.path.basename(path), "input": [int(v) for v in i["shape"]],
                      "ms_median": statistics.median(ts), "ms_p95": sorted(ts)[int(0.95 * len(ts)) - 1],
                      "peak_rss_mib": rss / (1 << 20) if sys.platform == "darwin" else rss / 1024,
                      "bytes": os.path.getsize(path)}))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--probe")
    a = ap.parse_args()
    if a.probe:
        return probe(a.probe)
    import numpy as np, torch, timm, litert_torch
    from PIL import Image
    from ai_edge_quantizer import quantizer as Q
    for p in (B.LABELS, B.SPLIT, NPZ):
        if not os.path.exists(p):
            B.die("missing %s" % p)
    bar = B.Bar(7)

    bar.step("head")
    truth, (dev_ids, hel_ids) = B.load_truth(), B.load_split()
    r = B.fit_cell(NPZ, truth, dev_ids, hel_ids)
    head = {"schema": 1, "encoder": "PE-Core-S16-384/timm:" + NAME, "precision": "float16w",
            "input_side": 384, "embedding_dim": len(r["coef"]), "normalised_input": True,
            "threshold": r["thr"], "intercept": r["intercept"], "coef": r["coef"],
            "accepted_on": {"protocol": "OOF sweep on dev (n=%d), applied once to heldout (n=%d)" % (r["n_dev"], r["n_hel"]),
                            "heldout_catch": r["hel_catch"], "heldout_false_retake": r["hel_frt"],
                            "heldout_auroc": r["auroc"], "ceiling_false_retake": B.CEILING,
                            "oracle_catch_at_ceiling": r["oracle_catch"]}}
    json.dump(head, open(HEAD, "w"), indent=1)
    bar.line("head: dim %d, thr %.3f, heldout catch %.3f, false retake %.3f" % (
        head["embedding_dim"], r["thr"], r["hel_catch"], r["hel_frt"]))

    bar.step("encoder")
    net, tf, cfg, params = B.build(NAME)
    net = net.float().eval()
    side = int(cfg["input_size"][-1])
    pre = {k: (list(v) if isinstance(v, tuple) else v) for k, v in cfg.items()}
    bar.line("preprocess (timm): %s" % json.dumps(pre))

    bar.step("export fp32")
    sample = torch.randn(1, 3, side, side)
    nhwc = litert_torch.to_channel_last_io(net, args=[0])
    litert_torch.convert(nhwc, (sample.permute(0, 2, 3, 1),)).export(F32)
    bar.line("fp32: %.1f MB" % (os.path.getsize(F32) / 1e6))

    bar.step("fp16 weights")
    q = Q.Quantizer(F32, RECIPE)
    q.quantize(serialize_to_path=F16, enable_progress_report=False)
    bar.line("fp16w: %.1f MB" % (os.path.getsize(F16) / 1e6))

    bar.step("verdict parity")
    it, i, o = interp(F16, 4)
    z = np.load(NPZ, allow_pickle=True)
    cached = {str(s): e for s, e in zip(z["sample_ids"], z["embeddings"])}
    coef, b, thr = head["coef"], head["intercept"], head["threshold"]
    flips, flips_fit, near, worst_cos, worst_dp, n = [], 0, 0, 1.0, 0.0, 0
    sids = B.sample_ids()
    t0 = time.time()
    for k, s in enumerate(sids):
        try:
            x = tf(Image.open(os.path.join(B.IMAGES, s + ".jpg")).convert("RGB")).unsqueeze(0)
        except Exception:
            continue
        with torch.no_grad():
            tv = net(x).numpy().ravel()
        ev = run(it, i, o, x.permute(0, 2, 3, 1).numpy().astype(np.float32))
        cos = float(np.dot(tv, ev) / (np.linalg.norm(tv) * np.linalg.norm(ev) + 1e-12))
        pt, pe = prob(coef, b, tv), prob(coef, b, ev)
        worst_cos, worst_dp, n = min(worst_cos, cos), max(worst_dp, abs(pt - pe)), n + 1
        if (pt > thr) != (pe > thr):
            flips.append((s, pt, pe, cos))
        if s in cached and (prob(coef, b, cached[s]) > thr) != (pe > thr):
            flips_fit += 1
        if abs(pt - thr) < 0.01:
            near += 1
        if k % 25 == 0:
            bar.draw("verdict parity", "%d/%d %.0f/s flips=%d" % (k + 1, len(sids), (k + 1) / max(time.time() - t0, 1e-9), len(flips)))

    bar.step("cost probes")
    probes = []
    for p in (OLD, F32, F16):
        if not os.path.exists(p):
            bar.line("no %s, skipped" % os.path.basename(p)); continue
        pr = subprocess.run([sys.executable, __file__, "--probe", p], capture_output=True, text=True)
        try:
            probes.append(json.loads(pr.stdout.strip().splitlines()[-1]))
        except Exception:
            bar.line("probe %s failed: %s" % (os.path.basename(p), (pr.stderr or "")[-300:]))

    bar.step("report")
    stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    man = {"encoder": head["encoder"], "params": params, "license": "Apache-2.0",
           "input_side": side, "input_layout": "NHWC", "normalised_in_graph": True, "preprocess_timm": pre,
           "tflite_fp32": {"file": os.path.basename(F32), "bytes": os.path.getsize(F32), "sha256": sha(F32)},
           "tflite_fp16w": {"file": os.path.basename(F16), "bytes": os.path.getsize(F16), "sha256": sha(F16)},
           "head": {"file": os.path.basename(HEAD), "sha256": sha(HEAD), "threshold": thr,
                    "embedding_dim": head["embedding_dim"], "heldout": head["accepted_on"]},
           "parity_fp16w_vs_torch_fp32": {"images": n, "verdict_flips": len(flips), "min_cosine": worst_cos,
                                          "max_prob_delta": worst_dp, "within_0.01_of_threshold": near,
                                          "flips_vs_fitted_embeddings": flips_fit},
           "cost_litert_1thread_mac": probes}
    L = ["", "=== PE-Core-S16 gate export ===",
         "params %.1fM | fp32 %.1f MB | fp16w %.1f MB | input %dpx NHWC" % (params / 1e6, man["tflite_fp32"]["bytes"] / 1e6, man["tflite_fp16w"]["bytes"] / 1e6, side),
         "head: thr %.3f | heldout catch %.3f | false retake %.3f | AUROC %.4f" % (thr, r["hel_catch"], r["hel_frt"], r["auroc"]),
         "", "--- verdict parity: PyTorch fp32 vs fp16w .tflite, %d photos ---" % n,
         "verdict flips          : %d" % len(flips),
         "min cosine             : %.8f" % worst_cos,
         "max probability delta  : %.3e" % worst_dp,
         "within 0.01 of thr     : %d" % near,
         "flips vs fitted (fp16 MPS) embeddings: %d" % flips_fit]
    for s, pt, pe, c in flips[:10]:
        L.append("  flip %s  %.5f -> %.5f  cos %.6f" % (s, pt, pe, c))
    L += ["", "--- cost: LiteRT interpreter, 1 thread, Mac (relative) ---",
          "%-30s %8s %8s %8s %8s" % ("model", "MB", "med ms", "p95 ms", "peakMiB")]
    for p in probes:
        L.append("%-30s %8.1f %8.1f %8.1f %8.0f" % (p["model"], p["bytes"] / 1e6, p["ms_median"], p["ms_p95"], p["peak_rss_mib"]))
    L += ["", "PASS: 0 flips." if not flips else "NOT PASSED: %d flips (see list)." % len(flips)]
    txt = "\n".join(L)
    print(txt)
    open(os.path.join(HERE, "results", "export_%s.txt" % stamp), "w").write(txt + "\n")
    json.dump(man, open(os.path.join(HERE, "results", "export_%s.json" % stamp), "w"), indent=1)
    print("\nsaved: results/export_%s.{txt,json}  (models + head in $ICARUS_GATE_DATA, untracked)" % stamp)


if __name__ == "__main__":
    main()
