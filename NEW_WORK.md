# Icarus — work built during the Meta Global AI Developer Hackathon

This file is the record of what is **new** in the Hackathon. Judges can see every new line with one compare:

    https://github.com/TwoThousandAndTwo2002/Icarus/compare/hackathon-start...hackathon/meta-2026

- **`main` at tag `hackathon-start`** is the base: the food-rescue app and its services as they were **before** the Hackathon. It is a scrubbed snapshot of our private prior product (commit `79c7d69`, built Oct 21–26 2026). It is not new work.
- **Branch `hackathon/meta-2026`** holds only work started at or after the kickoff (Oct 26 2026, 12:00 PM ET). Each entry below names the commit, the date and time (ET), and the Meta model it uses.

Times: the kickoff was Oct 26, 12:00 PM ET; the building period is Oct 27 – Nov 4.

---

## 1. A Meta model for the food gate (PE-Core), measured against the old gate

**When:** Oct 26 2026, 2:01 PM ET (bench run). After the kickoff, before the building period started. Committed Oct 26.
**Meta model:** Perception Encoder, `PE-Core-S16-384` (Apache-2.0).
**Files:** `tools/hackathon/pe_gate_bench.py`, `tools/hackathon/results/bench_20261026T180111Z.{txt,json}`

**Why.** The base gate (food / not food, before any cloud call) used MobileCLIP2-S0. Its weights carry Apple's research-only licence (`apple-amlr`: "does not include … use in any commercial product or service"), so it cannot ship in a product. We looked for a Meta model of about the same size.

**How.** The same protocol as the base gate, on the same 1,432 gold-labelled photos: a new logistic head for each encoder, 5-fold out-of-fold on DEV (573), threshold = the highest with false retake ≤ 3%, applied once to HELDOUT (859). Preprocessing comes from timm's own data config.

**Result (HELDOUT):**

| Model | Size | AUROC | Catch (not-food refused) | False retake (food refused) | Oracle catch | Pass |
|---|---|---|---|---|---|---|
| MobileCLIP2-S0 fp32 @192 (base) | 46.2 MB | 0.970 | 0.817 | 2.9% | 0.817 | bar |
| **PE-Core-S16-384 fp16** | **47.2 MB** | **0.980** | **0.839** | **2.3%** | **0.863** | **yes** |
| PE-Core-T16-384 fp32 | 24.2 MB | 0.973 | 0.758 | 1.2% | 0.798 | no (catch) |

PE-Core-S16 refuses more non-food and less real food than the old gate, at the same file size. Next: export to LiteRT, verdict parity (0 flips), then shadow mode on the server.

---

## 2. The PE-Core gate as a LiteRT file, with verdict parity

**When:** Oct 26 2026, 2:30 PM ET (export run). Committed Oct 26.
**Meta model:** Perception Encoder `PE-Core-S16-384` (Apache-2.0).
**Files:** `tools/hackathon/pe_export_gate.py`, `tools/hackathon/results/export_20261026T183051Z.{txt,json}`

**What.** The file the server (and later the phone) runs: the vision tower only, L2-normalised inside the graph, NHWC input at 384 px, exported with `litert_torch`, then **fp16 weights** with `ai_edge_quantizer` float casting. Compute, input and output stay float32, so the server's LiteRT C API call does not change. The head is refitted with the protocol of entry 1 (threshold 0.287). Model and head stay out of the repo, as the old gate assets did.

**Verdict parity (all 1,432 gold photos, PyTorch fp32 vs the fp16-weight `.tflite`, same head):**

| Check | Result |
|---|---|
| Verdict flips | **0** |
| Minimum cosine | 0.999994 |
| Largest probability change | 0.0016 |
| Photos within 0.01 of the threshold | 6 |

**Cost (LiteRT interpreter, 1 thread, Mac; relative only):**

| Model | File | Median | p95 | Peak memory |
|---|---|---|---|---|
| MobileCLIP2-S0 fp32 @192 (old) | 46.2 MB | 40 ms | 51 ms | 147 MiB |
| PE-Core-S16 fp32 @384 | 94.4 MB | 95 ms | 110 ms | 257 MiB |
| **PE-Core-S16 fp16 weights @384** | **48.1 MB** | **96 ms** | **112 ms** | **242 MiB** |

Same file size as the old gate; 2.4× the compute per photo (384 px input). Next: the server gate runs this file in shadow mode next to the old one, with the PE preprocess (timm config in the export JSON).

---

## 3. One preprocess for server and phone, and the PE gate in shadow mode on the server

**When:** Oct 26 2026, 2:57 PM ET (deploy-head run); Go change committed Oct 26.
**Meta model:** Perception Encoder `PE-Core-S16-384` (Apache-2.0).
**Files:** `tools/hackathon/pe_gate_deploy_head.py`, `tools/hackathon/results/deploy_head_20261026T185715Z.{txt,json}`, `gate/preprocess.go`, `gate/head.go`, `gate/handler.go`, `gate/events.go`, `gate/cmd/gate/main.go`, `gate/gate_test.go`

**One preprocess.** PE-Core was trained with timm's preprocess (resize, then crop; bicubic). The server crops the centre square first and then resizes, as the phone does, and Go must equal Python bit for bit. So the deployed preprocess is fixed as: crop-first centre square → Pillow **bicubic** to 384 → (x/255 − 0.5)/0.5, NHWC. The head is refitted on embeddings from the **deployed fp16-weight `.tflite`** with exactly that preprocess:

| Head (HELDOUT) | AUROC | Catch | False retake |
|---|---|---|---|
| Old gate (MobileCLIP2-S0) | 0.970 | 0.817 | 2.9% |
| PE, timm preprocess (#1, #2) | 0.980 | 0.839 | 2.3% |
| **PE, deployed preprocess (this)** | **0.981** | **0.831** | **1.6%** |

**Go gate (server).**
- A Pillow-exact **bicubic** resampler next to the bilinear one (same fixed-point code, cubic kernel a = −0.5, support 2). Bit-exact with Pillow on the 4 golden cases, including 640×480 → 384.
- The head JSON may declare its `preprocess`; a head without one keeps the old tensor bit for bit (tested).
- **Shadow mode:** a second gate (`-shadow-model`, `-shadow-head`, both pinned by sha256) scores the same decoded photo **after** the answer is sent, on its own slot. It never changes the answer or its latency. It writes its own event (`step: gate_shadow`) with the caller's verdict, so the two gates can be compared photo by photo. A busy shadow skips and counts; a shadow that fails to load is reported in `/healthz` and never runs.
- Tests: gate 13 → **19** (bicubic goldens, old-tensor identity, PE tensor, preprocess validation, shadow cannot change the answer, shadow failure is ignored); all pass with the race detector.

Next: run the shadow on the new box with live photos, then switch the router to the PE gate and remove MobileCLIP2.
