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

---

## 4. Multi-item scan: inference_engine (SAM 3.1 + Muse Spark)

**When:** Oct 26 2026, 3:55 PM ET (first live probe) to Oct 26, 7:52 PM ET (last SAM test); Go change committed Oct 27.
**Meta models:** SAM 3.1 (`sam-3.1`), Muse Spark 1.3 (`muse-spark-1.3`), both on the Meta Model API; PE-Core-S16-384 food gate (#3) in front.
**Files:** `inference/` (new Go module: engine, agent, prepare, pan table, SAM parser and one_bit decoder, 35 tests), `inference/cmd/{ie-probe,ie-tokens,sam-size}`, `tools/hackathon/gen_sam_goldens.py`, `tools/hackathon/results/inference/*`, `docs/inference_engine_design.md`, `docs/sources/X30.md`
**Sources:** `docs/sources/X30.md` (every decision D1–D23 with its sources).

One photo in, every food item out, each in analyze's strict 8-field contract (imported from `server`, so the two cannot drift). A photo of **one** item gives exactly analyze's answer (same prompt, same bytes). A photo of several gives one listing per product, counted.

**Pipeline.** Food gate → SAM 3.1 with 3 concepts **at the same time** ("food", "package", "food tray") while the photo is resized for Muse → merge (a picture on a package joins its package; a serving board under pans is dropped, the pans are the items) → 0 boxes: Muse agent with SAM as its tool; 1 box: analyze's call; 2+: one Muse call (3 in parallel from 12 boxes) with **numbered marks drawn on the photo** and boxes on Muse's 0–1000 grid → group identical products → count.

**Measured (5 live runs, 23 photos, $2.83 in total; hand check of 294 boxes):**

| Result | Value |
|---|---|
| Correct product (marks + coordinates, effort low) | **89.8%** |
| Correct when the box is ≥ 96 px in the image Muse reads | **97.4%** |
| Cost per photo / per named item | **$0.022 / $0.0012** (analyze today: $0.005–0.007 per item, one item per photo) |
| Wait per 24-item photo, 1 call → 3 parallel calls | 14.9 s → **6.1 s** |
| Pictures on packages listed as food (shelf 6) | 10 of 11 → **0 of 8** (package concept) |
| Rows of identical boxes | Cheerios ×15, Honey Maid ×25 |

**What the answer carries for the app:** every box on the 0–1000 grid (÷10 = CSS %), a "check this" mark for boxes of 64–96 px, "take a closer photo" when boxes are too small to name, "not listed" for non-food products (never deleted), and trays as one listing each with servings from a hotel-pan chart (the user can change them).

**Measured and rejected:** per-crop calls (most expensive, invented items), two passes (more cost, 79.9%), effort `minimal` (merged different products), marks without coordinates (84.7%).

Next: X30b real phone photos (decides what SAM is sent and the app's upload size), then X31 the `/v1/inventory` service.

---

## 5. Real phone photos: upright photos, a "not food" answer, parallel tests

**When:** Oct 26 2026, 6:54 PM ET (photos taken) to Oct 26, 9:39 PM ET (run); committed Oct 27.
**Meta models:** SAM 3.1, Muse Spark 1.3 (Meta Model API); PE-Core-S16-384 gate in front.
**Files:** `inference/orient.go` (new), `inference/engine.go`, `inference/prepare.go`, `inference/twopass.go`, `inference/cmd/ie-probe/main.go`, tests (35 → 41), `tools/hackathon/results/inference/probe_x30b_20261027T013947Z.json`, `tools/hackathon/results/inference/x30b_hand_check.json`, `docs/inference_engine_design.md` (D24–D28)
**Sources:** `docs/sources/X30b.md`

20 photos taken in a grocery store with an Android phone (rows of frozen pizza and shrimp, deli tubs, salad bowls, meat trays, loose and bagged produce, pet food, cleaning products). Each photo ran twice: the camera original, and the version the app uploads today (1600 px, JPEG 0.85).

| Result | Camera original | App upload today |
|---|---|---|
| Correct product, readable boxes (hand check) | **297/297 = 100.0%** | 274/283 = 96.8% |
| Bananas counted | **71** | 1 |
| Salad bowls with small labels (photo 185725) | 19 correct, 0 wrong | 13 correct, 6 wrong |
| Cost / wait per photo | $0.047 / 13.4 s | $0.037 / 9.1 s |

**Changes from what the photos showed:**
- **Upright first.** 11 of 20 photos were stored sideways or upside down, with the turn in the EXIF orientation tag (iOS and Android write the same values 1–8). Go ignores the tag, so the engine now reads it and turns every photo upright before SAM, the marks and the size levels.
- **"Not food" is an answer.** A photo with only pet food gave 24 correct "not food" boxes but ended in an error; it now answers `not_food` with the boxes, so the app can show them with "Add anyway". Pet food is named in the prompt as not food.
- **Parallel tests.** The probe runs 4 photos at the same time (`-parallel 1` to measure the wait); a shared SAM answer is paid once even when runs overlap. The SUP template (v3) runs its Go, Dockerfile, Python and mobile checks at the same time.

Next (X32): the app uploads at Muse's limit, about 2376 px at JPEG 0.9 (it costs about $0.01 more per photo and keeps the counts and the small labels).

---

## 6. Accuracy numbers traced to per-box labels

**When:** Oct 27 2026. **Sources:** `docs/sources/X30c.md`
**Files:** `tools/hackathon/score_labels.py`, `tools/hackathon/results/inference/{x30b_labels.json, score_x30b.json, score_v3.json}` (`x30b_hand_check.json` removed), `docs/inference_engine_design.md`, `NEW_WORK.md` #5

Entry #5 first gave the hand check as counts per photo, and some counts included boxes the reviewer could not read. It is replaced by one label per box (correct, wrong or unreadable, with the box and the names), and a script that computes every total. The corrected numbers in #5: camera originals **297/297 = 100.0%** of the readable boxes (60 unreadable), app upload **274/283 = 96.8%** (44 unreadable). The v3 numbers (89.8%, 97.4% at ≥ 96 px) are reproduced exactly by the same script from their committed labels. The SUP now stops if a changed document states an accuracy share that no committed score output contains.

---

## 7. The multi-item scan as a service: `/v1/inventory`

**When:** Oct 26 2026, 11:24 PM ET (code and tests); committed Oct 27.
**Meta models:** SAM 3.1, Muse Spark 1.3 (Meta Model API); PE-Core-S16-384 as the only food gate.
**Files:** `inference/service.go`, `inference/events.go`, `inference/cmd/inventory/main.go`, `inference/Dockerfile` (+ `.dockerignore`), `inference/service_test.go`, `router/inventory.go`, `router/inventory_test.go`, `router/router.go`, `router/events.go`, `router/cmd/router/main.go`, `server/handler.go` (an exported reader; `/v1/analyze` unchanged), `deploy/docker-compose.yml`, `deploy/Caddyfile`, `tools/morpheus_stack.sh` (folders from outside; PE assets; inventory checks), `docs/inference_engine_design.md` (D31–D38)
**Sources:** `docs/sources/X31.md`

The X30 engine is now a service. One photo in; every food item in it out, with its boxes, count and cost.

- **Two doors.** `POST /v1/inventory` with the bearer token (Postman, judges), and `POST /api/inventory` for the signed-in app. The app's door stores the photo and marks every item that the user **already has as a live listing** ("already listed"); it removes nothing.
- **The gate first.** PE-Core-S16-384 (Meta) is now the only food gate. A non-food photo costs $0; a gate that is down refuses the photo.
- **Spend and load are bounded.** 6 requests per minute per client, 3 photos at the same time, and a **$20 per day** spend cap that survives a restart.
- **Every request is logged** with each SAM and Muse call, its time, tokens and cost.
- Tests: inference 41 → 47, router 9 → 12.

Next: deploy to the Icarus box (`icarus.thehungreeapp.com`), then X32 (the app's review screen and the upload at Muse's limit).
