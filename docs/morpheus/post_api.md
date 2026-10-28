# Icarus post services API (X35, 2026-10-28)

Services that work on a listing after the scan. Each one is a separate Go
binary in the `post/` module (one image, `post/Dockerfile`). **Status: built
and tested, not deployed.** The integration step adds the compose services,
the volumes, the secrets and the Caddy routes.

Shared rules (the same as the analyze service):
- `/api/...` routes take the app's session cookie (`morpheus_session`); `/v1/...` routes take the analyze bearer token.
- Every non-2xx body is `{"request_id", "error", "code"}`.
- Every POST (and every touch-up job) writes one line to `events-YYYYMMDD.jsonl` with its cost; a daily cap per service (UTC day) gives `503 daily_cap_reached`.
- Rate limits use the socket peer. Set `-trust-forwarded` only behind Caddy (Caddy sets `X-Forwarded-For` itself).
- **Compose:** when a service sets `entrypoint`, the image CMD does not apply: give every flag in the compose `command` (at least `-listen 0.0.0.0:<port>`, `-auth`, `-listings`, `-dir`/`-data`, `-events`).
- Secrets are read-only files: `/run/secrets/meta_key`, `/run/secrets/analyze_token` (and the service's own, below). They are never printed.

## 1. Touch-up (X35a) — `cmd/touchup`, port 8088

Muse Image 1.0 edits the photo; Muse Spark 1.3 checks that the food did not
change. The original is always kept and stays the default.

```
POST /api/touchup?mode=light|background[&photo_id=<32 hex>]    session cookie
POST /v1/touchup?mode=light|background                         bearer token
     body: multipart "image" or JSON {"image_base64","mime"} (≤ 5 MB, jpeg/png/webp/gif)
     — or no body and photo_id = an item photo from /api/analyze that the caller owns
  -> 202 {"request_id", "touchup_id", "status": "running", "mode"}

GET /api/touchup/{touchup_id}            (or /v1/...)  -> 200 job
GET /api/touchup/{touchup_id}/photo      (or /v1/...)  -> image/jpeg (status "done" only, else 409)
GET /healthz
```

Job:
```json
{ "touchup_id": "…", "request_id": "…", "status": "done",
  "mode": "light", "source_photo_id": "…", "photo_id": "…(the edit)",
  "reason": "same items, better light", "cost_usd": 0.0139, "latency_ms": 9000,
  "image_model": "muse-image-1.0", "check_model": "muse-spark-1.3" }
```
- `status`: `running` → `done` (the edit passed the check; `photo_id` is the edit), `kept_original` (the check said the food changed, or it could not check), or `failed` (the edit failed).
- Poll every 1–2 s. A finished job stays readable for 24 h (in memory: a restart forgets it; the edited file stays).
- The edit is stored in the item-photo format (`<photo_id>.jpg` + `.json` with `photo_id`, `user_id`). To post a listing with it, the listings service must read this folder too (`-item-photos`); that is an integration step.
- Refusals: 400 `bad_request` (mode), 401, 404 (`photo_id` not found or not yours), 413, 415, 429, 503 `busy` (2 jobs at the same time) or `daily_cap_reached`.

Flags (`touchup -h`): `-cap-usd 2`, `-reserve-usd 0.03`, `-max-in-flight 2`, `-rate-burst 4`/min, `-job-timeout 90s`, `-image-reasoning ""` (API default), `-no-check` (then every edit is `kept_original`), `-item-photos` (the analyze photo folder, read-only), `-dir`, `-events`, `-auth`.

## 2. Rescue (X35b) — `cmd/rescue`, port 8089

For a listing that nobody claimed before its timer ran out. Muse Spark 1.3
decides shelf-stable or perishable from the photo and drafts an inquiry; the
Feed America directory gives the nearest places; the donor sends it.

```
POST /api/rescue/{listing_id}       session cookie, the caller's own listing
     {"zip": "10001"}  or  {"lat": 40.75, "lng": -73.99}
  -> 200 rescue
GET  /api/rescue/{rescue_id}        -> 200 rescue (owner only)
POST /api/rescue/{rescue_id}/send   {"place_id": "a2", "to_email": "…"[, "subject", "body"]}
  -> 200 {"request_id", "sent": true, "rescue_id", "place_id"}   (only with a mail relay)
GET  /healthz
```

Rescue:
```json
{ "rescue_id": "…", "request_id": "…", "listing_id": "l_…", "title": "Canned beans",
  "number_of_serving": 24, "kind": "food_bank", "shelf_stable": true,
  "reason": "sealed cans keep without a refrigerator",
  "draft": { "subject": "…", "body": "…" },
  "places": [ { "id": "a2", "name": "City Food Pantry", "type": "pantry",
                "address": "2 Oak Ave, Town, NY, 10001", "phone": "212-555-0102",
                "distance_miles": 1.5, "website": "https://…", "email": "…(only when the directory has one)",
                "tel": "tel:2125550102", "mailto": "mailto:…?subject=…&body=…" } ],
  "attribution": "Feed America (feedam.org), CC BY 4.0",
  "created_at": "…", "cost_usd": 0.004, "sent": [] }
```
- `kind`: `food_bank` (shelf-stable: pantries and food banks first) or `meal_program` (perishable: soup kitchens, meal programs, shelters first). Nearest first in each group; 3 places.
- The app must show `attribution` (CC BY 4.0).
- The app opens `mailto` (the donor's own mail app) or `tel`. `email` is filled only if the directory gives one; else the donor types the address (or uses `/send` with `to_email`).
- One rescue per listing: a second POST answers 200 with the first rescue (no new Muse call); while the first one runs, 409 `conflict`.
- `/send` sends the stored draft (or the donor's edited `subject`/`body`), Reply-To = the donor's email; once per place; 3 per donor per day; rate-limited. When the place has a known `email`, `to_email` must be that address (any case). The SMTP exchange stops after `-smtp-timeout` (20 s).
- Refusals: 400 (zip / lat-lng / body), 401, 404 (`listing not found`: unknown or not yours), 409 `collected` / `not_expired`, 422 `not_bulk`, 502 (the draft or the directory failed), 503 `daily_cap_reached` / `disabled` (no relay), 504.

Flags: `-min-servings 1` (**the bulk rule: T9 decides**), `-places 3`, `-cap-usd 1`, `-reserve-usd 0.02`, `-timeout 25s`, `-send-per-day 3`, `-smtp-timeout 20s`, `-trust-forwarded`, `-directory https://feedam.org`, `-smtp-addr` / `-smtp-user` / `-smtp-pass-file /run/secrets/smtp_pass` / `-from` (all empty = no relay), `-listings`, `-auth`, `-data`, `-events`.

Not confirmed: the directory's result field names (the public docs show `results`; the parser accepts the likely names). Check them on the first live call.
