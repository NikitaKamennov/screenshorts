#!/usr/bin/env bash
set -euo pipefail

SKRINPOLKA_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
SKRINPOLKA_PROFILE="$SKRINPOLKA_ROOT/config/runtime-linux.json"
if [[ -f "$SKRINPOLKA_ROOT/.local-data/background-linux/runtime-linux.json" ]]; then
  SKRINPOLKA_PROFILE="$SKRINPOLKA_ROOT/.local-data/background-linux/runtime-linux.json"
fi
cd -- "$SKRINPOLKA_ROOT"
exec env CGO_ENABLED=1 go run -mod=readonly -tags desktop,production,webkit2_41 \
  ./cmd/skrinpolka --data-root "$SKRINPOLKA_ROOT/.local-data/linux-dev" \
  --runtime-config "$SKRINPOLKA_PROFILE" "$@"
