#!/usr/bin/env sh
set -euo pipefail

if [ -z "${GHCR_TOKEN:-}" ]; then
  echo "error: GHCR_TOKEN is not set"
  exit 1
fi

TAG_ARG=""
if [ $# -gt 0 ] && [ "${1#-}" = "$1" ]; then
  TAG_ARG="$1"
  shift
fi

if [ -n "$TAG_ARG" ]; then
  export IMAGE_TAG="$TAG_ARG"
elif [ -z "${IMAGE_TAG:-}" ]; then
  export IMAGE_TAG="latest"
fi

echo "Using image tag: $IMAGE_TAG"

echo "$GHCR_TOKEN" | docker login ghcr.io -u iraj720 --password-stdin

if ! docker network inspect uploader-net >/dev/null 2>&1; then
  docker network create uploader-net
fi

docker compose -f docker-compose.postgres.yml -f docker-compose.yml up "$@"
