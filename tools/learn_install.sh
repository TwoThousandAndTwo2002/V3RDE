#!/usr/bin/env bash
# LR3: install (or update) the daily learn timer on the box. Run as root from the checkout:
#
#   sudo bash tools/learn_install.sh            # install, enable and start the timer
#   sudo bash tools/learn_install.sh --remove   # stop and remove it
#
# It writes /etc/systemd/system/v3rde-learn.{service,timer} from deploy/learn (with this
# checkout's path), reloads systemd and enables the timer. It does not restart the stack and
# does not reboot.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[ "$(id -u)" = 0 ] || { echo "ABORT: run as root (sudo)"; exit 1; }
D=/etc/systemd/system
if [ "${1:-}" = "--remove" ]; then
  systemctl disable --now v3rde-learn.timer 2>/dev/null || true
  rm -f "$D/v3rde-learn.service" "$D/v3rde-learn.timer"
  systemctl daemon-reload
  echo "== learn timer removed"; exit 0
fi
case "$ROOT" in *'"'*|*'|'*|*'%'*) echo "ABORT: the checkout path has a character the unit cannot hold: $ROOT"; exit 1;; esac
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
sed "s|@ROOT@|$ROOT|g" "$ROOT/deploy/learn/v3rde-learn.service" > "$TMP/v3rde-learn.service"
cp "$ROOT/deploy/learn/v3rde-learn.timer" "$TMP/v3rde-learn.timer"
systemd-analyze verify "$TMP/v3rde-learn.service" "$TMP/v3rde-learn.timer"   # before anything is written
install -m 644 "$TMP/v3rde-learn.service" "$TMP/v3rde-learn.timer" "$D/"
systemctl daemon-reload
systemctl enable --now v3rde-learn.timer
systemctl list-timers v3rde-learn.timer --no-pager
echo "== learn timer installed (first run: the next time above; by hand: systemctl start v3rde-learn.service)"
