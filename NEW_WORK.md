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

---

## 25. The listings service, written new for V3RDE

**When:** Oct 28 2026.
**Meta model:** none (the listings of the scan and the post services).
**Files:** `listings/listing.go`, `listings/store.go`, `listings/handler.go`, `listings/hook.go`, `listings/events.go`, `listings/cmd/listings/main.go`, `listings/Dockerfile`, `listings/go.mod`, `listings/listings_test.go` (all new)
**Sources:** `docs/sources/X38b.md`

The listings service (a donor posts a meal, a receiver grabs it) existed before the Hackathon. V3RDE does not copy it: this is new code. The post services import it.

- It keeps the routes, the status codes, the error texts, the JSON of a listing and the data file format, so the app and the data on the box do not change. It keeps the Hackathon parts too: a listing from a scan photo or a touched-up photo (entries 9 and 22), and the claim hook that marks the Facebook post [CLAIMED] (entry 22).
- Checked against the old service: its own tests pass on the new code, and an independent review compared the two request by request and the data file both ways.
- New: a grab that is not written changes nothing, so the listing can be grabbed when the disk is back. Photos are written through a temp file, so a crash cannot leave half a photo. Two listings in one second are in time order.
- The post services build and pass their tests again.
- Tests: listings 27 (new), post 51 (unchanged).

---

## 26. The sign-in service, written new for V3RDE

**When:** Oct 28 2026.
**Meta model:** none (sign-in for the app and the post services).
**Files:** `auth/auth.go`, `auth/password.go`, `auth/store.go`, `auth/handler.go`, `auth/events.go`, `auth/cmd/auth/main.go`, `auth/Dockerfile`, `auth/go.mod`, `auth/auth_test.go` (all new)
**Sources:** `docs/sources/X38c.md`

The sign-in service (email and password, a session cookie) existed before the Hackathon. V3RDE does not copy it: this is new code.

- It keeps the routes, the status codes, the error texts, the session cookie, the data file and the password hash format, so the accounts on the box keep working. A test logs in with a data file written by the old service.
- Checked against the old service: its own tests pass on the new code, and an independent security review sent the same requests to both.
- New, from that review: a damaged stored password hash can never match, cannot make a login slow, and takes the same time as a real check. The rate limit removes idle addresses.
- Open: the rate-limit address comes from a header that a caller can set; the deploy change will fix it in Caddy for every service.
- Tests: auth 22 (new).

---

## 27. The barcode lookup, written new for V3RDE

**When:** Oct 28 2026.
**Meta model:** none (a scanned barcode fills the listing without a model call).
**Files:** `barcode/gtin.go`, `barcode/product.go`, `barcode/store.go`, `barcode/store_sqlite.go`, `barcode/open_sqlite.go`, `barcode/open_stub.go`, `barcode/handler.go`, `barcode/events.go`, `barcode/cmd/barcode/main.go`, `barcode/Dockerfile`, `barcode/go.mod`, `barcode/go.sum`, and the tests (all new)
**Sources:** `docs/sources/X38d.md`

The barcode lookup (a scanned code in, the catalog's product out) existed before the Hackathon. V3RDE does not copy it: this is new code. The inference service calls it first for every photo with a barcode.

- It keeps the answer that the inference service reads, the four outcomes (found, not in the catalog, not a valid code, catalog not loaded), the events line and the catalog check: the catalog file must have the pinned sha256 and row count, else every lookup answers "not loaded" (it fails closed).
- Checked against the old service: its own tests pass on the new code, and an independent review sent 39,002 codes to both with no difference.
- New: a too-short catalog pin is an error, not a crash.
- Tests: barcode 20 (new), and 2 SQLite tests that run in the image build.

---

## 28. Three post tests no longer depend on the date

**When:** Oct 29 2026.
**Meta model:** none (tests only).
**Files:** `post/post_test.go`, `post/fanout_test.go`, `post/rescue_test.go`
**Sources:** `docs/sources/X38e.md`

Three tests of the sharing and rescue services passed only on Oct 28 2026 (UTC): they read the events file of their fixed test day, but the events log wrote with the real clock. The tests now give the log the test clock. The services do not change.

- Tests: post 51 (unchanged count; they now pass on any day).
---

## 29. The telemetry (sidecars and ledger), written new for V3RDE

**When:** Oct 29 2026.
**Meta model:** none (the time and memory record of every service, Meta calls included).
**Files:** `telemetry/proc.go`, `telemetry/files.go`, `telemetry/sidecar.go`, `telemetry/ledger.go`, `telemetry/cmd/sidecar/main.go`, `telemetry/cmd/ledger/main.go`, `telemetry/Dockerfile`, `telemetry/go.mod`, `telemetry/telemetry_test.go` (all new)
**Sources:** `docs/sources/X38f.md`

The telemetry (a sidecar next to each service that records its CPU, memory and events, and a ledger that joins them into one log per day) existed before the Hackathon. V3RDE does not copy it: this is new code.

- It keeps the files on the box (the spools, the ledger, the saved read positions), the start, stop and restart signals, and the trace route, so the sidecars go on from where they stopped.
- Checked against the old code: its own tests pass on the new code, and an independent review fed the same files to both in 300 random runs with no difference.
- New, from that review: a line is on the disk before its read position is saved, so a power cut does not lose it; one broken file or line no longer stops the rest.
- Tests: telemetry 19 (new).

## 30. The food gate, written new for V3RDE

**When:** Oct 29 2026.
**Meta model:** PE-Core-S16-384 (Perception Encoder), the gate of entries 1 to 3.
**Files:** `gate/gate.go`, `gate/preprocess.go`, `gate/decode_std.go`, `gate/decode_turbo.go`, `gate/litert.go`, `gate/open_litert.go`, `gate/open_stub.go`, `gate/handler.go`, `gate/events.go`, `gate/cmd/gate/main.go`, `gate/Dockerfile`, `gate/go.mod`, the tests and `gate/testdata/` (all new)
**Sources:** `docs/sources/X38g.md`

The food gate (it says if a photo shows food before any paid model call) existed before the Hackathon. V3RDE does not copy it: this is new code.

- It keeps the routes, the answers, the events and the "fails closed" rule, so the services that call it do not change.
- The defaults are now the PE-Core gate and its pinned files (D32); the old defaults were the earlier gate.
- The LiteRT library is pinned for each CPU type, so the image also builds on an ARM box.
- The decode and the resize give Pillow 12.3.0's bytes; the tests compare them with Pillow's own output.
- A new test runs a tiny model through LiteRT and compares it with TensorFlow Lite's Python interpreter. It found a defect in a first version of the new code (a pointer that cgo refuses); that is fixed.
- Checked against the old code: its own tests pass on the new code, and an independent review compared the two; its findings are fixed.
- Tests: gate 26 (new); with the LiteRT and libjpeg-turbo tags 29, and these run in every image build.

## 31. The deploy files, written new for V3RDE

**When:** Oct 29 2026.
**Meta model:** none (the stack that runs the services and their Meta calls).
**Files:** `deploy/docker-compose.yml`, `deploy/Caddyfile`, `deploy/web/index.html`, `tools/v3rde_stack.sh` (all new)
**Sources:** `docs/sources/X40a.md`

The deploy files (the compose file, the edge configuration and the stack script) existed before the Hackathon. V3RDE does not copy them: this is new code.

- It keeps the services, their ports, routes, limits and secrets as files mounted read-only. The names are now `v3rde` (project, images, volumes, settings). The sign-in cookie keeps its name.
- New: the edge sets the caller's address itself on every route, so a caller cannot choose the address that their rate limit uses.
- New: each image has its own build stamp (the last commit that changed its code). A deploy that does not change an image keeps it, and its telemetry shows the commit it really runs.
- Until the app (ui branch) is merged, `/` shows a holding page; all API routes are live.
- Checked: the edge configuration was validated with Caddy 2.8.4 and tested with false address headers on each route.

## 32. All item photos of one scan in one call

**When:** Oct 29 2026.
**Meta model:** none (it serves the item photos of a Muse Spark + SAM 3.1 answer).
**Files:** `inference/bundle.go`, `inference/bundle_test.go` (new); `inference/service.go`, `inference/events.go` (the route and the event); `docs/analyze_photos_api.md` (new)
**Sources:** `docs/sources/X41a.md`

After a scan with several items, the app can now get every item photo in one call (`POST /api/analyze/photos`), already made small (800 px, about 40 KB each), instead of one call per item.

- Each photo must belong to the caller and to that scan; if one does not, the call gives "not found".
- The photos are made small at the same time on the CPU cores of the box, and are turned upright; a repeat call comes from a cache.
- An independent review found 8 defects; all are fixed.
- Tests: inference 72 (10 new).

## 33. Photo touch-up works through the public site

**When:** Oct 29 2026.
**Meta models:** Muse Image 1.0 (the edit) and Muse Spark 1.3 (the check), as in entry 16.
**Files:** `post/touchup.go`, `post/touchup_test.go`, `gate/events.go`, `gate/gate_test.go`
**Sources:** `docs/sources/X42c.md`

The first live test of photo touch-up on the new box found that every touch-up through the public site was refused ("no body"), because the request reached the service with a Content-Type header and an empty body.

- Now only a real body is refused (the rule that no photo is uploaded stays).
- Also: a food gate test that failed at random (about 1 run in 15) is fixed.
- Tests: post 52 (1 new); gate 26.

## 34. A refused touch-up edit is kept for review

**When:** Oct 29 2026.
**Meta models:** Muse Image 1.0 and Muse Spark 1.3, as in entry 16.
**Files:** `post/touchup.go`, `post/touchup_test.go`, `docs/morpheus/post_api.md`
**Sources:** `docs/sources/X42d.md`

When the check says that an edit changed the food, the app still keeps the original photo. Now the refused edit is also kept, in a separate folder, so the team can look at what the image model did.

- Only the owner can see it (`GET /api/touchup/{id}/rejected`); it can never become a listing photo.
- Also: a touch-up test from entry 33 that failed at random (about 1 run in 7) now waits for its jobs.
- Tests: post 53 (1 new).

## 35. Photo touch-up and voice update for outside callers

**When:** Oct 29 2026.
**Meta models:** Muse Image 1.0, Muse Spark 1.3 and Muse Voice Transcribe 1.0, as in entries 16 and 19.
**Files:** `post/touchup.go`, `post/touchup_test.go`, `post/cmd/touchup/main.go`, `deploy/Caddyfile`, `deploy/docker-compose.yml`, `tools/v3rde_stack.sh`, `docs/morpheus/post_api.md`
**Sources:** `docs/sources/X42e.md`

Outside callers with the bearer token (for example Postman or a partner) can now use photo touch-up (`/v1/touchup`) and voice update (`/v1/voice`), as they already could use the scan (`/v1/analyze`).

- A token caller can touch up only photos from its own token scans, and the app's users and token callers never see each other's photos or jobs.
- The daily spend caps still apply to every caller.
- Tests: post 54 (1 new).

---

## 36. A daily limit for each caller

**When:** Oct 29 2026.
**Meta models:** no new model (the caps cover SAM 3.1, Muse Spark 1.3, Muse Image 1.0 and Muse Voice Transcribe 1.0 calls).
**Files:** `post/post.go`, `post/touchup.go`, `post/voice.go`, `post/rescue.go`, `post/fanout.go`, `post/cmd/*/main.go`, `inference/events.go`, `inference/service.go`, `inference/cmd/inventory/main.go`, the tests, `deploy/docker-compose.yml`, `docs/morpheus/post_api.md`
**Sources:** `docs/sources/X44a.md`

Each paid feature had one daily limit for all callers. One caller could use it all and stop the feature for everyone until the next day.

- Each caller (one signed-in user, or one bearer address) now also has its own daily limit: scan $5, touch-up $0.50, voice, rescue and Facebook sharing $0.25 each. The limit of each service stays.
- A caller over its own limit gets `429 caller_daily_cap_reached`. The other callers still work.
- Each events line has the caller's key, so a restart keeps each caller's spend.
- The ids of photos and touch-ups are 128 random bits each (checked in the code; no change needed).
- Tests: inference 72 -> 73, post 54 -> 56.

---

## 37. The scan shows what it finds while it works

**When:** Oct 29 2026.
**Meta models:** SAM 3.1 and Muse Spark 1.3, as in entry 9 (no new model).
**Files:** `inference/scanjob.go`, `inference/progress.go`, `inference/scanjob_test.go` (new), `inference/service.go`, `inference/engine.go`, `inference/events.go`, `docs/analyze_scan_job_api.md` (new), `tools/v3rde_stack.sh`
**Sources:** `docs/sources/X44b.md`

A scan takes about 14 s, and the donor saw only a wait. Now the app can start the scan as a job and show its progress.

- `POST /api/analyze?async=1` answers at once with a `scan_id`. The app asks `GET /api/analyze/scan/{id}` about once a second.
- The answer shows the boxes as soon as SAM 3.1 has found the items (about 3 s), then the names as each Muse Spark call ends, then the full answer.
- The full answer and every refusal are the same as on the normal route. The normal route does not change.
- Polling, because API Gateway HTTP APIs cannot stream an answer.
- Only the caller that started a scan can see it, for 10 minutes.
- Tests: inference 73 -> 79.

---

## 38. Touch-up of every photo when the scan ends

**When:** Oct 29 2026.
**Meta models:** Muse Image 1.0 and Muse Spark 1.3, as in entry 16 (no new model).
**Files:** `post/touchup.go`, `post/touchup_batch_test.go` (new), `post/cmd/touchup/main.go`, `docs/morpheus/post_api.md`, `tools/v3rde_stack.sh`
**Sources:** `docs/sources/X44c.md`

A touch-up takes 21 s to 27 s. The app can now start it for every photo of the scan at once, while the donor reads the list, so the better photos are ready when the donor wants to post.

- `POST /api/touchup/batch?photo_ids=…` takes up to 6 photos of the scan in one call.
- A photo that finds no free slot waits in a short queue instead of getting "busy".
- Each photo is checked before any job starts. The daily limits count every photo.
- Tests: post 56 -> 61.

---

## 39. Edge and cloud, side by side, from the ledger

**When:** Oct 29 2026.
**Meta models:** PE-Core-S16-384 on the box; SAM 3.1, Muse Spark 1.3, Muse Image 1.0 and Muse Voice Transcribe 1.0 in the cloud (no new model).
**Files:** `telemetry/edgecloud.go`, `telemetry/edgecloud_test.go`, `telemetry/cmd/edgecloud/main.go` (new), `telemetry/Dockerfile`
**Sources:** `docs/sources/X44d.md`

V3RDE runs some parts on the box and some in Meta's cloud. A new command shows how long each part takes, from the ledger only.

- Edge: the PE-Core food gate and the barcode lookup (time and memory on the 2 GiB ARM box).
- Cloud: SAM 3.1, Muse Spark 1.3, Muse Image 1.0 and Muse Voice Transcribe 1.0, each call.
- The median, the 90th percentile and the max, with the number of values and the ledger field of each row.
- It runs on the box: `docker run --rm --entrypoint /edgecloud -v <ledger>:/ledger-data:ro v3rde-telemetry`.
- Tests: telemetry 19 -> 22.

---

## 40. The donors' corrections tune the scanner

**When:** Oct 29 2026.
**Meta models:** SAM 3.1 and Muse Spark 1.3 (the boxes and names that the donors correct; no new model).
**Files:** `inference/calibrate.go`, `inference/calibrate_test.go`, `inference/cmd/calibrate/main.go` (new), `inference/prepare.go`, `inference/Dockerfile`
**Sources:** `docs/sources/X44g.md`

The app already reports what the donor changed before posting: the names, the servings and the items removed (entry 15). A new command reads these reports with the scans they belong to.

- For each item: the size of its box as Muse read it, and whether the donor kept the model's name and servings.
- Counts by box size and by day (the series for the accuracy chart of the pilot).
- When there are enough labels, it suggests a new value for the size limit that marks an item "check this" (now 96 px). A person makes the change.
- It runs on the box: `docker run --rm --entrypoint /calibrate -v <analyze events>:/events:ro v3rde-analyze`.
- Tests: inference 79 -> 83.

---

## 41. A 30-second listing timer for the tests

**When:** Oct 29 2026.
**Meta models:** none (the listings service).
**Files:** `listings/listing.go`, `listings/store.go`, `listings/handler.go`, `listings/cmd/listings/main.go`, `listings/listings_test.go`, `deploy/docker-compose.yml`
**Sources:** `docs/sources/X44h.md`

The rescue test needs a listing that nobody claimed before its timer ran out, and the timer is 30 minutes.

- A test account can now post a listing with its own timer (`ttl_seconds`, 30 s to 30 minutes), so the rescue test runs in one minute.
- Only the test accounts named in the settings can do this. Every other user gets 403, and the timer stays 30 minutes.
- Tests: listings 27 -> 28.

---

## 42. Rescue reads the live food directory

**When:** Oct 29 2026.
**Meta models:** Muse Spark 1.3, as in entry 17 (no new model).
**Files:** `post/rescue.go`, `post/rescue_test.go`, `docs/morpheus/post_api.md`
**Sources:** `docs/sources/X44l.md`

The first live rescue test failed: the food directory answered, but the service looked for its list under the wrong name.

- The service now reads the list where the live directory has it, with the full address and the type of each place.
- Each place shows where its record comes from.
- When the directory answer cannot be read, the events line keeps the reason.
- Tests: post 61 -> 63.

## 43. The food gate runs on LiteRT 2.3.0

**When:** Oct 29 2026.
**Meta models:** PE-Core-S16-384 gate, as in entry 7 (the same model and head, the same pins).
**Files:** `gate/litert.go`, `gate/litert_test.go`, `gate/Dockerfile`, `gate/cmd/gatebench/main.go`
**Sources:** `docs/sources/X45a.md`

The gate ran on LiteRT 1.4.0 and took about 1.1 s for each photo on the box, before SAM and Muse could start.

- The gate now uses LiteRT 2.3.0. On all 1,432 gold photos no verdict changed.
- In the cloud bench one call took 589 ms instead of 1,646 ms (1 thread). The box bench gives the box numbers.
- The thread count reaches the CPU engine again. The service keeps 1 thread until the box bench.
- New tool `gatebench` (also in the image): it scores a folder of photos and compares two runs.

## 44. Lost in translation: the translation quality estimator runs

**When:** Oct 29 2026.
**Branch:** `LIF` (lost in translation), made from `main`. It does not change `services` or `ui`.
**Meta models:** none in this change. The estimator is for the translations that Muse Spark Open will make.
**Open-source dependency:** MetricX-24 (Google, Apache-2.0), labelled as such.
**Files:** `qe/Dockerfile`, `qe/smoke.sh`, `qe/smoke_input.jsonl`, `qe/run_predict.py`, `qe/test_qe.py`
**Sources:** `docs/sources/L1.md`

Muse Spark Open returns no log probabilities on our endpoint, so a translation cannot be scored from the translator itself. A separate quality estimator scores each translation from the source and the translation only (no reference).

- One container image holds MetricX-24 with all package versions pinned. The model (Large, full precision) downloads into its own Docker volume on the first run. The bf16 model does not run on the laptop's ARM CPU, and the full-precision model is loaded with low memory use so that it fits the limit (root causes in the sources file).
- The container has a hard 10 GB memory limit and no swap. The fp32 model did not fit in 6 GB (measured), so the Colima VM was raised to 12 GiB and the other containers are stopped while it runs.
- A smoke test scores six English to Spanish pairs, three of them bad on purpose. It checks that the pipeline runs; it is not an accuracy measure.
- Regression tests (`qe/test_qe.py`, 6 tests) keep the fixes in place: the package pins, the full-precision model, the low-memory load, the memory limit, and the smoke input format.
- Not deployed by this change.

---

## 45. The food gate uses 2 CPU threads

**When:** Oct 29 2026.
**Meta models:** PE-Core-S16-384 gate, as in entry 43 (no model change).
**Files:** `deploy/docker-compose.yml`
**Sources:** `docs/sources/X46a.md`

The box bench (X45b) measured the gate on LiteRT 2.3.0 on the box.

- With 2 threads one call takes 595 ms; with 1 thread it takes 1,071 ms (median, 50 photos).
- When two calls run at the same time, both settings take about 1.1 s each.
- The gate now uses 2 threads. No image is built again: only the gate container starts again.

## 45. Lost in translation: the estimator image rebuilds from its cache

**When:** Oct 29 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** none in this change.
**Files:** `qe/Dockerfile`, `qe/test_qe.py`
**Sources:** `docs/sources/L2.md`

In #44, each change to a package pin built the whole package layer again and downloaded torch again.

- The image keeps pip's and apt's downloads in build cache mounts, so a rebuilt layer downloads only what changed.
- The layers go from the least often changed (system packages, torch) to the most often changed (our own code), so a change near the end does not rebuild what is before it.
- Each run of the apply script shows, for every build step, whether it came from the cache.
- A new regression test keeps this in place (`qe/test_qe.py`, 7 tests). The smoke test and its scores are the same as in #44.
- Not deployed by this change.

---

## 46. The app is on the live site

**When:** Oct 29 2026.
**Meta models:** none (no model change; the app uses the services of entries 1–45).
**Files:** the merge of branch `ui` (UI entries 1–18: `mobile/`, `NEW_WORK_UI.md`, `docs/sources/U*.md`), `deploy/docker-compose.yml`, `deploy/Caddyfile`, `tools/v3rde_stack.sh`
**Sources:** `docs/sources/X46b.md`

Until now the edge served a holding page at `/`. The app was only on branch `ui`.

- Branch `ui` (U18, c36d8a7) is merged into `services`. The merge adds files only; no services file changes.
- The edge serves the committed web build `mobile/web-dist` at `/`, on the same address as the API.
- The deploy check now also gets the app's script file through the edge.
- The UI changes are listed in `NEW_WORK_UI.md`.

## 47. Voice works with long item names

**When:** Oct 29 2026.
**Meta models:** Muse Voice Transcribe 1.0 and Muse Spark 1.3, as in entry 17 (no model change).
**Files:** `post/voice.go`, `post/voice_test.go`, `docs/morpheus/post_api.md`
**Sources:** `docs/sources/X47a.md`

On the live site, two voice clips failed with "Could not read what you said". The transcription refused one item name as a keyword because it was longer than 30 characters, so the whole clip failed.

- Each item name goes to the transcription as a keyword of at most 30 characters, cut at the end of a word.
- A name that has no word end before 30 characters is left out. The same keyword goes only one time.
- The events line shows how many keywords were sent and how many were cut.
- Tests: post 63 -> 64. A touch-up test no longer reads the events file before the second job has written its line (it failed one time on the Mac).

## 47. Lost in translation: one English source file and the language list

**When:** Oct 29 2026.
**Branch:** `LIF`. It does not change `services` or `ui`. It reads the app code of `ui` at one pinned commit.
**Meta models:** none in this change. The file is the input that Muse Spark Open will translate.
**Files:** `i18n/` (a Go module: `lex.go`, `extract.go`, `gitread.go`, `languages.go`, `cmd/extract`, `i18n_test.go`, `testdata/`), `i18n/en.json`, `i18n/skipped.json`, `i18n/rules.json`, `i18n/source.json`, `i18n/languages.json`
**Sources:** `docs/sources/L3.md`

The English text of the app was in many files (`words.ts`, `signin.ts`, `logic.ts`, `ui.tsx` and others). Some lines were made by joining parts with `+`, which a translator cannot put in a different word order.

- A Go extractor reads every `.ts` and `.tsx` file of `mobile/src` on branch `ui` at the commit in `i18n/source.json`, directly from git. It does not copy or change the app code.
- Each line of text becomes one message in `i18n/en.json`, with an id (`VOICE.rename`), the full text with its values as placeholders (`Item {n}: “{from}” to “{to}”`), and the file and line it comes from. This includes the screen-reader labels.
- Every other string of the app is in `i18n/skipped.json` with the rule that skipped it (code word, compared value, style or test id, color, SVG path, URL). A person can read the two files and see that no text is missing. The few decisions that a rule cannot make are in `i18n/rules.json`, each with a reason.
- Some messages are parts that the app joins (for example `item found` for `plural()`). They are marked with the function they go to. The app must change to whole messages and per-language plural forms before those lines translate well; that is work for the `ui` branch, not this change.
- `i18n/languages.json` lists English (source), Spanish for Latin America (`es-419`, first), Brazilian Portuguese (`pt-BR`), Simplified Chinese (`zh-Hans`), Hindi (`hi`) and Modern Standard Arabic (`ar-001`, right to left), and the names that stay in English (`V3RDE`, `WhatsApp`).
- Regression tests (`i18n/i18n_test.go`, 7 tests): the lexer (comments, regex, escapes, nested templates), JSX text fails, the message rules, stale rules fail, `en.json` and `skipped.json` are what the extractor makes from the pinned commit, the language list (each native name in its own script), and the format of `en.json`.
- When the app text changes on `ui`: put the new commit in `i18n/source.json` and run `go run ./cmd/extract` in `i18n/`.
- Not deployed by this change.

---

## 48. The food gate runs at the same time as SAM

**When:** Oct 29 2026.
**Meta models:** SAM 3.1, Muse Spark 1.3 and the PE-Core-S16-384 gate, as before (no model change).
**Files:** `inference/overlap.go` (new), `inference/meta.go`, `inference/events.go`, `inference/service.go`, `inference/service_test.go`, `inference/cmd/inventory/main.go`, `docs/inference_engine_design.md`
**Sources:** `docs/sources/X48a.md`

The speed plan (T9: single photo 5–6 s, multi-item 10–12 s). Before this change nothing started until the gate answered.

- SAM starts at the same time as the food gate. The paid Muse calls wait for the gate, and a photo that is not food never reaches Muse.
- The busy check and the daily cap now come before the gate.
- Each call in the events line has `at_ms` (when it started) and `wait_ms` (its wait for the gate), so the slow part of each scan can be found.
- The Meta calls share one connection pool with more open connections, so fewer calls wait for a new connection.
- The caller's live listings are read at the start, at the same time as the scan.
- A scan job shows its first boxes only after the gate passed, so a photo that is not food shows no boxes.
- Tests: inference 83 -> 84.

## 48. Lost in translation: Arabic is `ar`, so every Arabic phone finds it

**When:** Oct 29 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** none in this change.
**Files:** `i18n/languages.json`, `i18n/i18n_test.go`
**Sources:** `docs/sources/L3b.md`

In #47 Arabic was `ar-001`. A phone set to Arabic has a tag such as `ar-EG` or `ar-SA`. Lookup removes subtags from the end (`ar-EG`, then `ar`), so it finds `ar` but never `ar-001`.

- Arabic is now `ar`, so the Arabic text serves all Arabic regions. The text stays Modern Standard Arabic (the name in `languages.json` says so).
- The language test expects `ar`.
- Not deployed by this change.

---

## 49. Each caller gets its own rate limit

**When:** Oct 29 2026.
**Meta models:** none (no model change).
**Files:** `deploy/Caddyfile`
**Sources:** `docs/sources/X48b.md`

Through API Gateway every request seemed to come from the same address, so all callers shared one rate limit.

- The SME set API Gateway to send the caller's real address in the header `X-Client-Ip`. API Gateway replaces any value the caller sends.
- The edge reads that address first, then the old header, and gives it to every service.
- The edge does not pass `X-Client-Ip` on to the services.

## 49. Lost in translation: names and values are locked before translation

**When:** Oct 29 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** none in this change. The tags are for the prompts to Muse Spark Open (#50).
**Files:** `i18n/mask/mask.go`, `i18n/mask/mask_test.go`
**Sources:** `docs/sources/L4a.md`

A translator can change or lose a placeholder such as `{n}`, or translate a name such as `V3RDE`.

- Before a message goes to the translator, each `{name}` and each name in `languages.json` "keep" becomes a numbered tag (`<x1/>`, `<x2/>`, ...).
- After the translation, each tag must be there exactly once. A lost, doubled or unknown tag, or a bare `{name}`, is an error, and that translation is not used. The translator can move a tag to where the grammar of the language needs it.
- Regression tests (`i18n/mask/mask_test.go`, 4 tests): masking, restore in Spanish, Chinese, Hindi and Arabic text and with the tags in a new order, the error cases, and a round trip of every message in `en.json`. The i18n module now has 11 tests.
- Not deployed by this change.

---

## 50. The app sees each step of a scan at once

**When:** Oct 29 2026.
**Meta models:** none (no model change).
**Files:** `inference/scanjob.go`, `inference/service.go`, `inference/scanjob_test.go`, `mobile/src/logic.ts`, `mobile/src/api.ts`, `mobile/tests/logic.test.mts`, `mobile/web-dist/`, `docs/analyze_scan_job_api.md`, `mobile/docs/web.md`
**Sources:** `docs/sources/X48c.md`

The app asked for the scan job once a second, so the answer came up to 1 s late.

- Each scan job has a version. It goes up at each change: the stage, the boxes, a name, the end.
- The app sends the version it has and `wait=8000`. The service holds the answer until the job changes, or for at most 10 s.
- The app asks again at once. It does not sleep between polls, and it asks for the first time at once.
- An older service has no version: the app then asks once a second, as before.
- Tests: inference 84 -> 86, mobile 81 -> 82.

## 50. Lost in translation: a Go client for Muse Spark Open on Baseten

**When:** Oct 29 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** Muse Spark Open (`meta-models/muse-spark-open-1026`), through Baseten. The tests use a fake server only; this change makes no calls to Baseten.
**Files:** `i18n/muse/muse.go`, `i18n/muse/prompt.go`, `i18n/muse/muse_test.go`
**Sources:** `docs/sources/L4b.md`

- One request is one answer (Baseten supports only `n = 1`). More candidates are more requests.
- The settings are fixed: the exact model name, temperature 1.0, top_p 1.0, `top_k: -1` (Baseten's default is 50), `reasoning_effort: low`, `max_tokens` 4096. Nothing is sent that Baseten rejects or ignores for this model (`n`, `logprobs`, `seed`).
- The key comes from `BASETEN_API_KEY` and is never in an error message.
- 429, 5xx and network errors are tried again (`Retry-After`, else 1 s, 2 s, 4 s). 400 and 401 are not.
- An answer with no text (the reasoning used all the tokens) is an error, not an empty translation.
- The prompt gives the target language, the tag rule (#49) and the message id as the only context of a short text.
- Regression tests (`i18n/muse/muse_test.go`, 5 tests, fake server). The i18n module now has 16 tests.
- Not deployed by this change.

---

## 51. A listing keeps what the scanner found

**When:** Oct 29 2026.
**Meta models:** none (no model change).
**Files:** `listings/details.go` (new), `listings/listing.go`, `listings/store.go`, `listings/handler.go`, `listings/listings_test.go`, `mobile/src/logic.ts`, `mobile/src/api.ts`, `mobile/src/screens/Review.tsx`, `mobile/tests/logic.test.mts`, `mobile/web-dist/`
**Sources:** `docs/sources/X48d.md`

The scanner finds more than the title and the servings, but a listing kept only those two.

- The app now sends the other fields of each item in one optional form field, `details`: description, vegetarian or not, serving type, meal condition, weight and cost.
- The listings service keeps them with the listing and gives them back under `details`.
- Only these six keys are taken. A wrong value is refused with a clear error. The app sends only correct values, so a post never fails on them.
- A listing with no details is the same as before.
- The app does not show the details yet. That needs a mockup first.
- Tests: listings 28 -> 29, mobile 82 -> 83.

## 51. Lost in translation: rung 1, translate and score every message

**When:** Oct 29 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** Muse Spark Open, through Baseten (only when `cmd/translate` runs; the tests use fakes).
**Open-source dependency:** MetricX-24 (Google, Apache-2.0), labelled as such.
**Files:** `i18n/rung/rung.go`, `i18n/rung/score.go`, `i18n/rung/util.go`, `i18n/rung/rung_test.go`, `i18n/cmd/translate/main.go`, `qe/score.sh`
**Sources:** `docs/sources/L4c.md`

- `go run ./cmd/translate -lang es-419 -out <dir>` (in `i18n/`): every message of `en.json` goes to Muse Spark Open `k` times (default 1) with its tags (#49), 8 requests at a time.
- Each answer is checked and its tags are put back. MetricX-24 (QE mode, the image of #44/#45) scores all good answers in one container run (`qe/score.sh`). For each message the answer with the lowest score is kept.
- A message of only kept names (`V3RDE`, `WhatsApp`) is copied with no call.
- The output goes outside the repo: `<lang>.json` (the kept text, its score, and the ids with no good answer) and `<lang>.ledger.jsonl` (one line for each try: the masked text, the raw answer, the error or score, the settings, the reasoning tokens and the `en.json` commit). A later change commits a reviewed result.
- The scores rank the answers of one message. They are not an accuracy measure.
- Regression tests (`i18n/rung/rung_test.go`, 2 tests, fake model and fake scorer). The i18n module now has 18 tests.
- Not deployed by this change.

---

## 52. The lists scroll, and Receive shows food of others only

**When:** Oct 29 2026.
**Meta models:** none (no model change).
**Files:** `mobile/src/screens/Frame.tsx`, `mobile/src/screens/Give.tsx`, `mobile/src/screens/Receive.tsx`, `mobile/src/ui.tsx` (`ScreenHead`), `mobile/src/listing.ts` (`othersOnly`), `mobile/src/words.ts`, `mobile/tests/listing.test.mts`, `mobile/tests/style.test.mts`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/X48e.md`

On the web the lists did not scroll: older listings and the next pages of Receive were out of reach.

- The frame now has the height of the window, and each list scrolls inside it. Receive loads its next page as you scroll.
- The title of Give and of Receive is in one row of the same height, so it does not move when you change the role.
- Receive does not show your own listings. You see them under Give. When a page holds only your listings, the list asks for the next page at once.
- Tests: mobile 83 -> 86.

## 52. Lost in translation: the Baseten key is kept in a file, like the Meta key

**When:** Oct 29 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** none in this change.
**Files:** `tools/lif_baseten_key.sh`, `i18n/muse/muse.go`, `i18n/muse/muse_test.go`, `i18n/cmd/translate/main.go`
**Sources:** `docs/sources/L4d.md`

In #51, `cmd/translate` read the key only from `BASETEN_API_KEY`, so each new shell needed the key again.

- `tools/lif_baseten_key.sh` asks for the key once (the typing is hidden) and writes `~/.config/v3rde/baseten_api_key` (or `$V3RDE_CFG/baseten_api_key`) with mode 0600, in a folder with mode 0700. It never prints the key. The Meta key of the stack is kept the same way (`meta_model_api_key`, 0600).
- `cmd/translate` reads the key from that file (`-key-file`). A file with another mode, or with more than the key, stops the run. With no file, it uses `BASETEN_API_KEY`. With neither, the error names the setup script. The key is never in an error message.
- A new regression test (`i18n/muse/muse_test.go`, 6 tests). The i18n module now has 19 tests.
- Not deployed by this change.

---

## 53. Touch-up v2: a prototype and a side-by-side page

**When:** Oct 29 2026.
**Meta models:** Muse Spark 1.3 (chooses the settings), SAM 3.1 ("food" mask), Muse Image 1.0 (background only). Only in the comparison tool; the touch-up service does not change.
**Files:** `server/enhance.go` (new), `server/enhance_test.go` (new), `inference/touchupv2.go` (new), `inference/touchupv2_test.go` (new), `inference/cmd/touchupcompare/main.go` (new), `inference/Dockerfile`
**Sources:** `docs/sources/X48f.md`

The touch-up of today takes about 28 s and can change the food. T9 asked for a sharper, bigger photo of the item, made faster.

- **enhance:** code only. Crop, enlarge up to 2x, contrast, colour balance, exposure, colour and sharpness. No Meta call, no cost, under half a second.
- **smart:** Muse Spark looks at the photo and gives only the settings (numbers). The same code makes the edit.
- **smart_bg:** smart, then Muse Image paints a plain background. Only that background is used. The food pixels come from the real photo through a SAM mask of "food".
- `touchupcompare` makes all variants and today's touch-up for the newest item photos on the box, with the time and the cost of each. The script `x48f_touchup_compare.sh` brings a side-by-side page to the Mac.
- T9 chooses the default from that page. Nothing changes in the app until then.
- Tests: server 26 -> 32, inference 86 -> 89.

## 53. Lost in translation: requests at a steady rate, and a resume run

**When:** Oct 29 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** Muse Spark Open, through Baseten (only when `cmd/translate` runs; the tests use fakes).
**Files:** `i18n/muse/pace.go`, `i18n/muse/muse.go`, `i18n/muse/muse_test.go`, `i18n/rung/rung.go`, `i18n/rung/rung_test.go`, `i18n/cmd/translate/main.go`
**Sources:** `docs/sources/L4e.md`

The first Spanish run (run `20261029T215819Z`) kept 179 of 204 messages. All 25 failures were `HTTP 429: Rate limit exceeded`. There were no tag errors and no empty answers. Root cause: 8 workers sent all requests in about 80 seconds, and the client gave up after 4 tries in 7 seconds, which is less than the one-minute window of the limit.

- A pacer spaces the requests evenly. It takes the limit from Baseten's `x-ratelimit-limit-requests` header, or from `-rpm`.
- After a 429 with no `Retry-After`, the client waits 1, 2, 4, 8, 16 and 32 seconds, plus a random part of a second (6 retries, more than one minute in all).
- `-resume` translates only the ids that are missing in `<out>/<lang>.json` and merges the new answers into it, in the `en.json` order. The earlier run id is kept in `previous_runs`. The ledger gets the new tries at the end.
- 3 new regression tests (backoff, pacer, merge). The i18n module now has 22 tests.
- Not deployed by this change.

---

## 54. Touch-up v2: studio modes, Muse Image called correctly, a check in code

**When:** Oct 30 2026.
**Meta models:** SAM 3.1 (the item mask), Muse Image 1.0 (two edits). Only in the comparison tool; the touch-up service does not change.
**Files:** `server/studio.go` (new), `server/studio_test.go` (new), `server/enhance.go`, `inference/touchupv2.go`, `inference/touchupv2_test.go`, `inference/cmd/touchupcompare/main.go`
**Sources:** `docs/sources/X48g.md`

The first comparison page showed two problems. The code edit was too weak to see. Muse Image did not edit the photo: it made a new, unrelated picture.

- **Studio (code):** a stronger look (clarity, sharpness, colour). The item is cut out with a SAM mask and made up to 2x bigger. The background is a light surface with a soft shadow, or the photo itself strongly blurred. No food pixel is painted.
- **Muse Image:** now called as Meta's docs and cookbook show: JSON with the photo in `images`, search and shell tools off, and a prompt that names only the change and then what must stay.
- **A check in code:** each Muse Image edit is compared with its source (SSIM). A new picture gets a low score and is refused.
- **Small item photos:** under 500 px, the item is cut again from the upload with a wider margin, so the edits have more pixels.
- Tests: server 32 -> 35, inference 89 -> 90.

## 55. Touch-up v2: the combo T9 chose (Muse bigger, then studio blur, the container kept)

**When:** Oct 30 2026.
**Meta models:** Muse Image 1.0 (one edit), SAM 3.1 (food and container masks). Only in the comparison tool; the touch-up service does not change yet.
**Files:** `inference/touchupv2.go`, `inference/touchupv2_test.go`, `server/studio.go` (`Resize`), `server/studio_test.go`
**Sources:** `docs/sources/X48h.md`

T9 chose a combination from the second comparison page.

- **Sharp + blur:** the photo is made 1024 px in code, then Muse Image makes it bigger and sharper. The edit must pass the structure check. Then the item is cut out of that edit and the edit itself, blurred, goes behind it.
- **The container stays:** SAM is also asked for a plate, a bowl, a food tray and a takeout container. A container that holds the food is kept with it.
- **A stricter check:** the limit is 0.75. On the last page, every true edit scored 0.82 or more, and every edit that invented a wider scene scored 0.67 or less.
- If Muse Image fails or is refused, the code result is used.
- Tests: server 35 -> 36, inference 90 (2 tests replaced).

## 55. Lost in translation: each English text once, Docker checked first, the ledger written as it goes

**When:** Oct 30 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** Muse Spark Open, through Baseten (only when `cmd/translate` runs; the tests use fakes).
**Files:** `i18n/rung/rung.go`, `i18n/rung/score.go`, `i18n/rung/rung_test.go`, `i18n/cmd/translate/main.go`
**Sources:** `docs/sources/L4f.md`

Problems found in the first runs for 5 languages:
- The same English text got different translations (zh-Hans: "no blob" became 无斑点 and 无 blob).
- Colima was not running, so the scorer failed after all the requests. Every answer was lost, also the answers for languages that were already complete.

The fixes:
- Each English text is translated once. The other ids with the same text reuse that translation (`same_as`). 204 messages are 189 texts, so 189 requests for each language (T9 2026-10-30: once, always).
- `-collapse` applies the same rule to an earlier result with no requests. For each text it keeps the translation with the lowest score, and it keeps the old file as `<lang>.before-dedupe.json`.
- Before the first request, the run checks that Docker answers and that the MetricX image is there. If the check fails, no request is sent.
- The ledger gets each try when its answer comes in (`"kind":"try"`), and each score after the scoring (`"kind":"score"`), so a failure later in the run does not lose answers.
- A language whose result is complete (same `en.json` commit, model and k, made with dedupe) is skipped unless `-force` is given.
- 4 new regression tests (dedupe, check first, complete, collapse). The i18n module now has 26 tests.
- Not deployed by this change.

---

## 56. Touch-up v2: the item is cut out alone

**When:** Oct 30 2026.
**Meta models:** SAM 3.1, Muse Image 1.0 (no change). Only in the comparison tool.
**Files:** `inference/touchupv2.go`, `inference/touchupv2_test.go`, `inference/cmd/touchupcompare/main.go`
**Sources:** `docs/sources/X48i.md`

On the last page the cut kept neighbours of the same kind: the tomatoes next to the bananas, the plantains next to the tomatoes, the next cereal boxes.

- The scan knows the item's box. The item is cut again from the upload with that box, and only the SAM mask that matches the box is kept (plus masks inside it).
- The scan's name of the item is asked first, then "food", "fruit" and the others.
- Tests: inference 90 -> 92.

## 56. Lost in translation: the text graph, a context for every text

**When:** Oct 30 2026.
**Branch:** `LIF`. It does not change `services` or `ui`. It reads the app code of `ui` at the commit in `i18n/source.json`.
**Meta models:** none in this change. The graph and the contexts are for the prompts to Muse Spark Open (next change).
**Files:** `i18n/graph.go`, `i18n/graph_test.go`, `i18n/graph.json`, `i18n/context.json`, `i18n/concepts.json`, `i18n/cmd/graph/main.go`, `i18n/testdata/app/`, `i18n/extract.go`, `i18n/gitread.go`, `i18n/i18n_test.go`, `i18n/cmd/extract/main.go`, `i18n/en.json`
**Sources:** `docs/sources/L4g.md`

A translator needs to know what a text does and what is near it. In the first runs, "Post {plural}" was translated as a noun because nothing said that it is a button. When the app text changes, the texts that depend on it must be checked again.

- **A context for every text:** `i18n/context.json` has one context for each text, in ASD-STE100. It gives the kind (button, title, error line ...), the screen, what the text does for the user, and what each placeholder holds. Each context stores the hash of the English text it was written for. All contexts are drafts until the user approves them.
- **The text graph:** `i18n/graph.json` is made from the app code. Its nodes are the texts, the screens (10) and the main ideas of the app (14 concepts, `i18n/concepts.json`). Its edges are:
  - `on_screen`: the screen shows the text (found in the code);
  - `part_of`: the app joins the text into another text (found in the code);
  - `uses`: the text names a concept;
  - `opens` and `pair`: hand-made, for example "Log out" is paired with the sign-in button.
- **The change walk:** `go run ./cmd/graph` compares the committed texts with a new ui commit. It lists the added, removed and changed texts, walks the graph from them, and lists the texts to check, most connected first.
- **Checks:** a test fails when a text has no context, when the English text changed after its context was written, or when a context is left for a text that is gone. So a change to the app text cannot pass until its context is updated.
- **Two texts recovered:** the extractor lost the literals in a joined line that starts with `(` (for example `(...) + " — your photo is kept."`). It now counts every literal and fails if one is not in a message or a skip. `en.json` has 206 messages, 2 more than before: `logic.retryText.your_photo_is_kept` and `capture.elapsedText`.
- 8 new regression tests (`i18n/graph_test.go`). The i18n module now has 34 tests.
- Not deployed by this change.

---

## 57. Touch-up v2: one mask rule for every item

**When:** Oct 30 2026.
**Meta models:** SAM 3.1 (no change). Only in the comparison tool.
**Files:** `inference/touchupv2.go`, `inference/touchupv2_test.go`
**Sources:** `docs/sources/X48j.md`

Piles (tomatoes, plantains) still kept the pile next to them. SAM answers a pile with many small masks, one for each piece, and the old rule needed one mask for the whole item.

- The rule is the same for every kind of item: the masks that are mostly inside the item's box are the item. The item must fill at least 30% of its box.
- The food mask is cut to the item's box plus 5%, so a neighbour that SAM put in the same mask is cut away. A plate or a tray is not cut.
- The page shows, for each SAM concept, how many masks SAM found and how many were kept.
- Tests: inference 92 -> 93.

## 57. Lost in translation: each prompt gets the context of its text from the graph

**When:** Oct 30 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** Muse Spark Open, through Baseten (only when `cmd/translate` runs; the tests use fakes).
**Files:** `i18n/knowledge.go`, `i18n/knowledge_test.go`, `i18n/muse/prompt.go`, `i18n/rung/rung.go`, `i18n/rung/rung_test.go`, `i18n/cmd/translate/main.go`
**Sources:** `docs/sources/L4h.md`

#56 made the graph and the contexts. With this change, the translator uses them.

- **The context in the prompt:** each prompt now has the context of its text (prompt `context-v1`):
  - the kind and what the text does;
  - what each tag holds;
  - the texts it is paired with ("Log out" with the sign-in button);
  - the line it is joined into, or the parts joined into it;
  - up to 8 controls and titles near it on its screen;
  - the earlier translations of the texts that name the same idea (from `<out>/<lang>.json` or `-prev`), so one idea keeps one word.
  - A new rule tells the model that a button needs a verb, and that a text must not read like a near text with another action.
- **Dedupe key:** the key is now the English text plus its context. The same English with another context is translated on its own.
- **New texts are translated:** `-resume` and the skip check now compare the result with every text in `en.json`, so texts that are new in `en.json` are translated. A result made with the plain prompt is not complete for the context prompt.
- **The sweep:** `-ids` runs a small batch, and `-effort` and `-temperature` set other settings for that run. `-context=false` sends the plain prompt for a comparison.
- 2 new regression tests (context key, context block); the complete test now also covers new texts and the prompt. The i18n module now has 36 tests.
- Not deployed by this change.

---

## 58. Touch-up v2: the item's mask from the scan's box

**When:** Oct 30 2026.
**Meta models:** SAM 3.1 (box and point prompts), Muse Image 1.0 (no change). Only in the comparison tool.
**Files:** `inference/touchupv2.go`, `inference/touchupv2_test.go`, `inference/cmd/touchupcompare/main.go`
**Sources:** `docs/sources/X48k.md`

SAM found nothing for the scan's names ("plantains", "roma tomatoes"). The general words ("fruit", "food") gave one mask over two piles.

- **Step 1 (the established way):** the scan's box of the item goes to SAM as a box prompt, and SAM gives back one mask.
- **Step 2:** one point inside that mask, plus a "not this" point on each other object of the same scan. This is the method of the SAM-PD paper.
- The page shows the words mask, step 1 and step 2 side by side, all on the combo T9 chose (Muse Image bigger, then studio blur).
- Tests: inference 93 (2 tests replaced).

## 58. Lost in translation: translate many, score once

**When:** Oct 30 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** Muse Spark Open, through Baseten (only when `cmd/translate` runs; the tests use fakes).
**Open-source dependency:** MetricX-24 (Google, Apache-2.0), labelled as such.
**Files:** `i18n/rung/batch.go`, `i18n/cmd/score/main.go`, `i18n/rung/rung.go`, `i18n/rung/rung_test.go`, `i18n/cmd/translate/main.go`
**Sources:** `docs/sources/L4i.md`

The sweep of #57 was 25 runs (5 languages × 5 settings), and each run started the MetricX container and loaded its model again. That was most of the 30 minutes. More languages come next.

- `cmd/translate -score=false` translates and writes the result and the ledger with no scores (`"unscored": true`). It keeps the first good try.
- `go run ./cmd/score <dir>/*/*.json` collects every try that has no score yet in all the given results. It scores them in one container run, so the model loads once. It writes a score line for each try to its ledger, and rewrites each result with the lowest-scored try. A second run scores nothing again.
- The scores are the same MetricX-24 QE scores (0 to 25, lower is better). They rank the tries of one text and are not an accuracy measure.
- 1 new regression test: two results, one scorer call, score lines in each ledger, and the lowest-scored try kept. The i18n module now has 37 tests.
- Not deployed by this change.

---

## 59. Touch-up v2: the box masks are read

**When:** Oct 30 2026.
**Meta models:** SAM 3.1 (no change). Only in the comparison tool.
**Files:** `inference/touchupv2.go`, `inference/touchupv2_test.go`
**Sources:** `docs/sources/X48l.md`

On the last page, every box column said "no mask". A debug run showed why: SAM did give a mask for the box, but in a format that our decoder does not read.

- Box and point prompts now ask for the format that the decoder reads (`mask_encoding: one_bit`). In the debug run this was also faster: under 1 s instead of 3 to 5 s.
- Tests: inference 93 (no new test; the fake SAM now refuses a box prompt without the setting).

## 60. Touch-up v2: the mask is chosen by the scene

**When:** Oct 30 2026.
**Meta models:** SAM 3.1, Muse Image 1.0 (no change). Only in the comparison tool.
**Files:** `inference/touchupv2.go`, `inference/touchupv2_test.go`
**Sources:** `docs/sources/X48m.md`

T9 chose the words mask for a meal and for packages, and the box mask for piles of produce. One rule now makes that choice, with no list of foods:

- No item box (a meal photographed as a whole): the words mask, which keeps the tray.
- The words mask stays inside the item's box (a package, a plate): the words mask.
- The words mask spills past the item's box, or there is no words mask (a pile next to another pile): the box mask.
- The "not this" points (step 2) are not used: they cut holes in the items.
- The page shows each photo's choice and the reason.
- Tests: inference 93 -> 94.

## 60. Lost in translation: checks with no model

**When:** Oct 30 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** none in this change.
**Files:** `i18n/judge/checks.go`, `i18n/judge/checks_test.go`, `i18n/judge/load.go`, `i18n/cmd/check/main.go`, `i18n/results.go`, `i18n/results_test.go`, `i18n/knowledge.go`, `i18n/cmd/score/main.go`, `i18n/rung/rung_test.go`
**Sources:** `docs/sources/L4j-a.md`

MetricX scores one text at a time. It does not see that two texts on one screen use the same word, or that one idea gets two words. These checks use the text graph of #56 and need no model.

- `pair_same`: the two texts of a pair (for example Show and Hide) have the same translation.
- `target_inconsistent`: two texts with the same text and context have different translations.
- `source_same`: different English texts on one screen have one translation. Plural parts and brand names are not reported.
- `untranslated`: a text in a language with a non-Latin script still has Latin words (emails are not counted).
- `go run ./cmd/check runs-ctx/*/*.json` writes `<lang>.check.json` next to each result. On the earlier plain runs it finds real errors: pt "Grabbing…" as "Carregando…" (Loading), and zh "Post listing" and "listings" both as 发布列表.
- `i18n.IsResult` keeps only `<lang>.json` names, so `cmd/score`, `cmd/check` and the judges do not read the report files next to the results.
- Test fix: `TestRung` (from #51) failed about 1 time in 200. Root cause: its fake chose the lost-tag answer from a call count that all workers share, and the order of the workers changes from run to run. The fake now counts the tries of each message. 1000 runs with `-race` pass.
- 2 new regression tests. The i18n module now has 39 tests.
- Not deployed by this change.

---

## 61. Voice: the clip can be stopped on iOS

**When:** Oct 30 2026.
**Meta models:** none (no model change).
**Files:** `mobile/src/webvoice.ts`, `mobile/tests/webvoice.test.mts` (new), `mobile/package.json`, `mobile/web-dist/`
**Sources:** `docs/sources/X48n.md`

On an iPhone, the microphone button started a voice clip but did not stop it. Desktop browsers and Android worked.

- iOS Safari starts audio only from a tap. The app made its audio after the microphone question, outside the tap, so the audio never started and the start never ended. Stop waits for the start, so Stop did nothing.
- Now the audio is made and started in the tap, before the microphone question.
- If the audio still does not start, the app stops waiting after 2 s and asks the user to tap again. The button never stays stuck.
- Tests: mobile 86 -> 89.

## 61. Lost in translation: two judges read whole concepts and whole screens

**When:** Oct 30 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** Muse Spark Open (Baseten) and Muse Spark 1.3 (Meta Model API), only when `cmd/judge` runs; the tests use fakes.
**Files:** `i18n/meta/meta.go`, `i18n/meta/meta_test.go`, `i18n/judge/judge.go`, `i18n/judge/judge_test.go`, `i18n/cmd/judge/main.go`
**Sources:** `docs/sources/L4j-b.md`

A judge reads a group of texts of one language together: all texts of one concept of the graph, or all texts of one screen. For each text it gets the English text, what the text does, and the translation.

- A concept judge checks that one idea has one word, that buttons use the action form, and that the grammar is correct.
- A screen judge checks that pairs and opposites read as different actions, that the form of address is the same on the screen, and that each text fits what it does.
- Two judges: Muse Spark Open on Baseten and Muse Spark 1.3 on the Meta Model API. A text is flagged when one judge or one check reports it. The two judges are not yet compared with labels, so this report gives no accuracy numbers.
- The answer is JSON with a verdict and issues. A text id that is not in the group is dropped. A fail with no known text is a judge error.
- `meta/` is a small client for `https://api.meta.ai/v1/responses`. The key is in `~/.config/v3rde/meta_model_api_key` (mode 0600). Errors do not show the key.
- `go run ./cmd/judge runs-ctx/*/*.json` writes `<lang>.judge.json`: the checks, each verdict, and the flagged texts with the reasons.
- 5 new regression tests. The i18n module now has 44 tests.
- Not deployed by this change.

---

## 62. Touch-up: the service uses the scene rule

**When:** Oct 30 2026.
**Meta models:** Muse Image 1.0, SAM 3.1. Muse Spark is no longer called for the touch-up check.
**Files:** `inference/touchupauto.go` (new), `inference/touchupauto_test.go` (new), `inference/touchupv2.go`, `post/touchup.go`, `post/touchup_v2_test.go` (new), `post/cmd/touchup/main.go`, `post/go.mod`, `post/go.sum`, `post/Dockerfile`, `post/Dockerfile.dockerignore`, `deploy/docker-compose.yml`
**Sources:** `docs/sources/X48o.md`

T9 chose the "auto" column of the X48m page. The touch-up in the app now makes that picture.

- The item is cut again from the uploaded photo, with the scan's box and the scan's name of the item.
- Muse Image makes the item sharper and bigger. Code checks the edit (SSIM and the same shape). A new picture is refused, and then the code look is used on the photo.
- "Background": the scene rule chooses the mask (words, or the box when the words mask spills or is missing), then the background is blurred. A plate or a tray stays.
- "Light": the Muse edit only, with no new background.
- The original photo is always kept. The reason on the job says which mask was used.
- Tests: inference 94 -> 96, post 64 -> 66.

## 62. Lost in translation: fix the flagged texts once, and spread the fix through the graph

**When:** Oct 30 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** Muse Spark Open, through Baseten (only when `cmd/translate` runs; the tests use fakes).
**Files:** `i18n/rung/rung.go`, `i18n/rung/rung_test.go`, `i18n/cmd/translate/main.go`, `i18n/graph.go`, `i18n/graph_test.go`
**Sources:** `docs/sources/L4j-c.md`

The sweep of #57 showed that other settings (effort, temperature) do not correct a wrong word. The judge's reason is new information, so the second try gets it in the prompt. A corrected word must also agree with the texts of the same idea, so the correction goes through the text graph.

- `cmd/translate -fix <lang>.judge.json` translates each flagged text again, one time, with the context prompt and the line "Problems found in the earlier translation …: <reasons>. Do not repeat these problems."
- Spread (`-spread`, on by default): each corrected text that changed reaches its pairs, its joined parts and the texts of the same concept (`i18n.Spread`). Texts that are only on the same screen are not reached; the screen judge sees them. Each reached text gets one try with the correction in its prompt ("A related text was corrected … Use the same word for the same idea"), and its concept block shows the corrected translation. A flagged, revised or shared text is not reached.
- The new tries go into the same ledger and result. A text with a good new try keeps only the new tries; the result marks it `revised`, and a spread text also gets `spread_from`. Both stay after `cmd/score`.
- The result before the round is kept as `<lang>.before-round1.json`, and the report as `<lang>.judge.round1.json` (then `round2`, and so on). After `cmd/score` and `cmd/judge` again, a text that is still flagged is not translated a third time. `cmd/translate` names it for human review.
- 3 new regression tests (fix kept, spread kept after a rescore, spread targets). The i18n module now has 47 tests.
- Not deployed by this change.

---

## 63. Touch-up: one touch-up for every mode

**When:** Oct 30 2026.
**Meta models:** Muse Image 1.0, SAM 3.1 (no change).
**Files:** `inference/touchupauto.go`, `post/touchup.go`, `post/touchup_v2_test.go`
**Sources:** `docs/sources/X48q.md`

T9: the app has one touch-up, the auto one.

- "Light" now gives the same result as "Background": the Muse edit, the scene rule's mask and the blurred background.
- The "Light" path (the Muse edit only) is removed.
- Tests: post 66 (one test changed).

## 63. Lost in translation: one review file for the review page and the graph page

**When:** Oct 30 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** none in this change.
**Files:** `i18n/judge/review.go`, `i18n/judge/review_test.go`, `i18n/cmd/report/main.go`
**Sources:** `docs/sources/L4j-d.md`

- `go run ./cmd/report -out lif_review.json runs-ctx/*/*.json` joins, for each language and each text: the translation before and after the fix round, the MetricX scores, the findings of round 1 and of the last round (the check name or the judge model, the group and the reason), and `spread_from`.
- Each text gets one status: `ok`, `fixed` (corrected and passes now), `spread` (changed because a related text was corrected, passes now), `review` (corrected once and flagged again: a person decides), `flagged` (flagged in the last round only), `missing`.
- Each language gets a summary: the counts, the groups that each judge failed, and the judge errors. The counts are not accuracy numbers.
- The review page and the graph page read this file to show the loop.
- 1 new regression test. The i18n module now has 48 tests.
- Not deployed by this change.

---

## 64. Touch-up: one link in the app

**When:** Oct 30 2026.
**Meta models:** none (no model change).
**Files:** `mobile/src/screens/TouchUp.tsx`, `mobile/src/screens/Review.tsx`, `mobile/src/api.ts`, `mobile/src/words.ts`, `mobile/tests/touchup.test.mts`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/X48r.md`

T9: one touch-up button, not two.

- Each item now has one link, "Touch up photo", in place of "Better light" and "Plain background".
- The link starts the auto touch-up (the Muse edit, the scene rule's mask, the blurred background).
- Tests: mobile 89 -> 90.

## 64. Lost in translation: translate one screen in one call

**When:** Oct 30 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** Muse Spark Open, through Baseten (only when `cmd/translate` runs; the tests use fakes).
**Files:** `i18n/muse/batch.go`, `i18n/muse/batch_test.go`, `i18n/batch.go`, `i18n/batch_test.go`, `i18n/rung/batchrun.go`, `i18n/rung/rung.go`, `i18n/rung/rung_test.go`, `i18n/cmd/translate/main.go`
**Sources:** `docs/sources/L4k-a.md`

The run of #60-#63 (5 languages) showed two problems. Each text got its own call, so the 206 texts of a language took about 200 calls at the Baseten limit of 100 calls a minute. And the spread of #62 gave each reached text its own call, so one idea got different words: zh "Your listings" became 我的发布 ("my") while other texts of the same idea kept 你的 or got 您的.

- `cmd/translate -batch 12` translates the texts of one screen in one call (at most 12 texts in a call; `i18n.BatchGroups`: the smallest screen of a text, then its first concept; the texts on no screen by concept). Each text keeps its own context block. The answer is one JSON object, id -> translation (prompt `batch-v1`, max_tokens 8192).
- Each text of the answer gets its own ledger entry (`batch`: the group) and the same tag check. A text with no good answer from its group gets one call of its own, so no text is lost.
- With `-fix`, each flagged text and the texts it reaches through the graph go in one call, in the same round, so the group corrects its texts together. Without `-batch`, `-fix` works as in #62.
- In a test with fake servers, the 206 texts of a language took 25 calls.
- 3 new regression tests. The i18n module now has 51 tests.
- Not deployed by this change.

---

## 65. Review: the chat bar (talk or type)

**When:** Oct 30 2026.
**Meta models:** Muse Voice Transcribe 1.0 (voice lines), Muse Spark 1.3 (voice and typed lines).
**Files:** `post/voice.go`, `post/voice_test.go`, `mobile/src/screens/Voice.tsx`, `mobile/src/screens/Review.tsx`, `mobile/src/voice.ts`, `mobile/src/api.ts`, `mobile/src/words.ts`, `mobile/tests/voice.test.mts`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/X48s.md`

T9 approved the chat bar mockup. The review list now works like a chat with the app.

- Under the list: the microphone and a text box. Talk, or type and tap Send.
- The voice service takes typed text too. It skips the transcription and uses the same Muse Spark step.
- The changes go into the list at once. The chat shows what was said or typed and what changed. Undo is on the last change only.
- A changed card and the pickup say "Changed in the chat".
- On a wide screen the scan photo stays in place; only the list scrolls.
- The chat is cleared after a post. Post waits while the microphone is on.
- A line cannot remove every item; it says so (Start over does that).
- Tests: post 66 -> 67, mobile 90 -> 92.

## 65. Lost in translation: compare the ways of translating

**When:** Oct 30 2026.
**Branch:** `LIF`. It does not change `services` or `ui`.
**Meta models:** none in this change (it reads the results of `cmd/translate` and `cmd/judge`).
**Files:** `i18n/judge/compare.go`, `i18n/judge/compare_test.go`, `i18n/cmd/compare/main.go`, `i18n/rung/ledger.go`, `i18n/rung/rung_test.go`
**Sources:** `docs/sources/L4k-b.md`

- `go run ./cmd/compare -arm plain=runs -arm context=runs-ctx:round1 -arm loop=runs-ctx -arm batch=runs-batch:round1 -arm batch+loop=runs-batch` puts the ways of translating side by side for each language, with the same measures: model calls, MetricX QE (median, number over 5), check issues, texts flagged by a check or a judge, texts flagged by both judges, and the groups each judge failed. It writes `compare.json` and `compare.md`.
- `name=dir:round1` reads the result before the fix round (`<lang>.before-round1.json`, `<lang>.judge.round1.json`). Its scores come from the ledger (`rung.TextScores`), because `cmd/score` scores only `<lang>.json`.
- `rung.Calls` counts the calls of a result from its ledger: one for each single try, one for each batch group.
- These are measures, not accuracy: there are no human labels. MetricX ranks tries; the judges can disagree.
- 2 new regression tests. The i18n module now has 53 tests.
- Not deployed by this change.

## 66. Give: delete a listing before a grab

**When:** Oct 30 2026.
**Meta models:** none (no model change).
**Files:** `listings/store.go`, `listings/handler.go`, `listings/hook.go`, `listings/listing.go`, `listings/listings_test.go`, `mobile/src/screens/Give.tsx`, `mobile/src/ui.tsx`, `mobile/src/api.ts`, `mobile/src/listing.ts`, `mobile/src/words.ts`, `mobile/tests/listing.test.mts`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/X48t.md`

T9: a donor can delete a listing that is up, but only before someone grabs it.

- New route: `DELETE /api/listings/{id}`. Only the owner can delete. A grabbed listing answers 409 and stays. Another user's listing answers 404.
- The photo file goes too. Fan-out hears "expired", so a page post says the food is gone.
- In "Your listings", each free card has "Delete". A small popup asks first. A grabbed card says why it cannot be deleted.
- Tests: listings 29 -> 31, mobile 92 -> 93.

## 67. Review: every change to model fields is recorded

**When:** Oct 30 2026.
**Meta models:** none (no model change). The record feeds the self-improvement step.
**Files:** `inference/events.go`, `inference/labels.go`, `inference/labels_test.go`, `mobile/src/logic.ts`, `mobile/src/screens/Review.tsx`, `mobile/src/screens/Voice.tsx`, `mobile/src/voice.ts`, `mobile/tests/logic.test.mts`, `mobile/tests/voice.test.mts`, `mobile/web-dist/`
**Sources:** `docs/sources/X48u.md`

T9: every change a user makes to a field the models made must be recorded, for self-improvement.

- The label report of each scan now keeps every change in order: title, servings, a removed item, the pickup and the photo choice.
- Each change says how it was made: by hand, by voice, by text, or by Undo. Typing in a field counts as one change.
- The report keeps the chat lines and, for each item, the photo that posted and how its touch-up ended.
- The pickup text is never kept (it can be an address). Only the fact that it changed is kept. A chat line that named a pickup is kept without its words.
- The report always fits the service's limit; the item labels always stay.
- Tests: inference 96 -> 97, mobile 93 -> 96.

## 68. The translation work (LIF) joins the services branch

**When:** Oct 30 2026.
**Meta models:** Muse Spark Open, Muse Spark 1.3 (no change; the LIF entries 47-65 say how they are used).
**Files:** the LIF branch (`i18n/`, `qe/`, `docs/sources/L*.md`, `tools/lif_baseten_key.sh`) merged; `NEW_WORK.md`, `post/touchup_v2_test.go`
**Sources:** `docs/sources/X49a.md`

T9: the translation work of the LIF branch must run with the other services. First it joins the branch that is deployed.

- A real merge: the LIF commits keep their history.
- The only conflict was this file. It keeps every entry of both branches, in number order. The numbers both branches took (57, 63, 64, 65) stay on both entries.
- No service changes. The translation container comes in the next changes.
- Tests: i18n 53 and qe 7 (Python), added to the checks of every change. The auto touch-up tests wait longer (they failed under a parallel load).

## 69. Translation rounds run on the box

**When:** Oct 30 2026.
**Meta models:** Muse Spark Open (translation and a judge), Muse Spark 1.3 (a judge). MetricX-24 (Google, Apache-2.0) scores.
**Files:** `i18n/cmd/round/main.go`, `i18n/round/round.go`, `i18n/round/run.go`, `i18n/round/round_test.go`, `i18n/tree.go`, `i18n/tree_test.go`, `i18n/rung/score.go`, `i18n/rung/local_test.go`, `i18n/concepts.json`, `i18n/graph.json`, `qe/round.Dockerfile`, `qe/round.Dockerfile.dockerignore`, `tools/lif_round.sh`, `tools/v3rde_stack.sh`, `deploy/docker-compose.yml`, `deploy/Caddyfile`
**Sources:** `docs/sources/X49b.md`

T9: when the app changes, a translation round must rebuild the text graph, tag what was added or changed, and translate only that. It must run on the box with the other services.

- After a deploy, the box runs one round as a job. When the app text did not change, it stops at once.
- A round reads the app text of the deployed code, compares it with the last round, and makes a tag. Then it translates only the changed texts and the texts they reach in the graph.
- It uses one screen per call, two judges, and one fix round with the judges' reasons. MetricX scores each try.
- Texts that pass go live at `/i18n/<lang>.json`. A text that fails two times keeps its last good translation and goes on a review list.
- The job needs about 10 GB while MetricX scores, so it runs only when that much memory is free. It never fails a deploy.
- Also fixed: the post image now rebuilds when only the inference code changes.
- Tests: i18n 53 -> 64.

## 70. The translation image builds on the box

**When:** Oct 30 2026.
**Meta models:** none (no model change).
**Files:** `qe/round.Dockerfile`
**Sources:** `docs/sources/X49c.md`

The first build of the translation image on the box stopped. One test in the build reads the git history, and the build has no git history.

- The image build now skips that one test. Every other test still runs before the build.
- The SUP scripts still run that test with the whole repo.

## 71. Rescue: the food scanner's note helps choose the place

**When:** Oct 30 2026. Branch `rescue` (made from `services` at #70).
**Meta models:** Muse Spark 1.3 (the rescue draft; no new call).
**Files:** `post/post.go`, `post/rescue.go`, `post/rescue_source.go`, `post/rescue_test.go`, `post/rescue_hint_test.go`, `post/cmd/rescue/main.go`, `deploy/docker-compose.yml`, `docs/morpheus/post_api.md`
**Sources:** `docs/sources/R1.md`

T9: the food scan already says how the food is packed (`meal_condition`), but the rescue did not use it. Option A: give it to Muse Spark as a hint.

- The listing keeps the scanner's details since entry 51 ("A listing keeps what the scanner found"). Rescue now reads `meal_condition` and the description.
- Muse Spark gets one more line in the prompt: a scanner note. It says the note can be wrong and the photo decides.
- Muse still makes the decision. A sealed package is not always shelf-stable (some canned ham and seafood must stay cold).
- The hint is used only for a photo scan. Each scan's events line already records its route (barcode or vision) and the user. The listing keeps the scan's request id, so rescue reads that line. A barcode scan gives a fixed "ready to eat", so it gets no hint, also for listings posted before this change.
- No events line, another user's line, or no field: the same prompt as before, byte for byte.
- The events line records the hint and the route, so the self-improvement step can compare the scanner with Muse.
- Tests: post 67 -> 73.

## 72. A barcode post does not send a fixed food condition

**When:** Oct 30 2026. Branch `rescue`.
**Meta models:** none (no model change).
**Files:** `mobile/src/logic.ts`, `mobile/tests/logic.test.mts`, `mobile/web-dist/`
**Sources:** `docs/sources/R2.md`

T9: the rescue hint (entry 71) must not use the barcode answer's food condition. Rescue already checks the scan's route; this change also keeps the listing's own record true.

- A barcode answer always says "ready to eat". The barcode catalog has no such field, so the service sets a fixed value.
- The app now leaves that one field out of the listing's details for a barcode answer. A later screen that shows the details does not show a fixed value as a fact.
- A photo scan keeps the field. The other details do not change.
- Tests: mobile 96 -> 97.

## 73. Review: the item details, and a photo column on desktop

**When:** Oct 30 2026.
**Meta models:** none (no model change). The details come from the Muse Spark scan.
**Files:** `mobile/src/screens/Review.tsx`, `mobile/src/screens/Details.tsx` (new), `mobile/src/screens/TouchUp.tsx`, `mobile/src/details.ts` (new), `mobile/src/ui.tsx`, `mobile/src/theme.ts`, `mobile/src/words.ts`, `mobile/src/capture.ts`, `mobile/src/logic.ts`, `mobile/src/voice.ts`, `mobile/tests/details.test.mts` (new), `mobile/tests/logic.test.mts`, `mobile/tests/voice.test.mts`, `mobile/tests/style.test.mts`, `mobile/tests/touchup.test.mts`, `mobile/package.json`, `mobile/docs/style_guide.md`, `inference/labels.go`, `inference/events.go`, `inference/labels_test.go`, `mobile/web-dist/`
**Sources:** `docs/sources/X49d.md`

T9 approved the review layout mockup. The scan's details are now used, not thrown away.

- Each item has two pills side by side: "Touch up" and "Details", with icons.
- Details shows one short sentence, for example "Ready to eat, vegetarian, in bulk, about 1.2 kg.", then the description. The text types in. With "reduce motion" on, it shows at once.
- Phone: Details opens a small bottom sheet. Desktop: a third column.
- On desktop the scan photo fills the left column.
- The cost is never shown. Weight is in kg.
- The user can change the details (condition, food, portions, weight). Each change is recorded for self-improvement.
- Tests: mobile 96 -> 108; inference 97 (one more case).

## 73. Rescue: small listings get a drop-off with directions, bulk gets the email

**When:** Oct 31 2026. Branch `rescue`.
**Meta models:** Muse Spark 1.3 (the decision for a drop-off; the draft for bulk).
**Files:** `post/rescue.go`, `post/post.go`, `post/rescue_test.go`, `post/rescue_dropoff_test.go`, `post/cmd/rescue/main.go`, `deploy/docker-compose.yml`, `docs/morpheus/post_api.md`
**Sources:** `docs/sources/R3.md`

T9: instead of an email, the app suggests that the donor takes the food to one of 3 places near the pickup address. The email is only for bulk food.

- Bulk is 30 servings or more, or the scanner says "bulk" for a photo scan. Bulk keeps the email draft, as before.
- Any other listing is a drop-off. Muse Spark decides only perishable or shelf-stable, and writes no email. Each place gets a map directions link.
- The search uses the ZIP of the pickup address: the ZIP right after the state code ("Denver, CO 80219"). A house, suite or gate number is never read as a ZIP. The directions start at that address. When the address has no ZIP, the app's ZIP or location is used, as before.
- A small listing is no longer refused. The pickup address never goes into the events log.
- Tests: post 73 -> 79.

## 74. Rescue runs as a job, and a web search agent finds places when the directory cannot

**When:** Oct 31 2026. Branch `rescue`.
**Meta models:** Muse Spark 1.3 with the built-in `web_search` tool (the agent); Muse Spark 1.3 (the decision).
**Files:** `post/rescue.go`, `post/rescue_search.go`, `post/post.go`, `post/rescue_job_test.go`, `post/cmd/rescue/main.go`, `deploy/docker-compose.yml`, `docs/morpheus/post_api.md`
**Sources:** `docs/sources/R4.md`

T9: when the food directory finds nothing, a web search with an agent looks for places; a directory failure seeds that search. The rescue is a job that the app polls.

- The app starts the rescue and gets an answer at once. It then asks for the result each second or so, until the rescue is done or failed. A slow search no longer hits the 30-second gateway limit.
- When the directory finds no place, or does not answer, Muse Spark searches the web near the same ZIP. Its prompt says why the search runs.
- Each place from the web must have a name, an address and the web page that shows it. The answer says that a web search found the places, and asks the donor to check before going.
- If the directory and the search both fail, the rescue fails, and the donor can start it again.
- Each web search is counted in the cost.
- Tests: post 79 -> 90.

## 75. Distance on Receive, far items in gray, release a grab, and the pickup place

**When:** Oct 30 2026.
**Meta models:** none (no model change).
**Files:** `listings/place.go` (new), `listings/zcta.csv` (new), `listings/cmd/zctatable/main.go` (new), `listings/cmd/zctatable/main_test.go` (new), `listings/place_test.go` (new), `listings/store.go`, `listings/handler.go`, `listings/hook.go`, `listings/listing.go`, `listings/Dockerfile`, `listings/listings_test.go`, `post/fanout.go`, `post/fanout_test.go`, `mobile/src/place.ts` (new), `mobile/src/screens/Receive.tsx`, `mobile/src/screens/Review.tsx`, `mobile/src/api.ts`, `mobile/src/words.ts`, `mobile/tests/place.test.mts` (new), `mobile/package.json`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/X49e.md`

T9 asked what happens when a user grabs an item and is not in the area. Before this change, the list showed all food from all places, and a grab could not be given back. T9 approved the distance mockup (version 2).

- The giver's pickup place is the phone location, or a ZIP code when location is off. The server keeps it rounded to 0.01 mi and never sends it. The app gets only the distance, to 0.1 mi.
- The receiver uses the phone location, or a ZIP code. The receiver picks 1, 5 or 10 mi. Each card shows the distance.
- Food farther than the pick shows in gray, up to 25 mi. A grab of a gray item asks first, with the distance and the time left.
- "Your pickup" keeps the user's grabs. "Release it" gives a grab back until the timer ends. Fan-out removes "[CLAIMED]".
- New routes: `GET /api/listings?lat=&lng=` (or `?zip=`), `GET /api/listings/grabbed`, `POST /api/listings/{id}/release`.
- A phone build has no location module yet: it asks for the ZIP code.
- Tests: listings 31 -> 42, post 67 -> 68, mobile 108 -> 118.

## 75. Give: the Rescue pill and the rescue popup; the pickup field asks for the address

**When:** Oct 31 2026. Branch `rescue`.
**Meta models:** none in the app (the popup shows the rescue of entries 71-74: Muse Spark 1.3 and its web search).
**Files:** `mobile/src/rescue.ts`, `mobile/src/screens/Rescue.tsx`, `mobile/src/screens/Give.tsx`, `mobile/src/screens/Receive.tsx`, `mobile/src/api.ts`, `mobile/src/words.ts`, `mobile/src/ui.tsx`, `mobile/tests/rescue.test.mts`, `mobile/package.json`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/R5.md`

T9 approved the mockup on Oct 31 ("V3RDE Rescue Mockup"), with one change: a one-word red pill.

- On "Your listings", a card that expired with nobody grabbing it has the red pill "Rescue".
- The pill opens a popup. It starts the rescue, asks for the result each second, and shows up to 3 places with "Directions" and "Call". Bulk food also gets "Email" with the drafted message.
- When the pickup address has no ZIP code, the popup asks for one. It also shows when no place was found, and lets the donor try again after a failure.
- The pickup field now says "Pickup address and instructions", with the example "12 Main St, Denver, CO 80219. Front desk", so the rescue can read the ZIP code.
- From the independent check of the mockup: the links of an expired card keep full strength, the place links have a 44 pt touch area, and a dialog is never taller than the window allows.
- From the code review: a sign-out during the popup goes to sign-in; only the newest search can change the popup; nothing changes after it closes.
- Tests: mobile 97 -> 104.

## 76. Build fix: the post image copies the ZIP-center table

**When:** Oct 31 2026.
**Meta models:** none.
**Files:** `post/Dockerfile`, `post/Dockerfile.dockerignore`, `post/embed_test.go` (new)
**Sources:** `docs/sources/X49f.md`

The deploy of 75 stopped: the post image builds the listings package, and the package now embeds `listings/zcta.csv`, which the post build context did not have. The old containers started again.

- The post Dockerfile copies `listings/zcta.csv`, and its dockerignore lets it in.
- A new test checks that every file listings embeds is in the post image.
- Tests: post 68 -> 69.

## 77. The rescue work joins the services branch

**When:** Oct 31 2026.
**Meta models:** Muse Spark 1.3 and its web search (no change; entries 71-75 of the rescue branch say how they are used).
**Files:** the rescue branch (entries 71-75: `post/rescue*.go`, `post/cmd/rescue/main.go`, `mobile/src/rescue.ts`, `mobile/src/screens/Rescue.tsx`, …) merged; `NEW_WORK.md`, `mobile/package.json`, `mobile/docs/style_guide.md`, `mobile/src/api.ts`, `mobile/src/screens/Receive.tsx`, `mobile/web-dist/`
**Sources:** `docs/sources/X49g.md`

T9: the rescue work joins the branch that is deployed, the same way as the translation work (entry 68). The merge was made again on top of entries 75 and 76 (distance), so it is entry 77.

- A real merge: the rescue commits keep their history.
- This file keeps every entry of both branches, in number order. The numbers both branches took (73 and 75) stay on both entries, the services entry first.
- The test list and the style guide keep the lines of both branches. The app API imports both the place rules and the rescue rules.
- Receive keeps the distance screen (75). From the rescue side (75): on a card that is gone, the pickup line fades with the card text, and the actions stay at full strength.
- The web build is made again from the merged code.
- Tests: post 69 + 23 = 92; mobile 118 + 8 = 126; the other modules as before.

## 78. Give cards: Delete on the photo, a small Rescue pill

**When:** Oct 31 2026.
**Meta models:** none.
**Files:** `mobile/src/ui.tsx`, `mobile/src/screens/Give.tsx`, `mobile/tests/listing.test.mts`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/X49h.md`

T9 approved the Give and review mockup (version 2).

- Delete is an X in a round, frosted button at the top right of the photo. A grabbed card has no X.
- Rescue is a red pill with the size and the text of the time pill, under it.
- The frosted layer now takes the corners of its part (a round button showed a square blur on the web).
- Tests: mobile 126 -> 127.

## 78. An About page, and an About link on the sign-in screen

**When:** Oct 30 2026.
**Branch:** `about`, made from `services` at entry 77, like the rescue branch. It joins `services` by a merge later.
**Meta models:** none (a page that explains how the app uses them).
**Files:** `mobile/public/about/index.html` (new), `mobile/tests/about.test.mts` (new), `mobile/src/theme.ts`, `mobile/src/signin.ts`, `mobile/src/screens/Login.tsx`, `mobile/tools/web/serve.mjs`, `mobile/tests/web.test.mts`, `mobile/package.json`, `mobile/docs/style_guide.md`, `mobile/docs/web.md`, `mobile/web-dist/`
**Sources:** `docs/sources/A1.md`

T9 asked for a web page about the app: what it is for, who uses it (three roles), and how it is built. T9 approved the mockup ("V3RDE About Page Mockup", version 3) with these choices: the address `icarus.thehungreeapp.com/about/`, one word on the sign-in screen, a 22 pt title, the bread drawing for food programs, no Facebook for now, "Food DB" with no count, and the doodle wallpaper behind the whole page.

- A static page next to the app, not in the app bundle. The web build copies it, and the edge serves it at `/about/`.
- Sections: what V3RDE does, the three roles (Give, Receive, food programs), how it works in 7 steps, how it is built (the phone or browser, our server, Meta's cloud), measured numbers (entry 4), and what is new in the Hackathon.
- The page takes every colour, size, text style and doodle value from the app's tokens. The backdrop is the app's doodle wallpaper: the same drawings, rule 30 and seed. It moves one row each second and stands still with "reduce motion". Light and dark mode.
- A new token: `type.display` (22 / 28), the page title.
- The sign-in screen has "About" at the right of "Forgot password?". It opens the page in the same tab.
- The local web server serves a folder page with the trailing slash, as the edge does.
- "Watch the demo" stays hidden until the video has an address.
- Tests: mobile 126 -> 135.

## 79. Review: More holds Touch up and the details; the chat in the third column

**When:** Oct 31 2026.
**Meta models:** none (the details come from the Muse Spark scan).
**Files:** `mobile/src/screens/Review.tsx`, `mobile/src/screens/Details.tsx`, `mobile/src/screens/TouchUp.tsx`, `mobile/src/ui.tsx`, `mobile/src/theme.ts`, `mobile/src/words.ts`, `mobile/tests/details.test.mts`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/X49i.md`

T9 approved the Give and review mockup (version 2).

- Each item has one wide "More" button. More holds Touch up, and the details show at once under it. There is no Details button. Change edits the details in place.
- Desktop: More opens in the card. Phone: More opens the bottom sheet.
- Desktop (widest dialog): the third column holds the talk-or-type chat, frosted, 320 pt wide (token `size.chatColumn`, before `size.detailsColumn` 260).
- The frosted X and the small Rescue pill (78) get the hover state of the other buttons.
- Tests: mobile 127 -> 128.

## 80. The pickup address: one suggestion, then the map; rescue uses it

**When:** Oct 31 2026.
**Meta models:** none. Google Maps Platform (Places Autocomplete, New; Maps JavaScript): project `v3rde-maps`, a browser key limited to the app's site and 3 Maps APIs.
**Files:** `listings/listing.go`, `listings/store.go`, `listings/handler.go`, `listings/cmd/listings/main.go`, `listings/place_test.go`, `post/post.go`, `post/rescue.go`, `post/rescue_test.go`, `post/rescue_dropoff_test.go`, `deploy/docker-compose.yml`, `tools/v3rde_stack.sh`, `mobile/src/maps.ts` (new), `mobile/src/screens/Pickup.tsx` (new), `mobile/src/screens/Review.tsx`, `mobile/src/screens/Receive.tsx`, `mobile/src/screens/TouchUp.tsx`, `mobile/src/api.ts`, `mobile/src/capture.ts`, `mobile/src/words.ts`, `mobile/src/theme.ts`, `mobile/src/ui.tsx`, `mobile/tests/maps.test.mts` (new), `mobile/package.json`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/X49j.md`

T9 asked why the review asked for a ZIP and an address with a ZIP, and for a map. T9 approved the mockup (version 2).

- The giver types the pickup address. One suggestion shows. A tap locks the address, and a small map shows the pin.
- The text box holds only the instructions at the door, and it may be empty.
- The listing keeps the address (text, ZIP, point). Only the owner and the user who grabbed it get it, with the instructions. Before a grab, receivers see only the distance.
- Rescue takes the ZIP (or the point, when the address has no ZIP), and the directions start from the chosen address.
- The key is not in the app files: the listings service reads it on the box (x49n) and gives it to the app. Without a key the review shows the place row of 75.
- If Maps does not work, the review shows the place row of 75 again.
- Tests: listings 42 -> 44, post 92 -> 94, mobile 128 -> 135.

## 81. No ZIP code; the pickup address in any country; a map that moves

**When:** Oct 31 2026.
**Meta models:** none. Google Places Autocomplete (New), Maps JavaScript and Geocoding (the browser key of 80).
**Files:** `mobile/src/maps.ts`, `mobile/src/place.ts`, `mobile/src/api.ts`, `mobile/src/rescue.ts`, `mobile/src/words.ts`, `mobile/src/theme.ts`, `mobile/src/screens/Pickup.tsx`, `mobile/src/screens/Review.tsx`, `mobile/src/screens/Receive.tsx`, `mobile/src/screens/Rescue.tsx`, `mobile/src/screens/Voice.tsx`, `mobile/docs/style_guide.md`, `mobile/tests/maps.test.mts`, `mobile/tests/place.test.mts`, `mobile/tests/rescue.test.mts`, `post/rescue_search.go`, `post/rescue_job_test.go`, `mobile/web-dist/`
**Sources:** `docs/sources/X49q.md`

T9 (testing with users in Nigeria): "nigerian addresses dont work", "lets drop zip code" (everywhere), and the tester feedback on the map. T9 approved the "V3RDE No-ZIP Mockup" v2 and v3.

- The address search has no country limit. Google puts addresses near the user first.
- No screen asks for a ZIP code. The listing keeps the address text and its point.
- Receive: "Not now", or location off, shows all food with no distance. The review without the address search takes the location at Post. Rescue of a listing with no point asks for the giver's location. Rescue's web search names no country.
- The pickup address: "Use my current location" fills it from the phone location. The map moves; the pin stays in its centre. When the pin moves more than 20 m, the address follows it ("Finding the address…"; Post waits for it). While the listing posts, the map holds still.
- A tap on the chosen address opens the change: the block moves up and the map grows to 320. A tap outside closes it. The "choose" error is under the field.
- The chat box hint changes every 4 s while the chat is empty.
- Open: the services still accept a ZIP from an old app and keep the US ZIP table.
- Tests: mobile 135 -> 136, post 94 -> 95.

## 81. The About page: the problem, and how it is built in drawings

**When:** Oct 30 2026.
**Branch:** `about` (after entry 78).
**Meta models:** none (the page explains how the app uses them).
**Files:** `mobile/public/about/index.html`, `mobile/tests/about.test.mts`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/A2.md`

T9 found the page dense and asked for an architecture drawing. Over versions 5 to 8 of the mockup T9 asked for fewer words, the problem, six languages and Rescue, drawings read from the code in plain words, one drawing always open with more on a tap, a left-to-right then top-to-bottom reading order, and a "How it is built" that is not the same as "How it works". T9 approved version 8 ("lock this in").

- A new section: the problem. Posting food was manual work (typing, counting, a good photo); Meta's AI models now do it. Six languages. Rescue, so food nobody grabbed still finds a home.
- Fewer words: one line for each role, five short steps, short captions.
- How it is built: one drawing that is always open (the three parts and what each holds) and three that open on a tap: posting food (12 steps), Rescue (7) and six languages (5). Each box says where it runs. A red border marks a Meta AI model.
- Each drawing reads left to right, then down. No arrow points back.
- The tests read the page: no arrow points left or up, the steps are in order, each Meta model box has the red border, the six languages are the ones in `i18n/languages.json`, and the models are the ones in this file.
- Tests: mobile 135 -> 139.

## 82. "The food scanner is busy": a list answer gives every item; one message for each cause

**When:** Oct 31 2026.
**Meta models:** Muse Spark (muse-spark-1.3), the whole-photo call; no new call.
**Files:** `server/contract.go`, `server/server_test.go`, `inference/engine.go`, `inference/events.go`, `inference/scanjob.go`, `inference/service.go`, `inference/engine_test.go`, `inference/scanjob_test.go`, `mobile/src/logic.ts`, `mobile/src/api.ts`, `mobile/src/screens/Capture.tsx`, `mobile/tests/logic.test.mts`, `mobile/docs/web.md`, `mobile/web-dist/`
**Sources:** `docs/sources/X49r.md`

T9 (testing with users in Nigeria): a tester's scans, and T9's own, showed "The food scanner is busy". The event log showed 3 × 502 "no item passed the contract" on one photo of 3 products. A test with the stored photo (5 calls) showed why: in 4 of 5 answers the model gave a JSON list of the 3 items, and the server read only one JSON object. T9 approved the "V3RDE Scan Errors Mockup".

- A list answer gives one item for each valid object (at most 12); the same name twice is one item. Words around the list and an `{"items": [...]}` answer are read too. One object reads as before. The prompt does not change.
- When an answer fails the check, the event keeps the reason and the first 300 characters of the model's text (at most 5 reasons).
- The app shows one message for each cause: the scanner could not read the photo, no connection, no answer in time, the scan stopped, a server error, no items. "Busy" shows only for a real busy answer.
- An upload that finds no connection is sent again (at most 3 tries, 2 s apart, inside 75 s). A slow upload is not sent again. The service gives a photo it already scans for the same user (or scanned well in the last 10 s) the same job, so a second try is not a second paid scan.
- Limit: when SAM finds no box, each item of a list answer uses the whole photo.
- Tests: server 36 -> 38, inference 97 -> 101, mobile 136 -> 138.

## 82. Rescue: the end of a job frees the listing at once

**When:** Oct 31 2026.
**Branch:** `reach`, made from `services` at entry 80, like the rescue branch. It joins `services` by a merge later.
**Meta models:** none (no model change).
**Files:** `post/rescue.go`, `post/rescue_job_test.go`, `post/wait_events_test.go` (new), `post/touchup_test.go`, `post/touchup_v2_test.go`
**Sources:** `docs/sources/N1.md`

A post test failed now and then (1 run in 6). When a rescue job ended, the listing stayed marked "busy" for a short time after the result was visible. A new rescue in that moment got 409.

- The job's last state and the free listing are now one step (one lock).
- A failed save still marks the job failed in memory, as before.
- Three job tests wait for the events line (at most 5 s): under load they read the file before the job wrote it.
- Tests: post 94 -> 95.

## 83. Password reset by email link

**When:** Oct 31 2026.
**Meta models:** none. Google Workspace SMTP (`smtp.gmail.com:465`, TLS from the first byte, an app password of the sender mailbox).
**Files:** `auth/reset.go` (new), `auth/reset_test.go` (new), `auth/store.go`, `auth/handler.go`, `auth/auth.go`, `auth/cmd/auth/main.go`, `auth/Dockerfile`, `deploy/docker-compose.yml`, `tools/v3rde_stack.sh`
**Sources:** `docs/sources/X49o.md`

T9: "reset password does not work". The app had the reset screens (UI 9), but the auth service had no reset routes and no mail sender.

- `POST /api/auth/reset` answers 202 for every valid email. When an account has the email, a link `<app>/reset#token=...` goes to it. The link works for 30 minutes, one time, and a new request replaces it.
- `POST /api/auth/reset/confirm` sets the new password, ends every other session of the user and signs the user in.
- The mail goes out after the answer, through the Google Workspace mailbox (x49p puts its address and app password on the box). Without them the reset routes answer 503.
- Limits: 5 reset requests an hour per address, 3 mails in 15 minutes per email, and 300 mails a day in all (the mailbox may send about 2,000).
- The account lookup, the token and the mail happen after the answer, so the answer time is the same for every email.
- The data file has only the hash of the token. The logs have only the user id.
- The relay port is 465: the network of the setup Mac gives no answer on 587.
- Tests: auth 22 -> 32.

## 83. A barcode scan gets the product photo

**When:** Oct 31 2026.
**Branch:** `reach`.
**Meta models:** none (no model call on the barcode route).
**Files:** `inference/stock.go` (new), `inference/stock_test.go` (new), `inference/service.go`, `inference/clients.go`, `inference/photos.go`, `inference/events.go`, `inference/cmd/inventory/main.go`, `inference/service_test.go`, `listings/handler.go`, `listings/listing.go`, `listings/store.go`, `listings/listings_test.go`, `deploy/docker-compose.yml`
**Sources:** `docs/sources/N2.md`

T9 asked for a stock photo when a user scans a barcode ("Stock photo only"). The photos are the Open Food Facts front photos made in the Hackathon (22,322 products, 800 px).

- After a barcode hit, the listing photo is the product's white cutout, else the photo as Open Food Facts has it. No photo for the product: the camera shot, as before. The camera shot is still stored.
- Only the app's route (`/api/analyze`, session). `/v1/analyze` does not change.
- Open Food Facts photos are CC BY-SA. The answer item and the listing carry `photo_credit` ("Open Food Facts, CC BY-SA"). The app interface does not change in this entry (T9).
- The photos are a read-only folder on the box (`/opt/v3rde/assets/stock`), mounted into analyze. An empty folder gives the camera shot.
- A touch-up of a stock photo keeps the credit.
- Tests: inference 97 -> 100, listings 44 -> 46.

## 84. Every new listing goes to the Facebook Page

**When:** Oct 31 2026.
**Branch:** `reach`.
**Meta models:** Muse Spark 1.3 (no change: fan-out, entry 20, writes the post).
**Files:** `listings/share.go` (new), `listings/share_test.go` (new), `listings/handler.go`, `listings/cmd/listings/main.go`, `deploy/docker-compose.yml`
**Sources:** `docs/sources/N3.md`

T9 asked for a Facebook post of each food item, with a link to the app ("Link in the post only"), started by the server for every new listing.

- After a new listing, the listings service asks fan-out to post it, with the giver's own session. Fan-out keeps its owner check, rate limit and daily caps.
- The post ends with the claim link `https://icarus.thehungreeapp.com/l/<listing_id>` (entry 85 opens it in the app).
- Test accounts are never posted.
- Until the Facebook Page is set (`FANOUT_PAGE_ID` and its token), fan-out answers "disabled" before any model call: no cost.
- No answer, 429 or a server error: it tries again (3 more times in 85 s). Fan-out never makes a second post.
- Open before the Page is set: the giver's photo goes to the public Page with no photo check (fan-out checks only the text); a grab in the moment before the post starts leaves the post open until the timer sweep.
- Tests: listings 46 -> 48.

## 85. A claim link opens Receive after sign-in

**When:** Oct 31 2026.
**Branch:** `reach`.
**Meta models:** none.
**Files:** `mobile/App.tsx`, `mobile/src/signin.ts`, `mobile/tests/reset.test.mts`, `mobile/web-dist/`
**Sources:** `docs/sources/N4.md`

T9: a person who taps the link in the Facebook post goes to the app, and after sign-up or sign-in the app skips the role sheet ("Yes, skip to Receive").

- The link is `icarus.thehungreeapp.com/l/<listing_id>` (on a phone, `v3rde://l/<listing_id>`).
- After sign-in or sign-up, or at once with an open session, the app opens Receive. The listing shows in the list as today. The address goes back to `/`.
- Any other link starts the app as before. No new screen. Log out clears a claim that waits.
- On a phone the https link opens the browser (the web app), because the phone app has no associated domains yet.
- Tests: mobile 135 -> 136.

## 86. Password reset by an email link: the server part

**When:** Oct 31 2026.
**Branch:** `reach`.
**Meta models:** none.
**Files:** `auth/reset.go` (new), `auth/reset_test.go` (new), `auth/handler.go`, `auth/store.go`, `auth/auth.go`, `auth/cmd/auth/main.go`, `deploy/docker-compose.yml`
**Sources:** `docs/sources/N5.md`

The app already has "Forgot password?" (UI 9), but the server had no reset routes. T9: "we need to wire in the email link to work".

- `POST /api/auth/reset`: always 202, also when no account has the email. For an account: a one-time link that works for 30 minutes; a new request replaces the earlier link.
- `POST /api/auth/reset/confirm`: the new password, then the user is signed in. All other sessions of the user end.
- No email sender is set up yet (T9). The emails go to a folder on the auth volume, readable only by the server's root user, and are removed after 30 minutes. Amazon SES is the next step.
- The logs have the user id only, never the email, the token or the password.
- Tests: auth 22 -> 27.

## 87. The reach work joins the services branch

**When:** Oct 31 2026.
**Meta models:** none new (entries 82-86 of reach say what they use).
**Files:** the reach branch (entries 82-86 of reach: `post/rescue.go`, `inference/stock.go`, `listings/share.go`, `mobile/App.tsx`, `mobile/src/signin.ts`, ...) merged; `NEW_WORK.md`, `deploy/docker-compose.yml`, `post/rescue_job_test.go`, `auth/` (kept as entry 83 of services), `mobile/web-dist/`
**Sources:** `docs/sources/N6.md`

T9: the reach work (the barcode product photo, the Facebook post and the claim link, the rescue lock) joins the branch that is deployed, the same way as the earlier merges.

- A real merge: the reach commits keep their history.
- This file keeps every entry of both branches, in number order. The numbers both took (82, 83) stay on both entries, the services entry first.
- Password reset: both branches added it. The auth code of services (entry 83: it sends the mail, and it is tested on the box) stays; none of the code of reach entry 86 is taken. Entry 86 stays here as history.
- The compose file keeps the auth lines of services and the new lines of reach (the stock photos for analyze, the share hook of listings, the claim link base of fan-out). The rescue test file keeps the new tests of both branches.
- The web build is made again from the merged code.
- No other change. Tests: inference 104, listings 48, post 96, auth 32, mobile 139; the other modules as before.

## 88. The new listings go to the V3RDE Facebook Page

**When:** Oct 31 2026.
**Meta models:** Muse Spark 1.3 writes the post (fan-out, no change); the Facebook Pages API publishes it.
**Files:** `deploy/docker-compose.yml`, `tools/v3rde_stack.sh`
**Sources:** `docs/sources/X49t.md`

T9 made the Facebook Page "V3RDE" and a Meta app for it, and chose to start the posts with this change.

- Fan-out gets the Page token (a file on the box, made by the fb2 script) and the ID of the V3RDE Page. Every new listing of a real account now goes to the Page (entry 84 of reach).
- The token does not expire. An empty token file, or an empty `FANOUT_PAGE_ID`, turns the posts off.
- The deploy check now says whether fan-out posts to the Page, and checks that the token is not in the container settings.
- Open: no photo check before a public post; the Meta app is in development mode (the first post shows who can see it).

## 89. The About page joins the services branch

**When:** Oct 31 2026.
**Meta models:** none (the page describes the models of the other entries).
**Files:** the about branch (entries 78 and 81 of about: `mobile/public/about/index.html`, the About link, `type.display`, `mobile/tests/about.test.mts`, ...) merged; `NEW_WORK.md`, `mobile/package.json`, `mobile/web-dist/`
**Sources:** `docs/sources/A4.md`

T9: the About page goes to the branch that is deployed, the same way as the earlier merges.

- A real merge: the about commits keep their history.
- This file keeps every entry of both branches, in number order. The numbers both took (78, 81) stay on both entries, the services entry first. The test list keeps both sides.
- Two fixes on the page, from later work: the Rescue drawing now says "Uses the pickup address on the map, or your location" (no ZIP code since entry 81), and the footer names the source of the product photos of a barcode scan (Open Food Facts, CC BY-SA; entry 83 of reach). A page test checks both.
- The web build is made again from the merged code.
- Tests: mobile 153 (139 + 12 page tests and 1 web test of the About page + 1 new page test); the Go modules as before.

## 90. The Facebook post of an ended listing says "[UNAVAILABLE]"

**When:** Oct 31 2026.
**Meta models:** none new (fan-out edits the post text).
**Files:** `post/fanout.go`, `post/cmd/fanout/main.go`, `post/fanout_test.go`
**Sources:** `docs/sources/X49v.md`

T9: "[EXPIRED]" sounds like the food went bad.

- When the listing's time ends, fan-out now puts "[UNAVAILABLE]" in front of the Facebook post (it was "[EXPIRED]"). "[CLAIMED]" stays for a grab.
- Tests: post 96 -> 97.

## 91. The keyboard does not cover the field; Post goes to the address; clearer sign-in

**When:** Nov 1 2026.
**Meta models:** none.
**Files:** `mobile/src/keyboard.ts` (new), `mobile/src/ui.tsx`, `mobile/src/theme.ts`, `mobile/src/screens/AuthFrame.tsx`, `mobile/src/screens/Login.tsx`, `mobile/src/screens/Pickup.tsx`, `mobile/src/screens/Reset.tsx`, `mobile/src/screens/Review.tsx`, `mobile/src/signin.ts`, `mobile/docs/style_guide.md`, `mobile/package.json`, `mobile/tests/keyboard.test.mts` (new), `mobile/tests/maps.test.mts`, `mobile/tests/signin.test.mts`, `auth/auth.go`, `auth/store.go`, `auth/handler.go`, `auth/reset.go`, `auth/auth_test.go`, `mobile/web-dist/`
**Sources:** `docs/sources/X49w.md`

T9, from the tester feedback; T9 approved the "V3RDE Batch 1 Mockup" v3.

- On a phone browser the keyboard no longer covers the field: the sheet and the sign-in button move up, and the field you type in comes into view. When the keyboard closes, the screen is as before.
- Post with no pickup address scrolls the page to the address field, with the error under it.
- "Use my current location" is a round target icon inside the address field.
- Sign-in: an unknown email shows "No account uses this email." and "Create an account"; a wrong password shows "Wrong password." and "Forgot password?". Before a try only "About" shows.
- A new account's email must have a real form (name@domain.ending, any country). Sign-in and the password reset keep the earlier check, so older accounts still sign in and get a reset link.
- Tests: mobile 153 -> 159, auth 32 -> 33.


## 92. One photo with many barcodes

**When:** Nov 1 2026.
**Meta models:** SAM 3.1 and Muse Spark 1.3 for the other food in the photo (as the scan); no new model.
**Files:** `inference/barcodes.go` (new), `inference/service.go`, `inference/events.go`, `inference/barcodes_test.go` (new), `inference/service_test.go`, `mobile/src/capture.ts`, `mobile/src/api.ts`, `mobile/src/logic.ts`, `mobile/src/words.ts`, `mobile/src/screens/Capture.tsx`, `mobile/tests/capture.test.mts`, `mobile/tests/logic.test.mts`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/I1.md`

T9 chose "A: one photo with many barcodes" and "barcodes + vision" for a photo with codes and other food; T9 approved the "V3RDE Items Mockup" v3.

- After the shutter (or an upload), the app reads every retail barcode in the still photo (web; at most 2 s). "Use this photo?" says "2 barcodes found". The codes and their boxes go with the photo (`barcodes`, at most 24). One live barcode alone is the fast path, as before.
- The service looks up every code at the same time. Each catalog product is one item, with its stock photo when there is one.
- The scanner still runs for the other food in the photo (also for a plate with one packet). The smallest box of the scanner that holds a barcode is that product, so it is not listed twice.
- If the scanner cannot answer (busy, the daily limit, not food, an error), the barcode items still answer.
- A product that is already in your listings is left out, as before. The events line keeps every code and what it became.
- Branch `items` (not deployed yet).
- Tests: inference 104 -> 116, mobile 159 -> 162.

## 93. Find a missed item: the find service

**When:** Nov 1 2026.
**Meta models:** SAM 3.1 (the item's word as its text concept, or the drawn box as its box prompt) and Muse Spark 1.3 (names what SAM found).
**Files:** `inference/find.go` (new), `inference/findengine.go` (new), `inference/engine.go`, `inference/service.go`, `inference/events.go`, `inference/find_test.go` (new)
**Sources:** `docs/sources/I3.md`

T9 chose "2a, then 2b if not found"; T9 approved the "V3RDE Items Mockup" v3.

- `POST /api/analyze/find`: after a scan, its owner asks for an item that the list does not have. 2a: its name ("rice"); SAM looks for that word in the scan's photo. 2b: a box drawn on the photo and its name; SAM takes the box.
- Muse names what SAM found. The user's word goes to Muse as a quoted hint, never as an instruction.
- A box that is already on the list (the scan's or an earlier find's) is not found again, also a drawn one. At most 3 new items; each has a crop of the photo as its listing photo. A model error is an error, not "not found".
- 2b: if Muse cannot name the box as food, the item is the box with the user's name (T9: add it anyway).
- Nothing new found: `not_found` (the app then offers "Draw a box around it").
- Only the scan's owner; the rate limit, busy and the daily limit are as for a scan. The events line has step "find".
- Tests: inference 116 -> 121.

## 94. Find a missed item from the chat

**When:** Nov 1 2026.
**Meta models:** Muse Spark 1.3 (the chat's mapping now also names missed items); then SAM 3.1 and Muse Spark through the find service (#93).
**Files:** `post/voice.go`, `post/voice_test.go`, `inference/labels.go`, `inference/labels_test.go`, `mobile/src/logic.ts`, `mobile/tests/logic.test.mts`, `mobile/src/voice.ts`, `mobile/src/api.ts`, `mobile/src/words.ts`, `mobile/src/screens/Voice.tsx`, `mobile/src/screens/Review.tsx`, `mobile/tests/voice.test.mts`, `mobile/tests/maps.test.mts`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/I4.md`

T9 chose "2a, then 2b if not found"; T9 approved the "V3RDE Items Mockup" v3.

- In the review, say or type that an item is missing ("you missed the rice"). The chat's answer has `find: ["rice"]` (at most 3 names; only when there is a scan; a name already on the list is left out).
- The app asks the find service for each name. The chat shows "Looking for rice in the photo…", then "Rice is on the list now." or "No rice found in the photo.".
- A found item joins the list with its crop and "Found from your words" on its card. A one-item review becomes the list. Post waits while a find is out.
- The chat hint has a fourth example: "Try “rice is missing”".
- The label report logs a found item as a "found" edit (the self-improvement data).
- Tests: post 97 -> 98, inference 121 -> 122, mobile 162 -> 165.

