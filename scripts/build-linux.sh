#!/usr/bin/env bash
set -euo pipefail

SKRINPOLKA_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
SKRINPOLKA_OUTPUT="$SKRINPOLKA_ROOT/.local-data/build/linux-x64/ScreenShooterPro"
if [[ "$(uname -s)" != "Linux" || "$(uname -m)" != "x86_64" ]]; then
  echo 'Этот скрипт готовит нативный Linux x86_64 каркас.' >&2
  exit 1
fi
if ! command -v go >/dev/null 2>&1; then
  echo 'Установи Go 1.25 или новее; смотри README.md.' >&2
  exit 1
fi
mkdir -p -- "$(dirname -- "$SKRINPOLKA_OUTPUT")"
cd -- "$SKRINPOLKA_ROOT"
CGO_ENABLED=1 go build -mod=readonly -trimpath -tags desktop,production,webkit2_41 \
  -ldflags '-s -w' -o "$SKRINPOLKA_OUTPUT" ./cmd/skrinpolka
mkdir -p -- "$(dirname -- "$SKRINPOLKA_OUTPUT")/config"
cp -- "$SKRINPOLKA_ROOT/config/runtime-linux.json" "$(dirname -- "$SKRINPOLKA_OUTPUT")/config/runtime-linux.json"
echo "Подготовлен каркас: $SKRINPOLKA_OUTPUT"
echo 'Приложение, тесты и модели скрипт не запускает.'
