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
#                 fanout_status_token (made here when missing, 0600)
#   V3RDE_ASSETS  products.sqlite, gate_pe_s16_f16w_384.tflite, gate_head_pe_s16_384_deploy.json
#   V3RDE_PHOTOS  the photo store (items/, touchups/)
#   V3RDE_RUNS    the event logs, the sidecar spools and the ledger
#   V3RDE_PORT    the host port of the edge, on 127.0.0.1 (8080)
#   V3RDE_WEB     the folder the edge serves at / (deploy/web, the holding page)
# No secret is printed: checks report counts, owners and modes only.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export V3RDE_CFG="${V3RDE_CFG:-$HOME/.config/v3rde}"
export V3RDE_ASSETS="${V3RDE_ASSETS:-/opt/v3rde/assets}"
export V3RDE_PHOTOS="${V3RDE_PHOTOS:-$HOME/V3RDE_photos}"
export V3RDE_RUNS="${V3RDE_RUNS:-$HOME/V3RDE_runs}"
export V3RDE_PORT="${V3RDE_PORT:-8080}"
export COOKIE_SECURE="${COOKIE_SECURE:-true}" ANALYZE_RATE_BURST="${ANALYZE_RATE_BURST:-6}"
PATH="/opt/homebrew/bin:/usr/local/bin:/snap/bin:$PATH"
SERVICES="analyze auth listings barcode gate touchup rescue fanout voice"   # each writes $V3RDE_RUNS/<name>

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
    POST) echo "post server/go.mod :(glob)server/*.go listings/go.mod :(glob)listings/*.go" ;;
    *) die "no build context for $1" ;;
  esac
}
IMAGES="AUTH LISTINGS BARCODE GATE TELEMETRY ANALYZE POST"
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
  mkdir -p "$V3RDE_RUNS/ledger" "$V3RDE_PHOTOS/items" "$V3RDE_PHOTOS/touchups"
  if asroot; then
    for d in $SERVICES; do chown 65534:65534 "$V3RDE_RUNS/$d" "$V3RDE_RUNS/telemetry/$d"; done
    chown 65534:65534 "$V3RDE_RUNS/telemetry" "$V3RDE_RUNS/ledger" "$V3RDE_PHOTOS" "$V3RDE_PHOTOS/items" "$V3RDE_PHOTOS/touchups"
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
  check "/ (holding page or app)" 200 "$u/"
  check "POST /v1/analyze" 401 -X POST "$u/v1/analyze"
  check "POST /api/analyze" 401 -X POST "$u/api/analyze"
  check "GET /api/analyze/scan/<id>" 401 "$u/api/analyze/scan/00000000000000000000000000000000"   # X44b: the scan job route
  for p in /api/touchup /api/rescue/l_0000000000000000 /api/fanout/l_0000000000000000 /api/voice /v1/touchup /v1/voice; do check "POST $p" 401 -X POST "$u$p"; done
  for p in /api/scan /api/inventory /v1/inventory; do check "POST $p" 410 -X POST "$u$p"; done
  check "POST /v1/fanout/status" 404 -X POST "$u/v1/fanout/status"
  local s
  local s h
  for s in barcode:8083 gate:8084 analyze:8087 ledger:8086 touchup:8088 rescue:8089 fanout:8090 voice:8091; do
    h=$(dc exec -T caddy wget -qO- -T 5 "http://$s/healthz" </dev/null 2>/dev/null | cut -c1-150 || true)
    [ -n "$h" ] || { h="did not answer  <-- CHECK"; BAD=$((BAD+1)); }
    printf '   %-30s %s\n' "${s%%:*} (internal)" "$h"
  done
  local ids n f
  ids=$(dc ps -q) || die "docker compose ps failed"
  [ -n "$ids" ] || die "no containers"
  for f in meta_model_api_key fanout_status_token analyze_token; do
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
