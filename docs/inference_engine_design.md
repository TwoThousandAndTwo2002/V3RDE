# Icarus — inference_engine design addendum, 2026-10-26

Follows `docs/icarus_addendum_2026-10-26.md`. This document records **what** the multi-item scan pipeline does, **why**, and **the evidence** for each choice, so that a later change can check against it. Status: approved by T9 (2026-10-26); updated 2026-10-27 with runs v4 and the SAM input tests (D19–D23), and with real phone photos (X30b, D24–D28). Every decision lists its sources (§7); from X30 on, each change also carries `docs/sources/XNN.md` (D23).

Hackathon feature: #1 multi-item scan (SAM 3.1 + Muse Spark). Work folder before the commit: `~/Documents/Morpheus_transfer/x30/`. Target commit: X30 on `hackathon/meta-2026`.

---

## 1. Goal and fixed requirements (T9)

| # | Requirement |
|---|---|
| R1 | `inference_engine` takes over from analyze. For a photo of **one** item it must give **exactly** analyze's answer (same prompt, same bytes, same strict 8-field contract). |
| R2 | Same input formats (multipart / JSON base64) and the same auth (bearer token file) as analyze. `/v1/analyze` and `/api/analyze` do not change. |
| R3 | Go service, module `inference/`, port 8087, `POST /v1/inventory`. The contract is imported from `server` (replace directive), so the two cannot drift. |
| R4 | The top-level answer is the largest group. `items[]` holds every group. |
| R5 | `number_of_serving` = 1 per box, so a group's count is its servings. **Exception: a tray of cooked food (D5).** |
| R6 | Every call and every photo reports its cost (financial feasibility is part of the pitch). The telemetry sidecar and events are updated (X31). |
| R7 | Never list the same item twice (dedup within a photo; dedup against live listings in X31). |
| R8 | The app must show what was found (boxes or shaded areas). The answer carries the boxes for this. |

---

## 2. The pipeline (auto v2, final)

```
photo
  │
  ├─ barcode? ── phone decodes on device (server gets the number only) ──► catalog item
  │
  ▼
food gate (PE-Core-S16-384, local, ~0.3 s, $0) ── not food ──► "Try a food item" ($0)
  │ food
  ▼
SAM 3.1 "food"  ║  SAM 3.1 "package"  ║  SAM 3.1 "food tray"  ║  resize for Muse   ← all in parallel
  │
  ▼
merge: containers (package/tray; same object = IoU > 0.7; a holder of 2+ containers is dropped)
       + every food box NOT inside a container
  │
  ├─ 0 boxes ──► agent (Muse picks product concepts, SAM is its tool)
  │                 └─ agent finds 0 or 1 box ──► whole call (= analyze)
  ├─ 1 box ────► whole call (= analyze, byte-for-byte)
  └─ 2+ boxes ─► resize to Muse's 4,096-token limit
                 draw numbered marks + give 0–1000 coordinates
                 ONE Muse call (effort low), or 3 calls in parallel when 12+ boxes are named:
                 every box → 8 fields + same_as + food + tray (pan, depth, dish) + point
                 boxes without a valid answer → ONE retry call with only those boxes
  ▼
group (same_as, then equal names) → count; food:false → "not listed"
size levels: ≥96 px named · 64–96 px named + "check" · <64 px counted, not named ("closer photo")
trays: one listing each; servings from the pan-size table
  ▼
items[] (+ 0–1000 boxes for the screen) · not_listed[] · cost per call / photo
```

---

## 3. Design decisions

Each decision: **what**, **why**, **evidence**. Runs: v1 = 6 photos (`probe_out/probe_20261026T195534Z.json`), v2 = 17 photos (`probe_out_v2/probe_20261026T210803Z.json`), v3 = 23 photos with a hand check of 294 boxes (`probe_out_v3/probe_20261026T220918Z.json`).

### D1. The food gate runs first and keeps its rule (option 1c)
- **What:** PE-Core-S16-384 (Meta, Apache-2.0, fp16 weights, 48.1 MB) decides food / not food before any API call. A "not food" photo stops there ($0). Later, shelf and pantry photos go into the gate head training.
- **Why:** it saves API cost on non-food photos, and it is the only check that can move to the phone (feature #12). A user whose item is missed can take a close-up of that item, which uses the one-item path.
- **Evidence:** HELDOUT: AUROC 0.981, catch 0.831, false retake 1.6% (X29). On shelf photos the gate refused 3/17 (v2) and 5/23 (v3) food photos, all just under the threshold (p 0.15–0.25 vs 0.254). This is the known weak point (§5).
- **Rejected:** "if the gate says not food, check with SAM" (costs $0.0025 per refusal and moves the gate's job to the cloud).

### D2. Barcodes are read on the phone only
- **What:** the server gets the barcode number from the phone scanner, as in the prior product (ML Kit on live camera frames). No server decoder now.
- **Why:** shelf photos have no barcodes that can be read; a server decoder found nothing to test. Server-side decode per box is a later improvement.

### D3. SAM counts, Muse names
- **What:** SAM 3.1 finds and counts every instance; Muse Spark 1.3 names and describes.
- **Why:** SAM 3 counts better than multimodal models: CountBench 95.6% vs 92.4% for the best MLLM (Ultralytics SAM 3 page). Muse reads labels.
- **Evidence:** rows of identical boxes: Cheerios ×15, Honey Maid ×25 (v3, all correct).

### D4. Two SAM concepts in parallel: "food" and "package"
- **What:** both SAM calls are sent at the same time (wait = the slower call, not the sum). A food box that is 80% inside a package box (or the same object, IoU > 0.7) joins that package. Loose produce has no package box, so it stays.
- **Why:** SAM "food" finds the **pictures** printed on packages and on signs. A "skip" answer was rejected because it could drop real items (T9). The package is the real item, named from its label.
- **Evidence:** shelf 6: v2 = 10 of 11 listings were pictures ("Cooked Spaghetti Illustration"); v3 = 8 of 8 real spaghetti boxes, all correct. SAM "food" also found 0 boxes on rows of Cheerios and Honey Maid; "package" found 15 and 25, so these photos now use the single call (not the slow agent).
- **Cost:** +1 SAM image = +$0.0025 per photo.
- **Source:** SAM takes one concept per request (dev.meta.ai/docs/sam/segmenting).

### D5. A tray of cooked food = one listing; servings from the tray size
- **What:** a third parallel SAM concept, "food tray". Food inside a tray joins the tray. Muse marks `"tray": true`; each tray is its own listing (never grouped); its servings come from D20, not from the box count.
- **Why (T9):** shelf 20: SAM counted 7 chicken pieces in one foil tray → "chicken ×7", which is wrong for a donation. Shelves 21–22 already gave one item per tray.
- **Evidence (v4):** shelf 21: 7–8 trays, each one listing; shelf 22: all 12 trays, one listing each; shelf 23: empty stacked trays → "not listed". Shelf 20 showed the holder bug fixed in D19.

### D6. Route by the number of boxes
- **What:** 0 boxes → agent; 1 box → whole call; 2+ → one multi-item call. An agent that ends with 0 or 1 box → whole call.
- **Why:** R1. A one-item photo always gets analyze's exact prompt and the original bytes. The multi-item path is used only when there is more than one item.

### D7. The agent is a fallback, not the default
- **What:** Muse Spark with one function tool, `segment(concept)` → SAM 3.1. At most 4 turns. Same answer format as the single call.
- **Why:** it finds products that a generic concept misses. But it is slow.
- **Evidence:** v1: agent 21 s / photo (15–31 s), single 9–11 s. v2: 20–50 s. With the package concept (D4), only 2 of 18 photos needed the agent in v3.
- **Source:** dev.meta.ai/docs/tool-calling (function tools, `tool_choice` auto, `function_call_output` with `previous_response_id`).

### D8. Resize the photo ourselves to Muse's limit
- **What:** before a multi-item call, scale the photo (Catmull-Rom, aspect kept) so that ceil(w/28) × ceil(h/28) ≤ 4,096. Marks are drawn and sizes are measured on this image.
- **Why:** Meta's docs say "the model may resize images" but give no rule; coordinates "are relative to the image the model actually received". If we resize, the marks, the sizes and the coordinates agree, and every run is repeatable.
- **Evidence (measured, `cmd/ie-tokens`, `POST /v1/responses/input_tokens`, photo 14):**

  | Sent | Image tokens | 28 px prediction |
  |---|---|---|
  | 640×480 | 416 | 414 |
  | 1280×960 | 1,612 | 1,610 |
  | 1792×1344 | 3,074 | 3,072 |
  | 2240×1680 and up | **4,098 (stops)** | 4,800+ |

  So: 1 token = 28 × 28 px, +2 fixed tokens, limit 4,096 (≈ 3.2 MP). The maximum image cost is 4,098 × $1.25/1M = $0.0051.
- **Dependency:** `golang.org/x/image` v0.36.0 (BSD-3, Go team; the newest that still needs only Go 1.24, like the other modules).

### D9. Boxes on Muse's 0–1000 grid
- **What:** the prompt gives each box as [x1, y1, x2, y2] on the 0–1000 grid and states the axes in Meta's words.
- **Why:** dev.meta.ai/docs/image-understanding: "Muse Spark reports positions on a normalized 0–1000 grid"; "State the exact fields and axis order in your prompt and ask for JSON." v1 sent pixels, which the docs do not describe. The grid is also what the app draws (÷10 = CSS %).

### D10. Numbered marks drawn on the photo + coordinates (Set-of-Mark)
- **What:** each box gets an outline and its number in a coloured tag; the text also gives its coordinates. Each answer returns a `point` on the grid as a check.
- **Why:** Set-of-Mark prompting (Yang et al. 2023): GPT-4V referring accuracy 25.7 → 86.4 with marks. Meta's docs do not describe drawn marks, so we tested them.
- **Evidence (v3, hand check of 294 boxes):** marks + coords **89.8%** correct; marks only 84.7%. With no coordinates in the text, 90% of the points still hit their box, so **Muse reads the drawn numbers**, but the coordinates help on small, dense photos (shelf 4).

### D11. One pass, not two
- **What:** one call returns all 8 fields for every box.
- **Why / evidence (v3):** two passes (names at `minimal`, then details once per group at `low`) cost more ($0.0248 vs $0.0222 / photo) and were less accurate (79.9% vs 89.8%). The image was not cached between the two separate requests (0 cached tokens), and most photos have 10–24 different products, so there are few repeated details to save.

### D12. Reasoning effort `low`
- **What:** `reasoning.effort = "low"` on every Muse call (set per request; dev.meta.ai/docs/reasoning, /protocols/responses). Allowed values: minimal, low, medium, high, xhigh (and max on 1.3); `none` is not supported by Muse Spark.
- **Evidence (v2):** `minimal` is 20% cheaper and 37% faster, but it gave general names and grouped different products: shelf 3, 12 different mac & cheese products became "Boxed Mac and Cheese ×12" (wrong servings).

### D13. One retry; no per-crop fallback
- **What:** boxes with no valid answer get ONE more call with only those boxes (renumbered); then they are rejected and reported. Nothing half-filled is returned.
- **Evidence (v1):** per-crop calls were the most expensive ($0.0185 / photo) and the least accurate: small crops gave invented items ("Caesar Salad Kit", "Spaghetti with Meat Sauce").

### D14. Count everything; name at most 24; three size levels
- **What:** every SAM box (after duplicates) is counted. The largest 24 are sent to Muse to name. Short side in the pixels Muse sees: **≥ 96 px named; 64–96 px named with a "check this" mark; < 64 px counted but not named**, and the app asks for a closer photo (T9's zoom idea).
- **Why:** dense shelves have 55–112 instances; the cap must not limit counting (T9). Small boxes are the main source of wrong names.
- **Evidence (v3, marks + coords):**

  | Short side seen | Boxes | Correct |
  |---|---|---|
  | ≥ 96 px | 189 | **97.4%** |
  | 64–96 px | 63 | 81.0% |
  | 48–64 px | 21 | 71.4% |
  | < 48 px | 13 | 46.2% |

  The 48 boxes that the reviewer could not read either had a median of 55 px. The pixel rule predicted errors better than an area rule (0.5% of the photo). Zhang et al. (ICLR 2025) also report lower MLLM accuracy on small objects.

### D15. Not food → "not listed", never deleted
- **What:** each answer has `"food": true|false`. A not-food product (paper towels, soap, a sign, a bin) goes to `not_listed[]` with its box. The app shows it in grey with "Add anyway".
- **Why:** the user has the last word, so a wrong `false` loses nothing.
- **Evidence (v3):** wall signs (shelf 14), a plastic bin (shelf 9) and a cardboard box (shelf 4) were correctly marked not food.

### D16. Grouping and servings
- **What:** union of the model's `same_as` links and equal normalised names. The first box gives the fields; `number_of_serving` = count (1 per box), except trays (D5). Groups are ordered by count, then box area. The top-level answer is the first group (R4).

### D17. Cost is measured per call, per photo and per item
- **What:** each call records tokens (input, cached, output, reasoning) and cost from the official prices (dev.meta.ai/docs/pricing-rate-limits, read 2026-10-26): Muse Spark 1.3 $1.25 / 1M in, $4.25 / 1M out, $0.15 / 1M cached; SAM 3.1 $2.50 / 1,000 images. Reasoning tokens are billed as output.
- **Result (v3, marks + coords, low, 18 photos):** **$0.022 / photo, $0.0012 / named item**. Today's analyze: $0.005–0.007 per item (one item per photo), so a multi-item photo is about **4–6× cheaper per listing**.
- The probe shares one SAM answer per photo between variants (fair comparison, lower test cost); a shared call is logged at its production cost and marked `cached`.

### D18. What the screen gets (R8)
- **What:** each item has `boxes` (SAM pixels), `norm_boxes` (0–1000 grid → CSS %) and `seen` (size in the image Muse read). SAM's masks (box-local, one_bit) are decoded and checked; shaded areas can use them later.

---

### D19. Containers: duplicates by IoU only, and holders are dropped
- **What:** package and tray boxes are de-duplicated only when they are the same object (IoU > 0.7). A container that holds 2 or more other containers (each 80% inside it) is a **holder** (a serving board, a crate, a donation box) and is dropped; the containers inside are the items. A container with exactly one inside that fills half of it or more is one object seen twice (a box and its front panel): the inner box is dropped.
- **Why / evidence (v4, shelf 20):** SAM "food tray" found the wooden board under 3 foil pans; the old rule ("90% inside a kept box = duplicate") dropped the 3 pans, so the photo gave 1 listing instead of 3.

### D20. Tray servings from a pan-size table
- **What:** for a tray, Muse gives the pan size compared with a 12 × 20 in hotel pan (full, ⅔, ½, ⅓, ¼, ⅙, ⅑, or other), the depth (2.5 / 4 / 6 in) and main or side, using a serving spoon, tongs or a plate in the photo as the size reference (T9). Servings = the manufacturer's chart at 6 oz (main) or 4 oz (side); for example a half pan, 2.5 in, main = 22. "Other" keeps Muse's estimate. Muse's estimate is always kept for review, and the user can change servings in the app (T9: a ballpark is enough).
- **Why / evidence (v4):** Muse's own estimate was not stable: the same wings tray got 30 servings in one call and 12 in another. A table gives the same number for the same pan.
- **Source:** Vigor steam table pan layouts (servings per pan by size, depth and ounces). The 6 / 4 oz portions are our assumption.

### D21. Run independent work at the same time (T9: use parallel speed-ups)
- **What runs in parallel:** the 3 SAM concepts; the resize for Muse (it does not need the boxes); the agent's `segment` calls in one turn; and, when 12 or more boxes are named, 3 Muse calls on boxes split in reading order (rows, then left to right), so identical neighbours stay in one call. Across calls, names are matched after normalising ("&" = "and", no filler words, singular).
- **What cannot:** Muse needs SAM's boxes; the retry needs the first answer; the gate stays first (≈0.3 s, and it stops non-food photos at $0).
- **Evidence (v4, 7 photos):** split 3 cut the wait from 14.9 s to **6.1 s** (−59%) for +35% cost ($0.026 → $0.035 / photo). On shelf 22 the 3 SAM calls added up to 32 s but took 11.6 s. Split broke some groups ("Go Pure Fruit and Oat Bars" ≠ "Go Pure Fruit Oat Bar"), so names are now normalised; split is used only from 12 named boxes.
- **Source:** dev.meta.ai/docs/pricing-rate-limits (3,000 requests per minute: parallel calls fit).

### D22. What SAM is sent (decided in X30b, see D24 and D28)
- **Decision (X30b):** SAM gets the upload's own bytes when the photo is upright, else the upright pixels as JPEG q98; the upload itself becomes about 2376 px at JPEG 0.9 (D28). Real phone photos: originals 297/297 = 100.0% correct of the readable boxes (`score_x30b.json`).
- **What now:** SAM gets the original bytes of the upload. No change in X30.
- **Evidence (SAM tests 1 and 2, 110 calls, $0.275):** the wait is mostly upload: shelf 22 as a 14.7 MB PNG took 7.6–12.2 s; the same pixels as a 3.3 MB JPEG q98 took 2.9 s, at 2048 px 2.0 s, with the same items. SAM gives the same answer for the same bytes (original sent twice). Packages keep 95–100% of nameable boxes at every encoding. But SAM "food" lost items on some photos: shelf 16 PNG 50 boxes, JPEG q98 6, q92 13; making a dense produce photo smaller lost items (shelf 14: 84% at 2048 px, 47% at 1536 px).
- **Why open:** published studies find detection accuracy flat from JPEG quality 100 down to about 40, so q98 should not lose 44 of 50 boxes. Our test photos were web JPEGs saved as PNG, so a new JPEG compresses them a second time. Real phone photos (X30b) decide the rule.
- **Also found:** the app uploads at most 1600 px at JPEG 0.85 after the camera saved at 0.7 (two compressions); 1536 px kept only 47% of the nameable produce on shelf 14. The upload size and quality are part of the X30b test.
- **Sources:** Gandor & Nalepa 2022; Dodge & Karam 2016; Poyser et al. 2020; SAM is billed per image (pricing page), so a smaller photo saves time, not money.

### D23. Every decision cites its sources (T9, 2026-10-27)
- **What:** each change carries `docs/sources/XNN.md`: for each decision and experiment, the sources (title, URL, date read, what it supports) and our own result files. The `NEW_WORK.md` entry links it, the commit message has a `Sources:` trailer, and the SUP stops if the file is missing or a decision has no URL.
- **Why:** every decision can be traced to documentation, a paper, or a verified article where someone else did the same.

---

### D24. Turn every photo upright first (EXIF orientation)
- **What:** the engine reads the EXIF Orientation tag (1–8) and turns the photo upright before anything else; SAM, the marks, the size levels and the boxes all use the upright frame. An upright photo goes to SAM as its original bytes; a turned photo goes as JPEG q98. A one-item photo still sends the original bytes to Muse (R1).
- **Why / evidence:** phone cameras keep the sensor's pixel order and store the turn in EXIF (iOS and Android use the same values); Go ignores it. 11 of 20 X30b photos had orientation 6 or 3.
- **Limit:** HEIC (iPhone default) cannot be decoded in Go; the app always sends JPEG.

### D25. Pet food is not food; only not-food products = a "not food" answer
- **What:** the prompt lists pet food with paper towels and soap. When every box is not food, the answer is `not_food: true` with the boxes in `not_listed` (the app shows them with "Add anyway"), not an error.
- **Evidence (X30b):** 24 pet-food boxes on photo 1 were all marked not food, but the run returned "no item passed the contract". The gate passed both pet-food photos (p 0.60, 0.47).

### D26. Tests run at the same time
- **What:** the probe runs `-parallel` photos at once (default 4; the local gate queues 8). `-parallel 1` is for latency runs, because parallel load raises each call's time. The SAM cache is shared and in-flight aware: a second run of the same photo and concept waits for the first call (SAM is paid once).

### D27. The SUP runs its S3 checks at the same time (template v3)
- **What:** the Go tests of each module, the Dockerfile checks, Python and the mobile tests run together; each result prints in one piece; the web-dist rebuild stays after them.

### D28. The app uploads at Muse's limit (to build in X32)
- **What:** the app sends about **2376 px on the long side** (≈ 4,096 image tokens at 16:9) at **JPEG 0.9**, about 1 MB, in place of 1600 px at 0.85.
- **Evidence (X30b, per-box labels `x30b_labels.json`, totals by `score_labels.py` in `score_x30b.json`):** camera originals 297/297 = 100.0% correct of the readable boxes (60 unreadable, not counted); app version 274/283 = 96.8% (44 unreadable). The app version lost counting (bananas: 71 counted vs 1, from the probe report) and small label text (salad bowls, photo 185725: 19 correct, 0 wrong vs 13 correct, 6 wrong). The app version was cheaper ($0.037 vs $0.047 / photo) and faster (9.1 vs 13.4 s).

### D31. The `/v1/inventory` service (X31; R2–R4)
- **What:** `inference/service.go` + `cmd/inventory`, port 8087, inside the compose network. Same bearer token file and input formats as analyze (multipart `image` or JSON `{image_base64, mime}`; the image is read by analyze's own reader, `analyze.Config.ReadImage`). Image limit 8 MB (the X32 upload). Engine settings = the X30b production variant: marks + 0–1000 coordinates, effort low, split 3 from 12 named boxes, concepts `food` ║ `package` ║ `food tray`, 24 named.
- **Answer:** the top level is the largest group in the analyze contract (R4); `items[]`, `not_listed[]`, `not_food`, `closer_photo`, `listings`, `counted`, `width`/`height` (the boxes are in these upright pixels), `gate_verdict`, `cost_usd`. The call trace stays in the event line, not in the answer.
- **Routes:** `/v1/inventory` (bearer callers: Postman, judges) goes straight to the service; `/api/inventory` (the app) goes through the router (D35). `/v1/analyze`, `/api/analyze` and `/api/scan` do not change.

### D32. PE-Core-S16-384 is the only gate (T9, 2026-10-26)
- **What:** compose starts the gate with the PE files and their pins (`c0618012…`, `7ba764ea…`). The earlier gate is not deployed on the Icarus box (research-only weights). The inventory service calls the gate first; NOT_FOOD answers `not_food` at $0; a gate that is down or undecided gives 503 `unavailable` (fails closed), like `/api/scan`.

### D33. Limits: per client, at the same time, per day
- **What:** 6 requests per minute per client address → 429. At most 3 photos at the same time → 503 `busy` (the box has 1.9 GiB; a decoded 12 MP photo is about 32 MB, plus the prepared copy, marks and crops). A daily spend cap of **$20 per UTC day** (T9) → 503 `budget`, checked after the gate (a gate refusal costs $0). A photo reserves $0.10 and settles at its real cost, so photos that run at the same time cannot pass the cap together. After a restart, the spend is read back from today's event file.

### D34. The real client address
- **What:** the Icarus box is behind a load balancer and Apache, so Caddy's peer is always private. Caddy trusts private ranges (strict mode: the address is read from the right of `X-Forwarded-For`, so a client cannot spoof it), and only the X31 routes send `{client_ip}` on. The older routes keep `{remote_host}`, unchanged.

### D35. Dedup against live listings (R7)
- **What:** the router's `/v1/inventory` checks the session cookie, stores the photo, calls the service with the token, then reads the caller's listings (`/api/listings/mine`). An item whose normalised name (the engine's `normName` rules) equals the title of one of the caller's **active, unexpired** listings gets `already_listed: {id, title, number_of_serving, expires_at}`; the answer gets `already_listed: <n>` and `dedup: ok`.
- **Why:** nothing is removed; the app shows "already listed" and the user decides (as D15). Only the caller's own listings count: two donors may list the same product. A listings service that is down gives `dedup: unavailable`, not a failed scan.

### D36. Events and cost (R6)
- **What:** one JSONL line per request in `$RUNS/inventory/events-YYYYMMDD.jsonl` (synced): trace id (the router's, so one trace joins both), status, route, gate verdict and time, SAM and grouping counts, each call (purpose, model, status, time, tokens, cost), the request's cost and the day's spend. The router's line (step `inventory`) adds the user, the photo, `already_listed` and the service's status. A telemetry sidecar ships the folder.

### D37. The image and the box
- **What:** the image builds from the repo root (the module imports `../server`), with `inference/Dockerfile.dockerignore` so only `server/*.go` and `inference/` are sent; static binary on scratch; tests run in the build. `GOMEMLIMIT=400MiB` and `mem_limit: 700m`.

### D38. SUP template v5
- **What:** S3 also checks `inference/Dockerfile` (root context) and validates `deploy/Caddyfile` with the pinned `caddy:2.8.4` image.

### D39. The inventory timeout is 55 s (X31c; replaced by D58: 25 s)
- **What:** `-timeout 55s` (was 120 s), under the load balancer's 60 s idle timeout (T9: keep the LB at 60 s; "no photo should take that long"). A photo over the time gets 504 with "this photo took longer than 55s; try a closer photo of fewer items". The router waits 65 s, Caddy 70 s, so the clear 504 reaches the caller.

### D40. Keep split 3 (X31b bench)
- **Why:** the live test took about 27 s for two dense photos; the box events showed the Muse split calls at 15.9–20.4 s (local work < 1 s). The bench (`probe_x31b_20261027T050833Z.json`, `score_x31b_bench.json` from `tools/hackathon/bench_x31b.py`; 20 phone photos, 4 variants on each photo one after the other):

  | Variant | Muse wait median / p90 | Cost / photo | Out tokens / box | Reasoning share | Near-dup pairs |
  |---|---|---|---|---|---|
  | split 3 (live) | 10.4 / 12.3 s | $0.0452 | 251 | 56% | 132 |
  | split 6 | 9.7 / 10.6 s | $0.0665 | 340 | 68% | 147 |
  | split 3 + compact | 9.9 / 12.3 s | $0.0435 | 230 | 65% | 144 |
  | split 6 + compact | 8.6 / 10.6 s | $0.0653 | 330 | 73% | 196 |

- **Findings:** each extra call reasons again (cost +47% for split 6); most of the wait is the model's reasoning and the API's own speed (the same photo took 6.4–8.5 s per call in X30b and 17–19 s live); more calls give more near-duplicate names.

### D41. `compact` is an option that is off
- **What:** `Engine.Compact` / probe suffix `:compact`: a box with the identical product as an earlier box in the same call gives only `box`, `point`, `same_as`, and takes that box's fields (a chain resolves; a copy of a box with no valid entry fails). Measured gain too small (−8% output tokens, −4% cost) for a new answer format live.

### D42. Next for speed and quality (T9, 2026-10-27)
- **Speed for the user:** the app shows the boxes first (SAM is done after about 4 s), then the names (X32 UI).
- **Quality:** merge near-duplicate names in one photo (132 pairs in 182 listings with split 3), then the gate retrain with shelf, pantry and pet-product photos (the pet-product photo passed the gate at p 0.581 and cost $0.051).

### D57. The whole-photo call starts with SAM (X32c, T9 2026-10-27: option B)
- **What:** `Engine.EarlyWhole` (service flag `-early-whole`, on): in mode auto the whole-photo call (analyze's prompt, the original bytes) starts at the same time as SAM's three concepts. SAM finds 0 boxes, fails, finds 1 box, or finds boxes that are all too small to name: that call is the answer, and the agent does not run. 2+ named boxes: the multi-item path as before; the early call is paid, logged as `muse:whole:early`, and not used (the run waits for it, so the ledger is complete). D6/D7 stay for mode auto without the flag (the probe's live variant).
- **Why:** T9: "most listings will be a single item"; a single item held close (kombucha) got 0 boxes from SAM on 3 of 3 app tries, and the agent took 35–41 s.
- **Evidence (bench, `compare_x32c.json` from `tools/hackathon/compare_x32c.py`; `probe_x32c_live_*.json` and `probe_x32c_early_*.json`, 24 photos, each run alone with its own SAM calls; groups by SAM's first find):**

  | SAM's first find | Photos | Live wait median / max | Early wait median / max | Cost live → early |
  |---|---|---|---|---|
  | 0 boxes | 3 | 33.0 / 35.2 s | 2.7 / 5.2 s | $0.0805 → $0.0360 |
  | 1 box | 1 | 9.9 / 9.9 s | 6.0 / 6.0 s | $0.0155 → $0.0152 |
  | 2+ boxes | 20 | 14.0 / 22.3 s | 14.1 / 22.5 s | $0.8710 → $0.9831 |

- **Cost of the choice:** on photo `185749` (bananas and apples) SAM's concepts found 0 boxes; the agent gave 2 listings (Banana Bunch, Apple) in 33 s, the early call 1 (Bananas) in 5.2 s. The 2+ photos pay the unused call (bench: $0.8710 → $0.9831 for 20 photos).
- **After the bench:** boxes that are all too small to name also use the early answer (the bench's garlic-knot photo paid a second whole call, 2.5 s + 7.1 s).

### D58. The photo limit is 25 s: API Gateway's 30 s (X32c)
- **What:** `-timeout 25s` (was 55 s, D39). A slower photo gets the service's own 504 `timeout`, not the gateway's 503.
- **Why:** `icarus.thehungreeapp.com` is an API Gateway HTTP API custom domain in front of the load balancer (`apigw-requestid` on every answer). AWS: HTTP API "Maximum integration timeout | 30 seconds | No" (cannot be raised). Own evidence: from the Mac, the kombucha photo got 503 `{"message":"Service Unavailable"}` at 30.3 s (HTTP/2 and HTTP/1.1) while the service logged 200 at 35–41 s; a 24.3 s photo went through; the box resolves the name to private addresses and got 200 at 37.5 s.
- **Target (T9):** every photo under 20 s. Next: cut about 5 s from the multi-item photos (bench 2+ max 22.5 s).

---

## 4. Results by run

| Run | Photos | Variant | $ / photo | $ / named item | Time / photo | Notes |
|---|---|---|---|---|---|---|
| v1 | 6 | auto (food + single + crop) | $0.011 | $0.0020 | 9.1 s | SAM "food" = 0 on Cheerios and Honey Maid |
| v1 | 6 | agent | $0.017 | $0.0021 | 21.3 s | best counts on identical rows |
| v1 | 6 | crop | $0.018 | $0.0035 | 6.2 s | invented items |
| v2 | 14 | coords, low | $0.023 | $0.0011 | 23.6 s | 0–1000 grid, resize, cap 24 |
| v2 | 14 | marks, minimal | $0.018 | $0.0009 | 14.8 s | over-grouping |
| v3 | 18 | **marks + coords, low** | **$0.022** | **$0.0012** | 20.2 s | **89.8% correct** |
| v3 | 18 | marks only, low | $0.021 | $0.0011 | 15.1 s | 84.7%; points 90% |
| v3 | 18 | two passes | $0.025 | $0.0013 | 18.7 s | 79.9% |
| v4 | 7 | marks + coords, low (trays, levels) | $0.026 | $0.0016 | 14.9 s | trays: one listing each |
| v4 | 7 | same, split 3 | $0.035 | $0.0021 | **6.1 s** | groups split across calls |
| SAM 1 + 2 | 5 | 110 SAM calls, sizes and encodings | — | — | — | see D22 |
| X30b | 19 | real phone photos, originals | $0.047 | $0.0025 | 13.4 s | **297/297 = 100.0%** of readable boxes |
| X30b | 19 | same, app upload (1600 px, 0.85) | $0.037 | $0.0021 | 9.1 s | 274/283 = 96.8%; counting lost |

Probe spend so far: $0.36 + $0.77 + $1.05 + $0.37 + $0.13 + $0.15 = **$2.83**; X30b $1.58; total **$4.41**.

---

## 5. Known limits and open items

0. **Accuracy numbers are traceable (X30c).** Every accuracy number in this document is printed by `tools/hackathon/score_labels.py` from per-box labels (`v3_ground_truth.json`, `x30b_labels.json`) into `score_v3.json` and `score_x30b.json`. The labels are one reviewer's; boxes the reviewer cannot read are not counted, so a high share means "no error among the boxes a person can check", not "no error".

1. **Gate on shelves:** 5 of 23 food photos refused (shelves, pantries). Retrain the head with such photos.
2. **Produce that looks alike:** turnips named "corn" or "parsnip", Roma tomatoes named "mini peppers", radicchio named "red onion". San Pellegrino water named "pasta bag".
3. **Test photos:** mostly small stock photos (some 474–600 px wide, some with watermarks), one AI-generated tray photo (shelf 20). Ground truth is one manual review of crops at product level. Before a claim in the demo: re-run on real phone photos taken for the project. Stock photos must not appear in the demo video.
4. **Latency:** 15–20 s per multi-item photo with one Muse call; about 6 s with split 3 (D21); the agent 20–50 s (not used in the service since X32c, D57). The public name has a 30 s limit (API Gateway, D58); the target is 20 s. The app must show progress (and could show boxes first).
5. **Prompt cache:** two separate requests with the same image did not share the cache. Not needed with one pass.
6. **Bins and crates** (shelf 9): a bin of cans can become one "not listed" box; the cans inside are counted only if SAM finds them.
7. **Barcode per box** on the server: later improvement.
8. **Upload size and quality** of the app (1600 px, two JPEG compressions) hide small items and counts (D28; fixed in X32).
10. **The gate** passes pet food (X30b) and ignores EXIF orientation (it sees sideways photos); both are gate items for later.
9. **Tray servings** use a fixed portion (6 / 4 oz); the user corrects them in the app.

---

## 6. Next steps

1. **X30 SUP:** commit `inference/` (engine, agent, prepare, pan table, two-pass code kept as a measured option, 35 tests), `cmd/ie-probe`, `cmd/ie-tokens`, `cmd/sam-size`, `tools/hackathon/gen_sam_goldens.py`, the result files and the hand check, this document, and `docs/sources/X30.md`; `NEW_WORK.md` entry 4. The SUP gains the sources check (D23).
2. **X30b, real photos:** 20 photos taken for the project (shot list: rows, mixed shelves, counters, produce, trays with a serving spoon, non-food, donation box, single item, far view, low light, a close-up and a second angle), as camera originals and as the app's upload version; plus a private set of about 2,000 buffet photos with trays. Decides D22 and the app's upload size, and re-checks accuracy with a hand check.
3. **X31:** HTTP service `/v1/inventory` (R2–R4), JSONL events with cost, Dockerfile, compose + telemetry sidecar, router route, dedup against live listings (R7). **Built in X31 (D31–D38).**
4. **X32:** multi-item review mockup (boxes, "check this", "not listed", "take a closer photo", tray servings), then the app change.
5. **Later (T9 idea): nested SAM calls.** With fast SAM calls, a package box can be segmented again (label, then the food picture or the weight text), and the weight looked up. This is why SAM speed matters even though it is billed per image.

---

## 7. Sources

- Meta Model API docs (read 2026-10-26): image understanding (`dev.meta.ai/docs/image-understanding`), token counting, reasoning, Responses protocol, prompt caching, tool calling, SAM segmenting, pricing and rate limits.
- Yang et al., *Set-of-Mark Prompting Unleashes Extraordinary Visual Grounding in GPT-4V*, arXiv 2310.11441 (2023).
- Zhang et al., *MLLMs Know Where to Look: Training-free Perception of Small Visual Details*, ICLR 2025, arXiv 2502.17422.
- Ultralytics, *SAM 3: Segment Anything with Concepts* (CountBench, exemplar results).
- Akyon et al., *Slicing Aided Hyper Inference (SAHI)*, arXiv 2202.06934 (tiling; not used yet).
- Vigor steam table pan layouts (servings per pan), WebstaurantStore, webstaurantstore.com/documents/pdf/vigorpan_layouts.pdf.
- Gandor & Nalepa, *First Gradually, Then Suddenly: Understanding the Impact of Image Compression on Object Detection Using Deep Learning*, Sensors 22(3):1104, 2022.
- Dodge & Karam, *Understanding How Image Quality Affects Deep Neural Networks*, arXiv 1604.04004, 2016.
- Poyser et al., *On the Impact of Lossy Image and Video Compression on the Performance of Deep Convolutional Neural Network Architectures*, arXiv 2007.14314, 2020.
- COCO evaluation size ranges (small < 32², large > 96²).
