#!/usr/bin/env sh
set -euo pipefail

if [ -z "${GHCR_TOKEN:-}" ]; then
  echo "error: GHCR_TOKEN is not set"
  exit 1
fi

echo "$GHCR_TOKEN" | docker login ghcr.io -u iraj720 --password-stdin

#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

docker-compose up -d
echo "Uploader bot is starting (check logs with docker compose logs -f uploader)."
