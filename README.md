# V3RDE

V3RDE gets extra food to people near it, not to the trash. A donor takes one photo; Meta's AI models find, count and name each item, make the photo better, and the listing is live for 30 minutes. People near it grab it and pick it up. Food that nobody grabs goes to a food program near it (Rescue). The app is in seven languages.

Built for the Meta Global AI Developer Hackathon (Oct 26 – Nov 4, 2026). [NEW_WORK.md](NEW_WORK.md) lists every part made in the Hackathon, with its date, the Meta model it uses, its files and its sources. [NEW_WORK_UI.md](NEW_WORK_UI.md) does the same for the app's screens.

- Live: https://v3rde.thehungreeapp.com (the About page: https://v3rde.thehungreeapp.com/about/)

## How it is built

```
Your phone or browser      the Expo app (web build): Give, Receive, the chat, camera and barcode reader, 7 languages
        │
        ▼
Our server (one box)       Caddy (the only open port) → the Go services below, in Docker
        │                  gate, barcode and the ledger: unix sockets only; auth and push: a socket and a port
        ▼
Meta's AI models           SAM 3.1 · Muse Spark 1.3 · Muse Image 1.0 · Muse Voice Transcribe 1.0 · Muse Spark Open
                           (Meta's PE-Core runs on our server, in the food gate)
```

## The modules

The Go modules (Go 1.24, each with its own tests) are `server`, `inference`, `post`, `listings`, `auth`, `gate`, `barcode`, `telemetry`, `i18n` and `mobile/tools/brand` (the app icon tool). `mobile` is TypeScript and `qe` is Python.

| Folder | What it is | Runs as |
|---|---|---|
| `mobile/` | The app: Expo 58, React Native 0.88, the web build in `mobile/web-dist/` (committed). Give (photo → review → post), Receive (grab and pick up), the chat (voice or text), Touch up, Rescue, the language choice. Also the static About page and the admin Learn page (`mobile/public/`). Style rules: `mobile/docs/style_guide.md`. | `caddy` serves `web-dist` |
| `server/` | The shared library of the analyze path (the service runs `inference/cmd/inventory`; `server/cmd/analyze` is an older standalone binary): the Meta Model API client, the food prompt, the bearer token, the prompt registry (each Meta call names its prompt id, version and sha). | used by `inference`, `post` |
| `inference/` | The scanner: `POST /api/analyze` (one photo, or a photo with many barcodes) — the food gate, the barcode catalog, SAM 3.1 boxes, Muse Spark names and details; `POST /api/analyze/find` (find an item the scan missed); the daily learn report (`/api/analyze/learn`, admin only). | `analyze` |
| `post/` | The jobs after the scan, one binary each: `touchup` (Muse Image 1.0 makes the photo better), `voice` (Muse Voice Transcribe 1.0 and Muse Spark 1.3: change the list by voice or text), `rescue` (a food program near the food; Muse Spark web search when the directory has none), `fanout` (the post on the V3RDE Facebook Page), `push` (live updates to the web app through an API Gateway WebSocket, when a touch-up or a rescue ends). | `touchup`, `voice`, `rescue`, `fanout`, `push` |
| `listings/` | Listings: post, change, the 30-minute timer, grab and release, the pickup address, the photos. | `listings` |
| `auth/` | Sign-in: email and password, the session cookie, password reset by email link. | `auth` (unix socket and port) |
| `gate/` | The food gate: PE-Core (a Meta model, on our server, LiteRT) says "food" or "not food" before any cloud call. | `gate` (unix socket only) |
| `barcode/` | The barcode catalog (Food DB, from Open Food Facts): a known code fills the listing at once. | `barcode` (unix socket only) |
| `telemetry/` | One sidecar for each service (not `push`) and the ledger: the time and cost of every call, from the event logs only; `cmd/edgecloud` reports how long each part takes on the box and in the cloud. | `sidecar-*`, `ledger` |
| `i18n/` | The translation round: after each deploy it finds the new or changed app texts, translates them into 6 languages with Muse Spark Open, scores them with MetricX, checks them with two judges, and publishes `/i18n/<lang>.json`. Texts a person gives are in `i18n/fixed.json`. | the `lif` job |
| `qe/` | The MetricX scorer image and its tests (Python). | used by the `lif` job |
| `deploy/` | `docker-compose.yml` (every service, its flags and volumes) and `Caddyfile` (the routes, the security headers). | — |
| `tools/` | `v3rde_stack.sh` (build, start, check the stack; the image stamps), `lif_round.sh` (the translation round), `learn_round.sh` (the daily learn report, a systemd timer on the box), `hackathon/` (the benchmark and measurement scripts). | — |
| `docs/` | API notes (`analyze_photos_api.md`, `analyze_scan_job_api.md`), the engine design, and `docs/sources/`: one file for each change, with an https source for each decision. | — |

## Run the tests

```bash
# each Go module (the gate's LiteRT and turbojpeg tests need build tags: (cd gate && go test -tags "litert turbojpeg" ./...); the plain run skips them)
for m in server auth listings barcode gate telemetry inference post i18n mobile/tools/brand; do (cd $m && go test ./...); done
# the app: type check and tests
(cd mobile && npm ci && npx tsc --noEmit -p . && npm test)
# the MetricX scorer helpers
python3 -m unittest discover -s qe -p 'test_*.py'
```

## Run the stack

```bash
bash tools/v3rde_stack.sh up      # build the images whose code changed, start, check
bash tools/v3rde_stack.sh smoke <photo.jpg>   # one /v1/analyze call
bash tools/v3rde_stack.sh down
```

The stack needs host folders and secret files (read-only mounts, never in an image or an environment variable): see the head of `tools/v3rde_stack.sh`. Only Caddy publishes a port, on 127.0.0.1.

## How changes are made

Every change is one script made and tested first in a copy of the repo, then run on one machine: it checks the commit it starts from and the sha256 of each file it changes, writes the change, runs every test above and the Dockerfile builds, builds the web app again, and commits and pushes only when all pass. Each change has an entry in `NEW_WORK.md` and a sources file in `docs/sources/`. Branches: `services` is deployed; work branches (for example `items`, `logo`) merge into it.

## Credits

The Hungree red and the Poppins font (SIL Open Font Licence) are Hungree brand assets from before the Hackathon. The product photos after a barcode scan are from Open Food Facts (openfoodfacts.org), CC BY-SA.
