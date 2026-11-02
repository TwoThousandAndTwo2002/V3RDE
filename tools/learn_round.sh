#!/usr/bin/env bash
# LR3 (I1, T9 2026-11-01: a daily self-improvement report, "systemd timer"): one learn report on the
# box. The v3rde-learn.timer runs it once a day (deploy/learn, tools/learn_install.sh); it can also
# run by hand:
#
#   sudo bash tools/learn_round.sh            # the 7 whole UTC days before today
#   sudo bash tools/learn_round.sh -today     # the same, but ending with today so far
#
# It runs /learn from the image the analyze container runs, with no network, read-only, as uid
# 65534, on the analyze events (read-only) and the learn folder that analyze serves to admins
# (the two host folders of the running container's /events and /learn-reports mounts; /learn is
# the binary in the image). It changes no
# setting and no prompt. A failure never stops the stack; the exit code is the job's.
set -uo pipefail
say() { printf '== learn %s\n' "$*"; }
find_analyze() { docker ps --filter label=com.docker.compose.project=v3rde --filter label=com.docker.compose.service=analyze --format '{{.ID}}' | head -1; }
C=$(find_analyze)
for _ in $(seq 1 20); do   # a deploy at the time of the timer: wait up to 10 min for analyze
  [ -n "$C" ] && break
  sleep 30; C=$(find_analyze)
done
[ -n "$C" ] || { say "SKIPPED: the analyze container did not run for 10 min"; exit 2; }
src() { docker inspect -f '{{range .Mounts}}{{if eq .Destination "'"$1"'"}}{{.Source}}{{end}}{{end}}' "$C"; }
EV=$(src /events); OUT=$(src /learn-reports)
IMG=$(docker inspect -f '{{.Image}}' "$C")
[ -n "$EV" ] && [ -d "$EV" ] || { say "ABORT: no /events mount on the analyze container"; exit 2; }
[ -n "$OUT" ] || { say "ABORT: no /learn-reports mount on the analyze container (deploy the LR3 compose file first)"; exit 2; }
mkdir -p "$OUT"
[ "$(id -u)" = 0 ] && chown 65534:65534 "$OUT"
docker run --rm --network none --read-only --user 65534:65534 --memory 256m --cpus 1 \
  --security-opt no-new-privileges --cap-drop ALL \
  -v "$EV":/events:ro -v "$OUT":/learn-reports --entrypoint /learn "$IMG" \
  -events /events -last 7 -out /learn-reports -keep 90 "$@"
rc=$?
[ $rc = 0 ] && say "DONE ($(ls "$OUT" | grep -c '^report-.*\.json$') reports in the learn folder)" || say "FAILED (exit $rc)"
exit $rc
