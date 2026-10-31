# The web build and the web server

Written new for V3RDE (NEW_WORK_UI.md, UI 18). Sources: `docs/sources/U18.md`.

## What is here

- `mobile/web-dist/`: the web build of the app (`npm run web:build`, that is `expo export -p web --output-dir web-dist`). It is committed. The SUP script of a change to `mobile/` makes it again and checks that two builds give the same files.
- `mobile/tools/web/serve.mjs`: a local web server for the web build. It has the same routes as the edge of the stack (`deploy/Caddyfile` on branch `services`), so the browser sees one origin for the app and the services, as in production.
- `mobile/tools/web/routes.mjs`: the routes, the body limits, the wait times and the cache rules of the edge, as pure functions. `tests/web.test.mts` tests them and the server.

## Use

```
cd mobile
npm run web:build
V3RDE_API_BASE=https://<the stack> npm run web:serve      # http://127.0.0.1:8090
node tools/web/serve.mjs --api http://127.0.0.1:9000 --port 8792 --root web-dist
```

- `--api` (or `V3RDE_API_BASE`): the base URL of the services. Without it the app loads, and every `/api` and `/v1` call gets 502 `no_upstream`.
- `--host` is `127.0.0.1` by default. Use `--host 0.0.0.0` only on a network that you trust (for example to open the app on a phone).
- `V3RDE_WEB_QUIET=1`: no request log. The log has only the method, the path (no query) and the status.

## What the server does

| Path | Answer |
|---|---|
| `/healthz` | `{"ok":true,"service":"edge"}` |
| `/api/analyze*`, `/v1/analyze*` | to the services; body at most 7 MB; 70 s wait for the answer |
| `/api/touchup*`, `/v1/touchup*` | to the services; 64 KB |
| `/api/rescue/*`, `/api/fanout/*` | to the services; 64 KB |
| `/api/voice`, `/v1/voice` | to the services; 13 MB; 40 s wait |
| `/api/auth/*` | to the services; 64 KB |
| `/api/listings*` | to the services; 6 MB |
| `/v1/fanout*`, `/v1/rescue*`, `/v1/voice/*` | 404 `not_found` (internal routes of the services) |
| `/api/scan`, `/api/inventory`, `/v1/inventory` | 410 `route_removed` |
| a path that ends in `/` and names a folder with an `index.html` | that page, for example `/about/` (the About page, #78) |
| any other path | a file of the web build, or `index.html` (the app routes the path) |

- The limits are in units of 1000, as at the edge (7 MB = 7 000 000 bytes). The path matchers do not look at case. The route is chosen from the path after `.` and `..` are removed, which is the path the services get.
- The client address headers (`X-Forwarded-For`, `CF-Connecting-IP`) are set to the real address of the browser. When the browser goes away, the request to the services stops. An answer that breaks in the middle also ends the answer to the browser.
- A body over the limit gets 413 `too_large`. No first byte of the answer in the wait time gives 504 `edge_timeout`. Services that cannot be reached give 502 `upstream_unreachable`.
- The files in `/_expo/static/` and `/assets/` have a hash in the name: they are kept one year (`immutable`), but only when the file is there. All other answers are `no-store`.
- A path with `..`, a NUL byte or a bad escape never leaves the web build: the server sends `index.html`.
- The session cookie comes back without `Secure` and without `Domain`. The server is plain http, and a browser drops a `Secure` cookie on plain http (Safari also on `localhost`; all browsers on a network address, for example when a phone opens the app).

## Production

The edge of the stack (Caddy, branch `services`) serves the same web build from `/srv`. When branch `ui` is merged, set `V3RDE_WEB=./mobile/web-dist` for the edge (`deploy/docker-compose.yml`). This server is only for a local check. It is not part of the stack.

## How the app calls the services (UI 18)

- **Scan:** `POST /api/analyze?async=1` answers 202 with a `scan_id`. The app then asks `GET /api/analyze/scan/{id}` until the job is finished (X48c: a held poll with `since` + `wait=8000`, which the service answers when the job changes; once a second on a service with no `version`), at most 75 s in all. The job holds the same answer as the one-request route. Thus a long scan never meets the 35 s write limit of the service (with one request, the browser then got a 502 with no text). When the user leaves the screen, the app stops asking. A 404 while it asks (the service started again) shows "The food scanner is busy, try again"; the photo is kept. A service that answers 200 at once (no job route) still works.
- **Daily limits:** `caller_daily_cap_reached` (429, the share of one user) shows the same words as the limit of the service: the scan sheet "Scanning is paused", "Touch-up is paused for today.", "Voice is paused for today.".
- **Voice:** `no_speech` shows "No speech was heard. Try again."; `busy` and `rate_limited` show "Voice is busy. Try again in a minute.".
- **Grab:** 409 (collected or expired) and 404 show "This food is gone", and the card shows that the food is gone.
- **Touch-up:** a `queued` job is still working.
