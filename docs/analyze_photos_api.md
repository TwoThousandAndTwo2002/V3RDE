# Item photo bundle API (for the app)

After `POST /api/analyze` (or `/v1/analyze`) gives an answer with several items, the app can get every item photo in **one call**, already made small.

## Request

`POST /api/analyze/photos` (session cookie) or `POST /v1/analyze/photos` (bearer token), `Content-Type: application/json`, at most 4 KB:

```json
{ "request_id": "<the analyze answer's request_id>",
  "photo_ids": ["<items[0].photo_id>", "<items[1].photo_id>"],
  "side": 800,
  "kb": 40 }
```

| Field | Rule | Default |
|---|---|---|
| `request_id` | 32 hex, from the analyze answer | required |
| `photo_ids` | 1 to 24 different ids, from the same answer | required |
| `side` | the long side in px: `282` (review list) or `800` (listing) | `800` |
| `kb` | the target size of each JPEG, 10 to 90 KB | `40` |

## Answer (200)

```json
{ "request_id": "...", "side": 800, "kb": 40, "count": 2,
  "items": [
    { "photo_id": "...", "item": 1, "kind": "crop", "w": 800, "h": 600,
      "bytes": 39412, "q": 74, "sha256": "...", "jpeg_base64": "/9j/4AAQ..." }
  ],
  "ms": { "read": 4, "compress": 310, "total": 322 },
  "cached": false }
```

- `items` are in the order of `photo_ids`.
- Show one with `{ uri: "data:image/jpeg;base64," + item.jpeg_base64 }`.
- `q` is the JPEG quality it reached; if `bytes` is over `kb`, even quality 30 was too big.
- Each photo is turned upright (its EXIF orientation), and any upload type works (JPEG, PNG, GIF, WebP); the answer is always JPEG.
- With the default `kb` (40), each small photo is kept, and a call whose photos are all kept answers at once (`"cached": true`).

## Errors (`{request_id, error, code}`)

| Status | code | When |
|---|---|---|
| 400 | `bad_request` | not JSON, over 4 KB, a bad id, 0 or more than 24 ids, the same id twice, `side` or `kb` not allowed |
| 401 | `unauthorized` | not signed in, no token |
| 404 | `not_found` | one id is unknown, is another user's, or is from another answer |
| 500 | `internal_error` | a stored photo could not be read |
| 503 | `busy` | two bundles are in work; try again in a few seconds |
| 504 | `timeout` | not done in 20 s |
| 499 | `client_closed` | the app closed the connection before the answer (it gets no answer) |
