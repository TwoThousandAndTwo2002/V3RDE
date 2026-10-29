# Scan as a job (for the app): no visible wait

X44b (NEW_WORK.md 37, `docs/sources/X44b.md`). The scan takes about 14 s. With this route the app gets an answer at once and shows what the scanner finds while it works: first the boxes (about 3 s, when SAM 3.1 is done), then the names (as each Muse Spark call ends), then the full answer.

The 200 route (`POST /api/analyze`, `POST /v1/analyze`) does not change.

## Start

`POST /api/analyze?async=1` (session cookie) or `POST /v1/analyze?async=1` (bearer token). The body is the same as the 200 route: multipart `image` (+ `barcode`, `capture`), or JSON `{image_base64, mime}`.

The checks before the photo is read (caller, rate limit, image type and size) answer as they do now (401, 413, 415, 429, ...). Then:

```json
202 {"request_id": "…32 hex…", "scan_id": "…32 hex…", "status": "running"}
```

## Poll

`GET /api/analyze/scan/{scan_id}` (or `/v1/...` with the token), about once a second, until `status` is not `running`. Only the caller that started the job sees it (the same user, or the token); any other caller gets `404 not_found`. A job is kept 10 minutes after it ends.

```json
200 {
  "scan_id": "…", "request_id": "…",
  "status": "running",            // running | done | failed
  "stage": "naming",              // checking | finding | naming | done
  "boxes": [[120, 80, 470, 610], [500, 90, 880, 640]],   // 0-1000 grid of the upright photo: [left, top, right, bottom]
  "named": [{"n": 1, "name": "Tomato soup"}],             // box n = boxes[n-1]
  "elapsed_ms": 6120
}
```

- `checking`: the barcode, the food gate. `finding`: SAM 3.1 looks for the items. `naming`: the boxes are known; names come in as each Muse Spark call ends.
- `boxes` is empty until SAM is done. With 0 or 1 box, the next answer is the whole-photo answer (no names come before it).
- To draw a box on the photo: left = `box[0] / 10` %, top = `box[1] / 10` %, width = `(box[2] - box[0]) / 10` %, height = `(box[3] - box[1]) / 10` %.
- The names are hints for the wait. The review list uses only `answer`: the items there are grouped (two boxes of the same product are one item), and only they have a `photo_id`.

## End

```json
200 {"scan_id": "…", "status": "done",   "stage": "done", "http_status": 200, "answer": { …the 200 route's answer… }, …}
200 {"scan_id": "…", "status": "failed", "stage": "done", "http_status": 422, "answer": {"request_id": "…", "error": "no food detected in the image", "code": "not_food"}, …}
```

`answer` is exactly the body that the 200 route gives for the same photo (`done`), or its error body (`failed`, with that route's status in `http_status`: 409 `already_listed`, 422 `not_food` / `model_no_food_*`, 429 `caller_daily_cap_reached`, 502, 503 `busy` / `daily_cap_reached` / `gate_unavailable`, 504 `timeout`).

## Why polling

The public name is an API Gateway HTTP API. API Gateway streams a response only for REST APIs, so a streamed answer cannot pass. Each poll is a short request, far under the 30 s limit. The events line of the scan is the same as the 200 route's, with `"async": true`.
