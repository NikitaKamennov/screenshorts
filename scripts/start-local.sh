#!/usr/bin/env bash
set -euo pipefail

SKRINPOLKA_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
SKRINPOLKA_BINARY="$SKRINPOLKA_ROOT/.local-data/build/linux-x64/ScreenShooterPro"
if [[ ! -x "$SKRINPOLKA_BINARY" ]]; then
  echo 'Сначала подготовь каркас: bash scripts/build-linux.sh' >&2
  exit 1
fi
cd -- "$SKRINPOLKA_ROOT"
SKRINPOLKA_PROFILE="$SKRINPOLKA_ROOT/config/runtime-linux.json"
if [[ -f "$SKRINPOLKA_ROOT/.local-data/background-linux/runtime-linux.json" ]]; then
  SKRINPOLKA_PROFILE="$SKRINPOLKA_ROOT/.local-data/background-linux/runtime-linux.json"
fi
exec "$SKRINPOLKA_BINARY" --data-root "$SKRINPOLKA_ROOT/.local-data/linux-dev" --runtime-config "$SKRINPOLKA_PROFILE" "$@"
