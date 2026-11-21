#!/usr/bin/env bash
set -euo pipefail
SCREENSHOOTER_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
exec /usr/bin/python3 -I "$SCREENSHOOTER_ROOT/scripts/linux/package.py" "$@"
