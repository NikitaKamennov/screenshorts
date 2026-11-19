#!/usr/bin/env bash
set -euo pipefail
SCREENSHORTS_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
exec /usr/bin/python3 -I "$SCREENSHORTS_ROOT/scripts/linux/package.py" "$@"
