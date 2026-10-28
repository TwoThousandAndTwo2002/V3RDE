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

## 3. Facebook Page fan-out (X35c) — `cmd/fanout`, port 8090

Muse Spark 1.3 writes a short public post from the listing photo, title and
servings; the Pages API posts it with the photo to the Icarus Page. A claim
edits the post to start with `[CLAIMED]`; the timer to `[EXPIRED]`.

```
POST /api/fanout/{listing_id}   session cookie, the caller's own active listing  -> 201 post
GET  /api/fanout/{listing_id}   -> 200 post (owner only)
POST /v1/fanout/status          internal only: its own token (-status-token-file), NO Caddy route
     {"listing_id": "l_…", "status": "claimed" | "expired"}  -> 200 post
GET  /healthz
```

Post:
```json
{ "listing_id": "l_…", "owner_id": "…", "post_id": "PAGEID_POSTID", "photo_id": "…",
  "text": "Free apples from a neighbor! …", "status": "posted",
  "expires_at": "…", "posted_at": "…", "request_id": "…", "cost_usd": 0.004 }
```
- One post per listing: a second POST answers 409 `already_posted` with the post (409 `conflict` while the first one runs).
- A second Muse Spark call checks the text; a refusal is 422 `post_rejected` (nothing is posted). The title goes to the model as JSON data.
- The public text never has the pickup text or a link from the model; the service adds `Claim it in the app: <link-base><listing_id>` when `-link-base` is set.
- **Integration hook:** the listings service calls `POST http://fanout:8090/v1/fanout/status` with `claimed` after a grab, with the internal token `/run/secrets/fanout_status_token` (a new secret; never the analyze token, which is public). The same status again changes nothing; a claimed post stays claimed.
- `-sweep 1m` marks posts `[EXPIRED]` when the listing timer ran out. Keep it off until the claim hook exists (else a claimed listing could show `[EXPIRED]`).
- Refusals: 400, 401, 404, 409 `not_active` / `already_posted` / `conflict`, 422 `post_rejected`, 429, 500 (Facebook took it but the record failed: the events line has the `post_id`), 502 (the text broke a rule, or Facebook refused), 503 `disabled` (no Page) / `daily_cap_reached`.

Flags: `-status-token-file /run/secrets/fanout_status_token`, `-page-id`, `-page-token-file /run/secrets/fb_page_token` (a Page access token with `pages_manage_posts`, `pages_read_engagement`, `pages_show_list`), `-graph https://graph.facebook.com`, `-graph-version v25.0`, `-link-base`, `-sweep 0`, `-cap-usd 1`, `-reserve-usd 0.02`, `-timeout 25s`, `-listings`, `-auth`, `-data`, `-events`. No Page id or token = posting off (503), so it can be deployed first.

## 4. Voice posting (X35d) — `cmd/voice`, port 8091

Muse Voice Transcribe 1.0 turns one utterance into text; Muse Spark 1.3 maps
it to changes of the review list. The answer is a **proposal**: the app shows
it highlighted and posts nothing until the donor taps "Post".

```
POST /api/voice     session cookie        POST /v1/voice     bearer token
     multipart: "audio"       WAV, PCM, ≤ 60 s (measured from the samples, not the header), ≤ 12 MB
                              (mono 16-bit 16/24 kHz with exact sizes is sent as is; stereo, 8-48 kHz,
                              8/24/32-bit or a streaming header is rewritten; WebM/Opus/AAC → 415)
                "items"       optional JSON: [{"item":1,"title":"Pasta tray","number_of_serving":3}]
                              (only these 3 keys; title 1-80 bytes; servings 1-99; else 400)
                "pickup_text" optional, the current text
                "lang"        optional, the device's BCP 47 tag (es-MX → languageBias Spanish)
  -> 200 proposal
GET  /healthz
```

Proposal:
```json
{ "request_id": "…", "transcript": "six trays of pasta, the salad is gone, pick up at the back door before 5",
  "changes": [ {"item": 1, "number_of_serving": 6}, {"item": 2, "remove": true} ],
  "new_items": [],
  "pickup_text": "Back door, before 5 pm",
  "unclear": "",
  "dropped": ["a new item (Bread): the photo is the source of truth"],
  "audio": {"sample_rate": 24000, "channels": 1, "bit_depth": 16, "ms": 2000, "converted": false},
  "cost_usd": 0.0012, "latency_ms": 3100, "models": ["muse-voice-transcribe-1.0", "muse-spark-1.3"] }
```
- **The listing contract only:** `title` (≤ 80 bytes), `number_of_serving` (1–99), `pickup_text` (the directions, ≤ 200 bytes), from `listings/` itself. No description. Any other key from the model is dropped and listed in `dropped`.
- `changes`: only the fields the donor changed (absent = no change). `remove: true` removes the item.
- With `items`: no new items (the photo is the source of truth). Without `items`: `new_items` (at most 10), each `needs_photo: true`. The listings service needs a photo for every listing, so the app must ask for one (an integration decision).
- `unclear`: a short question to show when the model could not map something.
- `dropped`: what the rules refused (for the app and the ledger).
- The events line keeps the transcript and the proposal, never the audio.
- Refusals: 400 (not multipart, no audio, bad items), 401, 413 `audio_too_large`, 415 `unsupported_media` / `audio_too_long`, 422 `no_speech`, 429, 502, 503 `daily_cap_reached`, 504.

**The app must record WAV.** The web's MediaRecorder gives WebM/Opus, and Android gives AAC, so the app needs a PCM recorder: for example, the Web Audio API with a WAV writer on the web, and a PCM stream module on the phone. This is an integration step.

Flags: `-max-audio 60s`, `-max-bytes 12MB`, `-cap-usd 1`, `-reserve-usd 0.01`, `-rate-burst 10`/min, `-timeout 25s`, `-asr-url`, `-asr-model muse-voice-transcribe-1.0`, `-model`, `-auth`, `-events`, `-trust-forwarded`.

## 5. The app side of voice (X35e)

The clip is made on the device as the one format the speech endpoint takes:
WAV, 16 kHz, mono, 16-bit, exact header sizes. The server then sends it on
byte for byte.

- `useVoiceRecorder()` (`mobile/src/voicerec.ts`) → `{recording, start(), stop(), cancel()}`; `stop()` gives a `VoiceClip` (an `Upload` + `ms`, `rate`, `source`). It stops by itself at 59.5 s.
  - **Android / iOS:** expo-audio 58.0.10 `useAudioStream` (16 kHz, mono, int16) + `startFileRecordingAsync({format: "wav"})`. If an Android device refuses 16 kHz, the clip has the device rate and the server converts it.
  - **Web:** `src/webvoice.ts`: `getUserMedia` (https) + the AudioWorklet `/voice-capture-worklet.js` at the device rate, then `src/wav.ts` resamples to 16 kHz.
  - Errors: `VoiceError` with code `denied` (microphone refused), `unsupported` (no https or no AudioWorklet), `failed`.
- `api.voice(clip, items, pickupText, lang)` sends `audio`, `items` (only `item`, `title`, `number_of_serving`), `pickup_text`, `lang` to `/api/voice` (35 s timeout).
- Native builds need a new APK/IPA: expo-audio is a native module. The web app needs only the web-dist deploy.
