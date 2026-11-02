# LR3 — sources for each decision

Change: LR3 (services, branch `learn`), the daily run of the learn report on the box (a systemd timer) and a read-only route for admin accounts (`inference/learnroute.go` (new), `inference/learnroute_test.go` (new), `inference/service.go`, `inference/service_test.go`, `inference/cmd/inventory/main.go`, `deploy/docker-compose.yml`, `deploy/learn/v3rde-learn.service` (new), `deploy/learn/v3rde-learn.timer` (new), `tools/learn_round.sh` (new), `tools/learn_install.sh` (new), `tools/v3rde_stack.sh`; NEW_WORK.md 95).

T9 (2026-11-01): the scheduler: "systemd timer (Recommended)"; the report page: "Admin accounts only".

Code review (services bbfed58): the box has no scheduler; `tools/lif_round.sh` (X49b) runs a one-shot job from the stack's images after a deploy; the services run as uid 65534 and the stack script makes their folders and secret files; the routes of a signed-in user ask auth `/api/auth/me` (it answers the id and the email).

### D411. A systemd timer, once a day after the UTC day
- https://manpages.debian.org/buster/systemd/systemd.timer.5.en.html — `OnCalendar=` defines "realtime (wallclock) timers"; `RandomizedDelaySec=` "Delay the timer by a randomly selected, evenly distributed amount of time between 0 and the specified time value"; with `Persistent=true` "the time when the service unit was last triggered is stored on disk", and a run missed while the timer was inactive starts at once.
- https://www.mankier.com/7/systemd.time — "Timezone can be specified as the literal string "UTC"": the timer is `*-*-* 00:23:00 UTC` (up to 10 min later), after the UTC day that the report reads ends.
- `tools/learn_install.sh` (root, once) makes the two units with the checkout path in a temporary folder, checks them (`systemd-analyze verify`), only then installs them, and enables the timer. It does not restart the stack and does not reboot.

### D412. The job runs with no network, read-only, as 65534
- https://docs.docker.com/engine/network/drivers/none/ — "If you want to completely isolate the networking stack of a container, you can use the `--network none` flag". The report needs no network.
- `tools/learn_round.sh` waits up to 10 min for the analyze container (a deploy at the time of the timer), then takes the image, the events folder and the learn folder from the running analyze container (its image and its `/events` and `/learn-reports` mounts; not `/learn`, which is the binary in the image), so it reads the same files the service writes, with no path set by hand. `docker run --rm --network none --read-only --user 65534:65534 --memory 256m --cpus 1 --cap-drop ALL --security-opt no-new-privileges`; the events are mounted read-only.

### D413. The route: admin accounts only, 404 for everyone else
- https://api-security.owasp.org/editions/2023/en/0xa5-broken-function-level-authorization/ — the checks "should deny all access by default, requiring explicit grants to specific roles", and administrative functions need their own authorization check.
- `GET /api/analyze/learn` (the days) and `/api/analyze/learn/{day}` (the JSON; `?format=md` the Markdown), only on the session prefix. An admin is a signed-in user whose auth user id is in `admin_users` (a file in the config folder, one id per line or comma-separated, text after `#` is a note; the stack script makes it empty when missing). Not the email: sign-up does not check that an email is the user's, so anybody could sign up first with an admin's email. A user finds their id at `/api/auth/me` when signed in. The file is read when analyze starts (a new admin needs a deploy or a restart of analyze).
- No session, another user, the bearer token, another method (POST, HEAD, ...), no file or an empty file, a day that is not 8 digits, no report: the plain 404 of a path that does not exist (`http.NotFound`, no `Allow` header), so the route does not show that it exists. The analyze service mounts the learn folder read-only at `/learn-reports`.

### D414. No cache
- https://www.rfc-editor.org/rfc/rfc9111#section-5.2.2.5 — `no-store`: a cache must not store the response. The report has the donors' edits (titles), so no browser or proxy keeps it.

### D415. The stack makes the folder and the file
- https://docs.docker.com/engine/storage/bind-mounts/ — a bind-mount source that does not exist is made by docker as a directory, so the stack script makes the file first.
- `tools/v3rde_stack.sh`: `$V3RDE_RUNS/learn` (owned by 65534 as root) and `$V3RDE_CFG/admin_users` (an empty 0600 file owned by 65534 when missing; else docker would make a folder). The edge check: `GET /api/analyze/learn` answers 404 without a session (an older image answers 404 too, so it shows only that nothing is open).

Own evidence: `TestLearnRouteAdminOnly` (the admin gets the days newest first, the JSON and the Markdown, with no-store; 10 other cases and 4 other methods get the same body as a path that does not exist; the admin's email as the id: 404; no learn folder or no admins: 404); the units checked with `systemd-analyze verify` and `systemd-analyze calendar` (next elapse 00:23 UTC); `tools/learn_round.sh` run against a real docker daemon (a compose project v3rde with the analyze image, its mounts and the admin file): it found the container, ran the job with no network, read-only as 65534, and wrote the two files; `-today` too.

Independent review (4 defects, all fixed): (1) the reports folder was mounted on `/learn`, the binary in the image (found also by the docker run: the container did not start): now `/learn-reports`; (2) see LR2; (3) another method got 405 and a refusal a JSON 404, so the route showed that it exists: now the plain 404 for all; (4) the admin check trusted an email that nobody checked: now the auth user id. Its notes fixed: the wait for analyze, the install checks before it writes, a quoted path in the unit. Limit: the timer is installed on the box by hand once (`sudo bash tools/learn_install.sh`), after the deploy of this change. Tests: inference 114 -> 115.
