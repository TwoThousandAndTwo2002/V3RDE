# V3RDE — work built during the Meta Global AI Developer Hackathon

This file is the record of what is **new** in the Hackathon. Every commit in this repository was made at or after the kickoff (Oct 26 2026, 12:00 PM ET). Each entry below names the date, the Meta model it uses, its files and its sources.

- **Entries 1–23** were first built during the Hackathon in our earlier repository (Icarus). They were moved here with their commit history (`git filter-repo`, only the new-work folders), so their dates and messages are the original ones. Some of them name files of that repository that are not here yet (for example `tools/morpheus_stack.sh`).
- **From entry 24**, the parts of the app that existed **before** the Hackathon (the analyze server, auth, listings, barcode, telemetry, the food-gate service and the deploy files) are written again here as new code. Each of these entries says which earlier part it replaces and what it keeps the same (for example an API contract that the app already uses). The features of those parts are not new; the code is.
- Branches: `services` (the services) and `ui` (the app) merge into `main`.

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

---

## 8. A clear time limit, and a measured choice of Muse variant

**When:** Oct 27 2026 (bench 05:08 UTC; code the same day).
**Meta models:** SAM 3.1, Muse Spark 1.3 (Meta Model API); PE-Core-S16-384 gate.
**Files:** `inference/cmd/inventory/main.go`, `inference/service.go`, `inference/service_test.go`, `inference/engine.go` (the `compact` option, off), `inference/twopass_test.go`, `inference/cmd/ie-probe/main.go` (`:compact`), `router/router.go`, `deploy/Caddyfile`, `tools/hackathon/bench_x31b.py`, `tools/hackathon/results/inference/{probe_x31b_20261027T050833Z.json, score_x31b_bench.json}`, `docs/inference_engine_design.md` (D39–D42)
**Sources:** `docs/sources/X31c.md`

The first live photos through `icarus.thehungreeapp.com` took about 27 s. The box's own records showed where: the Muse calls, not the server.

- **A clear limit.** A photo now stops at 55 s with a clear message, under the load balancer's 60 s.
- **A measured choice.** Four ways to call Muse were run on the same 20 phone photos. More parallel calls cut the wait only a little (10.4 s → 9.7 s median) and cost 47% more, because each call reasons again. The live variant stays.
- Tests: inference 47 → 49.

Next: the app shows the boxes first and the names after (X32); merge near-duplicate names in one photo; retrain the gate with shelf and pet-product photos.

---

## 9. One route for every photo: barcode, gate, every item, one answer

**When:** Oct 27 2026.
**Meta models:** PE-Core-S16-384 food gate; SAM 3.1; Muse Spark 1.3 (Meta Model API).
**Files:** `inference/service.go`, `inference/service_test.go`, `inference/clients.go`, `inference/photos.go`, `inference/nofood.go`, `inference/nofood_test.go`, `inference/testdata/x32_muse_names.json`, `inference/events.go`, `inference/engine.go`, `inference/agent.go`, `inference/prepare_test.go`, `inference/cmd/inventory/main.go`, `listings/listings.go`, `listings/listings_test.go`, `listings/cmd/listings/main.go`, `deploy/Caddyfile`, `deploy/docker-compose.yml`, `tools/morpheus_stack.sh`, `docs/morpheus/analyze_api.md`; `router/` removed
**Sources:** `docs/sources/X32a.md`

A photo with no food came back as "200, No food", and the app had three routes for one job. Now there is one: `POST /v1/analyze`.

- **One flow.** The phone's barcode first (catalog hit = fast path); then the Meta food gate (no food = 422, no model call); then SAM 3.1 counts every item and Muse Spark names them.
- **One answer shape** for one item and for many: `items[]`, each with the eight listing fields unchanged and the count of identical items as the servings.
- **Clear refusals.** Every error has a fixed `code`: `not_food`, `model_no_food_single`, `model_no_food_multi`, `already_listed` (409), `rate_limited`, `busy`, `daily_cap_reached`, …
- **Item photos.** The server cuts a crop for each item; the app lists it with `photo_id`, and only its owner can read it.
- **Nothing lost.** Every SAM box goes in the ledger with what became of it, for the gate and naming training later.
- Tests: inference 49 → 59, listings 10 → 11; the router module (12) is removed. The old race-test setup in `prepare_test.go` now calls `defaults()` once, as the service does (`-race` found it).

Next: the app's multi-item review and posting screens (X32b, mockup approved), then one deploy.

---

## 10. The app on the one route: every item in one review list

**When:** Oct 27 2026.
**Meta models:** PE-Core-S16-384 food gate; SAM 3.1; Muse Spark 1.3 (through `/api/analyze`).
**Files:** `mobile/src/screens/Capture.tsx`, `mobile/src/screens/Give.tsx`, `mobile/src/api.ts`, `mobile/src/logic.ts`, `mobile/src/photo.ts`, `mobile/src/theme.ts`, `mobile/tests/logic.test.mts`, `mobile/web-dist/`
**Sources:** `docs/sources/X32b.md`

The app now sends every photo to the one route and shows what it found.

- **One item:** the form as before; the listing uses the server's photo.
- **Several items:** one card per item (the server's crop, the title, the servings = the count, remove), one pickup for all, then "Post N listings". A failed item stays on the list with its error.
- **A clear sheet for each answer:** not food, no food the scanner can name (one item or several), already listed, busy, too many photos, daily limit.
- **Sharper input:** photos go up at about 2376 px, JPEG 0.9 (D28).
- Checked on the real app export in Chromium with a fake camera and recorded answers (not only the mockup). Tests: mobile 21 → 24.

Next: one deploy of X32a + X32b, then the live test on the web app and the APK.

---

## 11. Single items first, and every photo under the 30 s gateway limit

**When:** Oct 27 2026.
**Meta models:** SAM 3.1, Muse Spark 1.3 (Meta Model API); PE-Core-S16-384 gate.
**Files:** `inference/engine.go`, `inference/engine_test.go`, `inference/service.go`, `inference/cmd/inventory/main.go`, `inference/cmd/ie-probe/main.go` (`:early`), `tools/hackathon/compare_x32c.py`, `tools/hackathon/results/inference/{compare_x32c.json, probe_x32c_live_20261027T190540Z.json, probe_x32c_early_20261027T191106Z.json}`, `docs/inference_engine_design.md` (D57, D58), `docs/morpheus/analyze_api.md`
**Sources:** `docs/sources/X32c.md`

The first live test showed two problems. A single bottle held close gave SAM no box, so the slow agent ran (35–41 s). And the public name is behind API Gateway, which stops every request at 30 s: the app showed "busy" while the server still answered 200.

- **Single items first.** The whole-photo call (the original analyze request) now starts at the same time as SAM. With 0 or 1 box its answer is the answer, with no agent. Bench, 24 photos: photos with no SAM box 33.0 s → 2.7 s (median); one box 9.9 s → 6.0 s; multi-item photos unchanged (14.0 s → 14.1 s), plus the unused call's cost.
- **A clear limit.** A photo stops at 25 s with the service's own `timeout` answer, under the gateway's 30 s.
- Tests: inference 59 → 60.

Next: every photo under 20 s; first cut about 5 s from the multi-item photos.

---

## 12. The web camera at full size

**When:** Oct 27 2026.
**Files:** `mobile/src/webcam.ts` (new), `mobile/src/screens/Capture.tsx`, `mobile/src/logic.ts`, `mobile/tests/logic.test.mts`, `mobile/web-dist/`
**Sources:** `docs/sources/X32d.md`

The first live test from a phone browser showed that the web app sent every photo at 480x640: the camera library opens the camera with no size and copies the preview frame. On a counter with 7 food items, SAM got a 640x480 frame and the answer named 3.

- After the camera opens, the app asks for the camera's largest size; at the shutter it takes a real still photo where the browser can (Chrome for Android, Safari on iOS 18.4+), else the larger video frame. Then the upload is about 2376 px at JPEG 0.9, as planned in #5.
- Checked on the real web export in Chromium with a fake camera: the upload went from 640x480 to 2376x1337. Tests: mobile 24 -> 26.

Next: the same counter and single items from the phone, then the parity bench against the original single call.

---

## 13. How each app photo was taken, in the ledger

**When:** Oct 27 2026.
**Files:** `mobile/src/webcam.ts`, `mobile/src/screens/Capture.tsx`, `mobile/src/api.ts`, `inference/events.go`, `inference/service.go`, `inference/service_test.go`, `mobile/web-dist/`
**Sources:** `docs/sources/X32e.md`

Two reports from the phone test: a delay between the camera button and the shot, and once an older photo of the previous item. Before any fix, the app now sends how each photo was taken, and the ledger keeps it next to the photo's sha: what started it (shutter or barcode), a real still or the preview frame, the time from the press to the photo, the time to prepare the upload, and the preview video's time and state at the press. A preview whose time does not move between two presses was frozen.

- The service logs the field as sent (one small JSON object) and never uses it for the answer.
- Tests: inference 60 -> 61.

Next: read the field on the next phone test, then fix the shutter.

---

## 14. The shutter answers at once

**When:** Oct 27 2026.
**Files:** `mobile/src/screens/Capture.tsx`, `mobile/src/webcam.ts`, `mobile/src/photo.ts`, `mobile/src/logic.ts`, `mobile/src/theme.ts` (`press.scale`), `mobile/tests/logic.test.mts`, `mobile/web-dist/`
**Sources:** `docs/sources/X32f.md`

The capture log from the phone showed why the shutter felt broken: the photo itself took 0.8 s, then resizing the 12 MP photo took 10.4 s before anything changed on screen, and every press in that time was ignored.

- The press shows at once: the shutter goes down and the preview stops on the frame the user saw. The "Use this photo?" sheet opens as soon as the photo exists.
- The resize runs once, after "Use photo", inside the Analyzing timer (one decode, one encode).
- The preview runs at the upload size, not the sensor size, so the camera and the barcode reader stay light.
- Checked on the real web export in Chromium: sheet 0.6–1.1 s after the press; resize 0.33 s; upload 2376x1337.

---

## 15. The user's corrections, in the ledger

**When:** Oct 27 2026.
**Files:** `inference/labels.go` (new), `inference/labels_test.go` (new), `inference/events.go`, `inference/service.go`, `mobile/src/screens/Capture.tsx`, `mobile/src/logic.ts`, `mobile/src/api.ts`, `mobile/tests/logic.test.mts`, `docs/morpheus/analyze_api.md`, `mobile/web-dist/`
**Sources:** `docs/sources/X32g.md`

The pipeline has thresholds set by hand. To tune them on real use, the ledger needs the truth for each answer, and the user gives it in the app: they rename an item, change its servings, remove it, or start over.

- After the user acts on an answer, the app sends one report: every item with the model's title and servings and the user's final ones, kept or removed, posted or not, and the device's language (for the translated app later).
- The service logs it next to the answer (same request_id). It never changes a listing.
- Tests: inference 61 -> 62, mobile 26 -> 28.

Next: the barcode front-of-package image; then a replay bench that tunes the thresholds on labelled photos.

---

## 16. Photo touch-up, checked by a second model

**When:** Oct 28 2026.
**Meta models:** Muse Image 1.0, Muse Spark 1.3 (Meta Model API).
**Files:** `post/` (new module: `post.go`, `touchup.go`, `cmd/touchup/main.go`, `Dockerfile`, tests), `docs/morpheus/post_api.md` (new)
**Sources:** `docs/sources/X35a.md`

A donor's photo is often dark or cluttered. The new touch-up service makes it look better for the post, and it never changes the food.

- Muse Image 1.0 edits the photo in one of two fixed modes: light (exposure, white balance, sharpness) or background (the same, and a plain surface behind the food). The user never writes a prompt.
- Muse Spark 1.3 then compares the original and the edit. The edit is offered only when every item, label and count is the same. In all other cases the original stays.
- It runs as a job (202, then poll), because the public gateway stops a request at 30 s.
- The edit is stored like an item photo, so a listing can use it later. Only its owner can read it.
- Its own events file and daily cap ($2 by default).
- Built, not deployed: no compose or Caddy route yet.
- Tests: post 16 (new module).

Next: the rescue service for listings that nobody claimed.

---

## 17. Rescue: food that nobody claimed goes to a food program

**When:** Oct 28 2026.
**Meta model:** Muse Spark 1.3 (Meta Model API).
**Files:** `post/rescue.go`, `post/rescue_test.go`, `post/cmd/rescue/main.go` (new), `docs/morpheus/post_api.md`
**Sources:** `docs/sources/X35b.md`

When a listing's timer runs out and nobody claimed it, the donor can still give the food away.

- Muse Spark looks at the photo and decides: shelf-stable (cans, dry goods) or perishable. It writes a short inquiry that asks the program if it can accept the food and when the donor can drop it off. It does not say the food is safe.
- The Feed America directory gives the nearest places by ZIP or location: food banks and pantries for shelf-stable food, soup kitchens and meal programs for perishable food.
- The donor sees the draft and the places, and sends it from their own mail app (mailto) or calls (tel). A server mail relay is optional and off by default.
- Only the donor's own listing, still active, after its timer ran out. A bulk rule (servings) is a setting.
- Built, not deployed.
- One rescue per listing. If the relay is on, it sends only to the place's known address, with a time limit and a rate limit.
- Tests: post 16 -> 27.

Next: share a listing on the Icarus Facebook Page.

---

## 18. Share a listing on the Icarus Facebook Page

**When:** Oct 28 2026.
**Meta model:** Muse Spark 1.3 (Meta Model API); Facebook Pages API (Graph API v25.0).
**Files:** `post/fanout.go`, `post/fanout_test.go`, `post/cmd/fanout/main.go` (new), `docs/morpheus/post_api.md`
**Sources:** `docs/sources/X35c.md`

A donor can share a live listing with the neighborhood on the Icarus Facebook Page, and nobody makes a wasted trip.

- Muse Spark writes a short public post from the photo, the title and the servings. The post never has the pickup text, a link from the model, or a safety claim.
- The photo and the text go to the Page in one photo post, with a link to claim the food in the app.
- When the food is claimed, the post is edited to start with [CLAIMED]. When the timer runs out, a sweep can mark it [EXPIRED].
- A second Muse Spark call checks the text before it goes public. The listing title goes to the model as data, not as instructions.
- One post per listing, also when the donor taps twice. Only the owner can share it. Only the listings service can mark a post [CLAIMED], with its own internal token.
- Built, not deployed: no Page token yet.
- Tests: post 27 -> 35.

---

## 19. Voice posting: say what to change

**When:** Oct 28 2026.
**Meta models:** Muse Voice Transcribe 1.0, Muse Spark 1.3 (Meta Model API).
**Files:** `post/voice.go`, `post/wav.go`, `post/voice_test.go`, `post/cmd/voice/main.go` (new), `post/third_party/GO_AUDIO_LICENSE.txt` (new), `post/go.mod`, `post/go.sum` (new), `post/Dockerfile`, `post/Dockerfile.dockerignore`, `docs/morpheus/post_api.md`
**Sources:** `docs/sources/X35d.md`

After a scan, the donor can speak instead of typing: "6 trays of pasta, the salad is gone, pick up at the back door before 5".

- Muse Voice Transcribe turns the recording into text. The item titles are sent as keywords, and the device language as a hint.
- Muse Spark maps the text to changes of the scanned items, in the listing contract only: title, servings, remove, and the pickup text (the directions). There is no description field.
- Code checks every change against the listing contract, with the limits imported from the listings service: only items that exist, servings 1 to 99, a title up to 80 bytes, a pickup text up to 200 bytes. A test runs every proposal through the listings service's own check. Any other field is dropped and listed. Voice cannot add food that the photo does not show.
- With no scan, voice makes a draft of new items, and each one still needs a photo.
- The app shows the changes, and nothing posts until the donor taps "Post". The ledger keeps the transcript, never the audio.
- The WAV reader is our own and does not trust the file header: every chunk must fit in the bytes received, and the length comes from the real samples. The WAV writer reuses go-audio/wav (Apache-2.0). The request shape follows Meta's own cookbook.
- Built, not deployed.
- Tests: post 35 -> 46 (and 329,094 fuzz runs of the WAV reader with no failure).

---

## 20. The voice clip is made on the phone

**When:** Oct 28 2026.
**Meta model:** Muse Voice Transcribe 1.0 (the format it takes).
**Files:** `mobile/src/wav.ts`, `mobile/src/webvoice.ts`, `mobile/src/voicerec.ts`, `mobile/public/voice-capture-worklet.js`, `mobile/tests/wav.test.mts` (new), `mobile/src/api.ts`, `mobile/app.json`, `mobile/package.json`, `mobile/package-lock.json`, `mobile/web-dist/`, `docs/morpheus/post_api.md`
**Sources:** `docs/sources/X35e.md`

The speech model takes one format: WAV, mono, 16-bit, 16 or 24 kHz. Normal recorders do not give it, so the app now makes it on the device and the server sends it on unchanged.

- Android and iOS: Expo's own audio module (expo-audio, the version the SDK pins) records 16 kHz mono 16-bit PCM into a WAV file.
- Web: Expo's audio stream does nothing on the web (we read its code), so the app records with the browser's Web Audio API and makes the WAV itself.
- Checked in Chromium with a fake microphone: a 440 Hz tone came out as a 16 kHz WAV with a pitch of 439.8 Hz, and the server's own WAV check sent it byte for byte.
- The app asks for the microphone with one clear text. It does not ask to run audio in the background.
- No UI yet: the mic button comes next, with its mockup.
- Tests: mobile 28 -> 31.

---

## 21. Review fixes before the post services go live

**When:** Oct 28 2026.
**Meta models:** Muse Image 1.0, Muse Spark 1.3, Muse Voice Transcribe 1.0 (no new model).
**Files:** `post/touchup.go`, `post/rescue.go`, `post/fanout.go`, `post/voice.go`, `post/post.go`, `post/cmd/touchup`, `post/cmd/fanout`, `post/cmd/voice`, `post/Dockerfile`, the post tests, `mobile/src/clipsession.ts` (new), `mobile/src/voicerec.ts`, `mobile/tests/clipsession.test.mts` (new), `mobile/package.json`, `mobile/web-dist/`, `docs/morpheus/post_api.md`
**Sources:** `docs/sources/X36a.md`

Two reviewers who did not write #16 to #20 read the code before deploy. They found 7 medium defects and some small ones. Each fix has a test.

- Touch-up takes only a photo from the scan (photo_id), from the signed-in donor. That photo has passed the food gate and is upright, so no other image can reach the paid calls. The donor starts it with a button.
- The rescue mail relay sends only to the address the food directory gives. With no such address, the donor uses their own mail app or the phone.
- Facebook sharing: one share makes one public post, also when the disk write fails, and a slow upload is not cut short. A claim during a share is retried, not lost.
- Voice: at most 2 recordings are processed at the same time, as the scan does, so the box keeps its memory. Speech cost rounds up to the whole second.
- The app keeps the clip when recording stops by itself at the time limit.
- Every events line has the caller's address, as the scan's does.
- Built, not deployed.
- Tests: post 46 -> 51, mobile 31 -> 39.

---

## 22. The post services join the stack

**When:** Oct 28 2026.
**Meta models:** Muse Image 1.0, Muse Spark 1.3, Muse Voice Transcribe 1.0 (no new model).
**Files:** `deploy/docker-compose.yml`, `deploy/Caddyfile`, `tools/morpheus_stack.sh`, `listings/listings.go`, `listings/hook.go` (new), `listings/hook_test.go` (new), `listings/cmd/listings/main.go`, `docs/morpheus/post_api.md`
**Sources:** `docs/sources/X36b.md`

Touch-up, rescue, Facebook sharing and voice posting now run in the Icarus stack, next to the scan.

- One image holds the four services; each runs with its own data, events, memory limit and telemetry sidecar.
- The app reaches them with its session: `/api/touchup`, `/api/rescue/…`, `/api/fanout/…`, `/api/voice`. Their internal routes have no public address.
- A listing can use a touched-up photo. Only its owner can use it.
- After a receiver grabs a listing, listings tells the sharing service, so the Facebook post can show [CLAIMED]. This uses its own secret, made on the box. Expired posts get [EXPIRED].
- Safe defaults: Facebook posting is off until the Page is connected, and the rescue mail relay is off (the donor's own mail app or phone).
- Not deployed by this change: the deploy script does it.
- Tests: listings 11 -> 15.

---

## 23. A deploy builds only what changed

**When:** Oct 28 2026.
**Meta models:** none (deploy tooling).
**Files:** `tools/morpheus_stack.sh`
**Sources:** `docs/sources/X36c.md`

Each image carries the commit it was built from. Because of that, every deploy compiled every image again, also when its code had not changed. On the box this used up to 1.6 GB of disk for the food gate alone, and the first deploy of #22 stopped on the disk check.

- The stack script can now start the stack with the images that are already built (`STACK_NO_BUILD=1`).
- The deploy builds only the services whose code changed, one at a time, and keeps the images and their build stamp for the others.
- Without the setting, the stack script works as before.

---

## 24. The analyze server, written new for V3RDE

**When:** Oct 28 2026.
**Meta model:** Muse Spark 1.3 (no new model).
**Files:** `server/contract.go`, `server/access.go`, `server/muse.go`, `server/handler.go`, `server/metrics.go`, `server/cmd/analyze/main.go`, `server/Dockerfile`, `server/go.mod`, `server/server_test.go` (all new)
**Sources:** `docs/sources/X38a.md`

The analyze server (`POST /v1/analyze`: one food photo in, the eight-field listing out) existed before the Hackathon. V3RDE does not copy it: this is new code. The inference and post services import it.

- It keeps the same contract, so the app and the inference service do not change: the same prompt (a test pins its sha256), the same eight fields, routes, status codes and error texts.
- Checked against the old server: its own tests pass on the new code, and a fuzz test of the contract code found no difference.
- The rate limit now removes idle addresses, so its memory does not grow until a restart.
- The inference service builds and passes its tests on it again. The post services also need listings (a later entry).
- Tests: server 26 (new), inference 62 (unchanged).
