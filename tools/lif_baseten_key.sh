#!/usr/bin/env bash
# V3RDE LIF L4d: keep the Baseten key in a file, like the Meta key of the stack.
# It asks for the key (the typing is hidden), writes $V3RDE_CFG/baseten_api_key
# (default ~/.config/v3rde) with mode 0600, and never prints the key.
set -euo pipefail
CFG="${V3RDE_CFG:-$HOME/.config/v3rde}"
KEY="$CFG/baseten_api_key"
mkdir -p "$CFG"
chmod 700 "$CFG"
if [ -s "$KEY" ]; then
  printf '%s already exists. Replace it? [y/N] ' "$KEY"
  read -r ans </dev/tty
  [ "$ans" = y ] || [ "$ans" = Y ] || { echo "kept $KEY"; exit 0; }
fi
printf 'Baseten API key (hidden): '
IFS= read -rs k </dev/tty
echo
k="$(printf '%s' "$k" | tr -d '[:space:]')"
[ -n "$k" ] || { echo "no key typed: nothing written" >&2; exit 1; }
umask 077
tmp="$(mktemp "$CFG/.baseten_api_key.XXXXXX")"
printf '%s\n' "$k" > "$tmp"
chmod 600 "$tmp"
mv -f "$tmp" "$KEY"
unset k
echo "wrote $KEY (mode 0600); the key is not shown"
