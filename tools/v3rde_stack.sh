#!/usr/bin/env bash
# V3RDE stack script (written new for V3RDE, NEW_WORK.md 31). The same
# script runs on the Mac and on the EC2 box.
#
#   bash tools/v3rde_stack.sh up       # build the images, start the stack, check it
#   STACK_NO_BUILD=1 bash tools/v3rde_stack.sh up   # start with the images already built
#   bash tools/v3rde_stack.sh stamps   # print "<image> <stamp>" for each image (D151)
#   bash tools/v3rde_stack.sh down     # stop (the data volumes are kept)
#   bash tools/v3rde_stack.sh logs     # follow all logs
#   bash tools/v3rde_stack.sh smoke <photo.jpg>   # one /v1/analyze call with the bearer token
#
# Folders (set them from outside; the box uses /opt/v3rde/...):
#   V3RDE_CFG     secrets: meta_model_api_key (made by hand, 0600), analyze_token and
#                 fanout_status_token (made here when missing, 0600), google_maps_api_key (X49j: an
#                 empty file when missing: no map), smtp_user and smtp_pass (X49o: empty files when
#                 missing: no reset mail), admin_users (LR3: auth user ids of the admins, empty when missing: no admin)
#   V3RDE_ASSETS  products.sqlite, gate_pe_s16_f16w_384.tflite, gate_head_pe_s16_384_deploy.json
#   V3RDE_PHOTOS  the photo store (items/, touchups/)
#   V3RDE_RUNS    the event logs, the sidecar spools and the ledger
#   V3RDE_PORT    the host port of the edge, on 127.0.0.1 (8080)
#   V3RDE_WEB     the folder the edge serves at / (mobile/web-dist, the app; D191)
#   V3RDE_I18N    X49b: the published translations, served at /i18n/ (the lif job writes them)
# No secret is printed: checks report counts, owners and modes only.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export V3RDE_CFG="${V3RDE_CFG:-$HOME/.config/v3rde}"
export V3RDE_ASSETS="${V3RDE_ASSETS:-/opt/v3rde/assets}"
export V3RDE_PHOTOS="${V3RDE_PHOTOS:-$HOME/V3RDE_photos}"
export V3RDE_RUNS="${V3RDE_RUNS:-$HOME/V3RDE_runs}"
export V3RDE_PORT="${V3RDE_PORT:-8080}"
export V3RDE_I18N="${V3RDE_I18N:-$HOME/V3RDE_i18n}"           # X49b: the published translations (/i18n/)
export V3RDE_LIF_SEED="${V3RDE_LIF_SEED:-$HOME/V3RDE_lif_seed}" # X49b: the first round's start (the Mac's runs)
export COOKIE_SECURE="${COOKIE_SECURE:-true}" ANALYZE_RATE_BURST="${ANALYZE_RATE_BURST:-6}"
# X57: the push settings (not secret) from $V3RDE_CFG/push.env, lines KEY=value; only these three keys are read.
# No file or no value: push stays off (the app polls as before).
if [ -f "$V3RDE_CFG/push.env" ]; then
  while IFS='=' read -r k v; do
    case "$k" in V3RDE_PUSH_ENDPOINT|V3RDE_PUSH_URL|V3RDE_PUSH_REGION) [ -n "$v" ] && export "$k=$v" ;; esac
  done < <(grep -E '^V3RDE_PUSH_(ENDPOINT|URL|REGION)=' "$V3RDE_CFG/push.env" | tr -d '\r"'"'"' ')
fi
PATH="/opt/homebrew/bin:/usr/local/bin:/snap/bin:$PATH"
SERVICES="analyze auth listings barcode gate touchup rescue fanout voice push"   # WS1: push   # each writes $V3RDE_RUNS/<name>

say()  { printf '\n== %s ==\n' "$*"; }
die()  { printf '\nABORT: %s\n' "$*" >&2; exit 1; }
dc()   { docker compose -f "$ROOT/deploy/docker-compose.yml" --project-directory "$ROOT" "$@"; }
perm() { if [ "$(uname -s)" = Darwin ]; then stat -f %Lp "$1"; else stat -c %a "$1"; fi; }
newsecret() { od -An -tx1 -N32 /dev/urandom | tr -d ' \n'; }
asroot() { [ "$(id -u)" = 0 ]; }

# D151: each image's stamp is the last commit that changed its build
# context, with "-dirty" when that context has changes not committed. The
# same code gives the same stamp, so its build is a cache hit
# (docs.docker.com/build/cache/invalidation) and its sidecar label is right.
context() {
  case "$1" in
    AUTH) echo auth ;; LISTINGS) echo listings ;; BARCODE) echo barcode ;; GATE) echo gate ;;
    TELEMETRY) echo telemetry ;;
    # what inference/Dockerfile and post/Dockerfile COPY from the other modules (":(glob)": not the subfolders)
    ANALYZE) echo "inference server/go.mod :(glob)server/*.go" ;;
    # X49b: post also builds the inference Go files since X48o (post/Dockerfile COPY inference/...)
    POST) echo "post server/go.mod :(glob)server/*.go listings/go.mod :(glob)listings/*.go inference/go.mod inference/go.sum :(glob)inference/*.go inference/third_party" ;;
    LIF) echo "i18n qe/round.Dockerfile qe/round.Dockerfile.dockerignore qe/Dockerfile qe/run_predict.py" ;;   # X49b
    *) die "no build context for $1" ;;
  esac
}
IMAGES="AUTH LISTINGS BARCODE GATE TELEMETRY ANALYZE POST LIF"
stamp() {
  local paths s; paths=$(context "$1")
  # shellcheck disable=SC2086
  s=$(git -C "$ROOT" log -1 --format=%h -- $paths 2>/dev/null || true)
  [ -n "$s" ] || s=dev
  # changed tracked files AND new files not in git both make it dirty
  # shellcheck disable=SC2086
  [ -z "$(git -C "$ROOT" status --porcelain --untracked-files=all -- $paths 2>/dev/null)" ] || s="$s-dirty"
  # shellcheck disable=SC2086
  git -C "$ROOT" rev-parse --git-dir >/dev/null 2>&1 || s="dev-dirty"
  echo "$s"
}
export_stamps() { local i; for i in $IMAGES; do export "V3RDE_BUILD_$i=$(stamp "$i")"; done; }

secret_file() {   # $1 = path; made when missing; must be a file, 0600
  [ -e "$1" ] && [ ! -f "$1" ] && die "$1 is not a file (remove it; the next up makes it)"
  if [ ! -s "$1" ]; then
    mkdir -p "$V3RDE_CFG"; chmod 700 "$V3RDE_CFG"
    ( umask 077; newsecret > "$1" ); echo "   made $1"
  fi
  chmod 600 "$1"
  asroot && chown 65534:65534 "$1"   # the services run as 65534 and read it through a bind mount
  return 0
}

preflight() {
  command -v docker >/dev/null || die "docker is not on PATH"
  docker info >/dev/null 2>&1 || die "the docker daemon does not run"
  docker compose version >/dev/null 2>&1 || die "the docker compose plugin is missing"
  local key="$V3RDE_CFG/meta_model_api_key"
  [ -s "$key" ] || die "no Meta API key at $key"
  [ "$(perm "$key")" = 600 ] || die "$key must be mode 0600"
  asroot && chown 65534:65534 "$key"
  secret_file "$V3RDE_CFG/analyze_token"
  secret_file "$V3RDE_CFG/fanout_status_token"
  # X49j: the Google Maps browser key (x49m on the Mac, x49n on the box). Missing: an empty file, so the bind mount
  # is a file, not a folder Docker would make; the listings service then gives no key and the app shows no map.
  local maps="$V3RDE_CFG/google_maps_api_key"
  [ -e "$maps" ] && [ ! -f "$maps" ] && die "$maps is not a file (remove it; the next up makes it)"
  [ -e "$maps" ] || { ( umask 077; : > "$maps" ); echo "   made $maps (empty: no map in the app)"; }
  chmod 600 "$maps"
  asroot && chown 65534:65534 "$maps"
  # X49o: the reset mail relay user and app password (x49p on the Mac and the box). Missing: empty files, so the
  # bind mounts are files; the auth service then answers 503 to a reset request.
  local sf
  # FB: the Facebook Page token (fb2 on the Mac and the box). Missing: an empty file; fan-out then does not post.
  # LR3: admin_users, the accounts (auth user ids, one per line or comma-separated) that may read the learn reports.
  # Missing: an empty file; then nobody is an admin and the route answers 404.
  for sf in "$V3RDE_CFG/smtp_user" "$V3RDE_CFG/smtp_pass" "$V3RDE_CFG/fb_page_token" "$V3RDE_CFG/admin_users" "$V3RDE_CFG/push_secret"; do
    [ -e "$sf" ] && [ ! -f "$sf" ] && die "$sf is not a file (remove it; the next up makes it)"
    [ -e "$sf" ] || { ( umask 077; : > "$sf" ); echo "   made $sf (empty: no reset mail / no Page posts / no admin / no push)"; }
    chmod 600 "$sf"
    asroot && chown 65534:65534 "$sf"
  done
  # an edge of another project (for example an old stack) on our port would
  # answer our checks: stop here instead
  local other
  other=$(docker ps --filter "publish=$V3RDE_PORT" --format '{{.Names}} {{.Label "com.docker.compose.project"}}' | awk '$2!="v3rde"{print $1}')
  [ -z "$other" ] || die "port $V3RDE_PORT is held by a container that is not v3rde: $other (stop it first)"
  local f d
  for f in products.sqlite gate_pe_s16_f16w_384.tflite gate_head_pe_s16_384_deploy.json; do
    [ -s "$V3RDE_ASSETS/$f" ] || die "missing asset $V3RDE_ASSETS/$f"
  done
  # made here, so Docker does not make them as root; the services run as 65534
  for d in $SERVICES; do mkdir -p "$V3RDE_RUNS/$d" "$V3RDE_RUNS/telemetry/$d"; done
  mkdir -p "$V3RDE_RUNS/ledger" "$V3RDE_RUNS/learn" "$V3RDE_PHOTOS/items" "$V3RDE_PHOTOS/touchups"   # LR3: learn
  mkdir -p "$V3RDE_I18N" "$V3RDE_LIF_SEED"   # X49b: else docker makes them as root-owned empty folders
  if asroot; then
    for d in $SERVICES; do chown 65534:65534 "$V3RDE_RUNS/$d" "$V3RDE_RUNS/telemetry/$d"; done
    chown 65534:65534 "$V3RDE_RUNS/telemetry" "$V3RDE_RUNS/ledger" "$V3RDE_RUNS/learn" "$V3RDE_PHOTOS" "$V3RDE_PHOTOS/items" "$V3RDE_PHOTOS/touchups"
  fi
}

code() { curl -s -m 5 -o /dev/null -w '%{http_code}' "$@" 2>/dev/null || true; }
check() {   # $1 label, $2 want, rest: curl args
  local label=$1 want=$2 got; shift 2; got=$(code "$@")
  printf '   %-30s %s (want %s)%s\n' "$label" "$got" "$want" "$([ "$got" = "$want" ] || echo '  <-- CHECK')"
  [ "$got" = "$want" ] || BAD=$((BAD+1))
}

up() {
  export_stamps
  say "0 preflight"; preflight
  say "1 config"
  dc config -q || die "the compose file does not parse"
  echo "   compose OK"
  local v
  v=$(docker run --rm -v "$ROOT/deploy/Caddyfile:/etc/caddy/Caddyfile:ro" caddy:2.8.4 \
    caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile 2>&1) || { echo "$v" | tail -5; die "the Caddyfile is not valid"; }
  echo "   $(echo "$v" | tail -1)"
  local log; log=$(mktemp)
  say "2 build"
  local nb=""
  if [ "${STACK_NO_BUILD:-}" = 1 ]; then echo "   skipped (STACK_NO_BUILD=1)"; nb="--no-build"
  else
    # a failed build stops here: the sidecars must not show a stamp the service does not run (D151)
    dc build >"$log" 2>&1 || { tail -30 "$log"; die "the build failed"; }
    grep -E "Built" "$log" | sed 's/^/   /' || true
  fi
  say "3 start"
  # shellcheck disable=SC2086
  dc up -d --remove-orphans $nb >"$log" 2>&1 || { tail -30 "$log"; die "compose up failed"; }
  grep -E "Started|Running|Created" "$log" | sed 's/^/   /' || true
  # the Caddyfile is a one-file bind mount: a new file (new inode) needs a new container
  dc up -d --force-recreate --no-deps caddy >"$log" 2>&1 || { tail -30 "$log"; die "the edge did not start"; }
  rm -f "$log"
  local i; for i in $(seq 1 40); do [ "$(code -X POST "http://127.0.0.1:$V3RDE_PORT/v1/analyze")" = 401 ] && break; sleep 1; done
  say "4 checks through the edge (127.0.0.1:$V3RDE_PORT)"
  BAD=0; local u="http://127.0.0.1:$V3RDE_PORT"
  check "/healthz" 200 "$u/healthz"
  check "/api/auth/healthz" 200 "$u/api/auth/healthz"
  check "/api/listings/healthz" 200 "$u/api/listings/healthz"
  check "/ (the app)" 200 "$u/"
  # D191: the page's own script bundle is served (a stale or missing web build would 404 or give index.html)
  local js; js=$(curl -s -m 5 "$u/" 2>/dev/null | grep -o '/_expo/static/js/web/[A-Za-z0-9._-]*\.js' | head -1 || true)
  if [ -n "$js" ]; then
    check "app script ${js##*/}" 200 "$u$js"
    # a missing file also gets 200 (index.html, try_files): the type and the cache rule show it is the script
    local ct cc; ct=$(curl -s -m 5 -o /dev/null -w '%{content_type}' "$u$js" 2>/dev/null || true)
    cc=$(curl -s -m 5 -o /dev/null -D - "$u$js" 2>/dev/null | tr -d '\r' | grep -i '^cache-control:' | cut -c16-70 || true)
    printf '   %-30s %s / %s%s\n' "app script type / cache" "${ct:-none}" "${cc:-none}" \
      "$(case "$ct$cc" in *javascript*immutable*) ;; *) echo '  <-- CHECK';; esac)"
    case "$ct$cc" in *javascript*immutable*) ;; *) BAD=$((BAD+1));; esac
  else
    printf '   %-30s %s\n' "app script" "not in the page  <-- CHECK"; BAD=$((BAD+1))
  fi
  check "POST /v1/analyze" 401 -X POST "$u/v1/analyze"
  check "POST /api/analyze" 401 -X POST "$u/api/analyze"
  check "GET /api/analyze/scan/<id>" 401 "$u/api/analyze/scan/00000000000000000000000000000000"   # X44b: the scan job route
  check "GET /api/analyze/learn" 404 "$u/api/analyze/learn"   # LR3: admin sessions only; any other caller gets 404
  for p in /api/touchup /api/touchup/batch /api/rescue/l_0000000000000000 /api/fanout/l_0000000000000000 /api/voice /v1/touchup /v1/touchup/batch /v1/voice; do check "POST $p" 401 -X POST "$u$p"; done
  for p in /api/scan /api/inventory /v1/inventory; do check "POST $p" 410 -X POST "$u$p"; done
  check "POST /v1/fanout/status" 404 -X POST "$u/v1/fanout/status"
  local s h
  # US: auth, barcode, gate and ledger answer on their unix sockets (the binary probes its own socket);
  # barcode, gate and ledger have no TCP port any more
  for s in auth barcode gate ledger; do
    # the probe prints the body also on a 503 (a gate with no model): its exit status decides
    if h=$(dc exec -T "$s" "/$s" -probe-socket "/sock/$s.sock" </dev/null 2>&1); then h=$(printf '%s' "$h" | head -1 | cut -c1-150)
    else h="$(printf '%s' "$h" | tail -1 | cut -c1-110)  <-- CHECK"; BAD=$((BAD+1)); fi
    printf '   %-30s %s\n' "$s (socket)" "$h"
  done
  for s in barcode:8083 gate:8084 ledger:8086; do
    if dc exec -T caddy wget -qO- -T 3 "http://$s/healthz" </dev/null >/dev/null 2>&1; then
      printf '   %-30s %s\n' "${s%%:*} on TCP" "answers  <-- CHECK (want: no TCP port)"; BAD=$((BAD+1))
    else
      printf '   %-30s %s\n' "${s%%:*} on TCP" "no answer (want no answer)"
    fi
  done
  for s in analyze:8087 touchup:8088 rescue:8089 fanout:8090 voice:8091 push:8092; do
    h=$(dc exec -T caddy wget -qO- -T 5 "http://$s/healthz" </dev/null 2>/dev/null | cut -c1-150 || true)
    [ -n "$h" ] || { h="did not answer  <-- CHECK"; BAD=$((BAD+1)); }
    printf '   %-30s %s\n' "${s%%:*} (internal)" "$h"
    # FB: with a Page token on the box, fan-out must say it posts ("page":true)
    if [ "${s%%:*}" = fanout ] && [ -s "$V3RDE_CFG/fb_page_token" ] && [ "${FANOUT_PAGE_ID-x}" != "" ]; then
      case "$h" in *'"page":true'*) echo "   fanout posts to the Page        yes (want yes)";;
        *) echo "   fanout posts to the Page        no (want yes)  <-- CHECK"; BAD=$((BAD+1));; esac
    fi
  done
  local ids n f
  ids=$(dc ps -q) || die "docker compose ps failed"
  [ -n "$ids" ] || die "no containers"
  for f in meta_model_api_key fanout_status_token analyze_token smtp_pass fb_page_token push_secret; do
    [ -s "$V3RDE_CFG/$f" ] || continue # an empty file matches every line (X49o: smtp_pass is empty until x49p)
    # shellcheck disable=SC2086
    n=$(docker inspect $ids | grep -cFf "$V3RDE_CFG/$f" || true)
    printf '   %-30s %s times (want 0)%s\n' "$f in docker inspect" "${n:-0}" "$([ "${n:-0}" = 0 ] || echo '  <-- CHECK')"
    [ "${n:-0}" = 0 ] || BAD=$((BAD+1))
  done
  for i in $IMAGES; do printf '   stamp %-10s %s\n' "$i" "$(eval echo "\$V3RDE_BUILD_$i")"; done
  [ $BAD = 0 ] || die "$BAD checks are not as wanted"
  echo; echo "== UP == http://127.0.0.1:$V3RDE_PORT"
}

case "${1:-up}" in
  up) up ;;
  stamps) for i in $IMAGES; do echo "$i $(stamp "$i")"; done ;;
  down) export_stamps; dc down && echo "== DOWN == (volumes kept)" ;;
  logs) export_stamps; dc logs -f --tail=100 ;;
  smoke)
    img="${2:-}"; [ -f "$img" ] || die "usage: $0 smoke <photo.jpg>"
    curl -sS -m 90 -w '\n   HTTP %{http_code} in %{time_total}s\n' \
      -H @<(printf 'Authorization: Bearer %s\n' "$(cat "$V3RDE_CFG/analyze_token")") \
      -F "image=@$img;type=image/jpeg" "http://127.0.0.1:$V3RDE_PORT/v1/analyze" | cut -c1-300 ;;
  *) die "usage: $0 up|stamps|down|logs|smoke <photo>" ;;
esac
