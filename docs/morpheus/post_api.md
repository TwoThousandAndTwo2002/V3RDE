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
