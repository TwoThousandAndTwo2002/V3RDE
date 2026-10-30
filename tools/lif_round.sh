#!/usr/bin/env bash
# X49b: one translation round (LIF) on the box, after a deploy (T9 2026-10-30: "as we update the
# app there has to be a translation round which rebuilds the relationship graph, creates a tag for
# the time being added or changed, and then runs translation on just that which has changed").
#
#   bash tools/lif_round.sh          # build the lif image if its stamp changed, then run the round
#   bash tools/lif_round.sh --dry    # the same, but the round only lists what it would translate
#
# The round (i18n/cmd/round in the v3rde-lif image) does nothing when the app text did not change.
# A translation failure never fails the deploy: this script reports it and ends with 0; only a
# setup error (no key file, too little memory) ends with 2. No key is printed.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export V3RDE_CFG="${V3RDE_CFG:-$HOME/.config/v3rde}"
export V3RDE_I18N="${V3RDE_I18N:-$HOME/V3RDE_i18n}"
export V3RDE_LIF_SEED="${V3RDE_LIF_SEED:-$HOME/V3RDE_lif_seed}"
export V3RDE_RUNS="${V3RDE_RUNS:-$HOME/V3RDE_runs}"
BUILT="${V3RDE_LIF_BUILT:-$HOME/built/LIF}"
dc() { docker compose -f "$ROOT/deploy/docker-compose.yml" --project-directory "$ROOT" --profile lif "$@"; }
say() { printf '== LIF %s\n' "$*"; }

for k in baseten_api_key meta_model_api_key; do   # a missing file would be mounted as an empty folder
  f="$V3RDE_CFG/$k"
  if [ ! -f "$f" ] || [ ! -s "$f" ]; then say "SKIPPED: no $k file in $V3RDE_CFG (put it there, 0600)"; exit 2; fi
done
mkdir -p "$V3RDE_I18N" "$V3RDE_LIF_SEED" "$V3RDE_RUNS/lif" "$(dirname "$BUILT")"

want=$(bash "$ROOT/tools/v3rde_stack.sh" stamps | awk '$1=="LIF"{print $2}')
case "$want" in ""|*dirty*|dev) say "ABORT: the stamp of the lif image is '${want}'"; exit 2;; esac
if ! docker image inspect v3rde-lif >/dev/null 2>&1 || [ "$(cat "$BUILT" 2>/dev/null)" != "$want" ]; then
  say "build v3rde-lif (stamp ${want}; the first build downloads torch and MetricX, several minutes)"
  t0=$(date +%s)
  dc build lif > "$V3RDE_RUNS/lif/build-$want.log" 2>&1 || { tail -30 "$V3RDE_RUNS/lif/build-$want.log"; say "ABORT: the build failed"; exit 2; }
  echo "$want" > "$BUILT"
  say "built in $(( $(date +%s) - t0 ))s"
else
  say "keep v3rde-lif (stamp $want)"
fi

# MetricX holds about 10 GB while it scores: start only with room for it and the stack
avail=$(awk '/MemAvailable/{print int($2/1024)}' /proc/meminfo 2>/dev/null || echo 0)
[ "$avail" -ge 10600 ] || { say "SKIPPED: ${avail} MB available, the round needs 10600 MB"; exit 2; }

args=(-repo /repo -state /state -out /out -qe local -batch 12
      -baseten-key-file /secrets/baseten_api_key -meta-key-file /secrets/meta_model_api_key)
[ -n "$(ls -A "$V3RDE_LIF_SEED" 2>/dev/null)" ] && args+=(-seed /seed)
[ "${1:-}" = "--dry" ] && args+=(-dry-run)
log="$V3RDE_RUNS/lif/round-$(date -u +%Y%m%dT%H%M%SZ).log"
say "round start (log $log)"
dc run --rm -T lif "${args[@]}" 2>&1 | tee "$log"
rc=${PIPESTATUS[0]}
case $rc in
  0) say "DONE (the round published, or the app text did not change)";;
  3) say "DONE WITH FAILED LANGUAGES (they keep their live files; see the log)";;
  *) say "FAILED (exit $rc); the live translations did not change; see the log";;
esac
exit 0
