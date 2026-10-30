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

## 45. The food gate uses 2 CPU threads

**When:** Oct 29 2026.
**Meta models:** PE-Core-S16-384 gate, as in entry 43 (no model change).
**Files:** `deploy/docker-compose.yml`
**Sources:** `docs/sources/X46a.md`

The box bench (X45b) measured the gate on LiteRT 2.3.0 on the box.

- With 2 threads one call takes 595 ms; with 1 thread it takes 1,071 ms (median, 50 photos).
- When two calls run at the same time, both settings take about 1.1 s each.
- The gate now uses 2 threads. No image is built again: only the gate container starts again.

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

## 49. Each caller gets its own rate limit

**When:** Oct 29 2026.
**Meta models:** none (no model change).
**Files:** `deploy/Caddyfile`
**Sources:** `docs/sources/X48b.md`

Through API Gateway every request seemed to come from the same address, so all callers shared one rate limit.

- The SME set API Gateway to send the caller's real address in the header `X-Client-Ip`. API Gateway replaces any value the caller sends.
- The edge reads that address first, then the old header, and gives it to every service.
- The edge does not pass `X-Client-Ip` on to the services.

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

## 56. Touch-up v2: the item is cut out alone

**When:** Oct 30 2026.
**Meta models:** SAM 3.1, Muse Image 1.0 (no change). Only in the comparison tool.
**Files:** `inference/touchupv2.go`, `inference/touchupv2_test.go`, `inference/cmd/touchupcompare/main.go`
**Sources:** `docs/sources/X48i.md`

On the last page the cut kept neighbours of the same kind: the tomatoes next to the bananas, the plantains next to the tomatoes, the next cereal boxes.

- The scan knows the item's box. The item is cut again from the upload with that box, and only the SAM mask that matches the box is kept (plus masks inside it).
- The scan's name of the item is asked first, then "food", "fruit" and the others.
- Tests: inference 90 -> 92.

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

## 63. Touch-up: one touch-up for every mode

**When:** Oct 30 2026.
**Meta models:** Muse Image 1.0, SAM 3.1 (no change).
**Files:** `inference/touchupauto.go`, `post/touchup.go`, `post/touchup_v2_test.go`
**Sources:** `docs/sources/X48q.md`

T9: the app has one touch-up, the auto one.

- "Light" now gives the same result as "Background": the Muse edit, the scene rule's mask and the blurred background.
- The "Light" path (the Muse edit only) is removed.
- Tests: post 66 (one test changed).

## 64. Touch-up: one link in the app

**When:** Oct 30 2026.
**Meta models:** none (no model change).
**Files:** `mobile/src/screens/TouchUp.tsx`, `mobile/src/screens/Review.tsx`, `mobile/src/api.ts`, `mobile/src/words.ts`, `mobile/tests/touchup.test.mts`, `mobile/docs/style_guide.md`, `mobile/web-dist/`
**Sources:** `docs/sources/X48r.md`

T9: one touch-up button, not two.

- Each item now has one link, "Touch up photo", in place of "Better light" and "Plain background".
- The link starts the auto touch-up (the Muse edit, the scene rule's mask, the blurred background).
- Tests: mobile 89 -> 90.

