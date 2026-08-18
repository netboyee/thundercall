#!/usr/bin/env sh
set -eu

PLATFORM="${PLATFORM:-linux/amd64}"
TAG_SUFFIX="${TAG_SUFFIX:-amd64}"

build_image() {
  target="$1"
  image="thundercall-${target}:${TAG_SUFFIX}"

  echo "==> building ${image} for ${PLATFORM}"
  docker buildx build \
    --platform "${PLATFORM}" \
    --target "${target}" \
    --tag "${image}" \
    --load \
    .
}

build_image api
build_image ingest
build_image worker
build_image voice-dispatcher

echo "Built images:"
echo "  thundercall-api:${TAG_SUFFIX}"
echo "  thundercall-ingest:${TAG_SUFFIX}"
echo "  thundercall-worker:${TAG_SUFFIX}"
echo "  thundercall-voice-dispatcher:${TAG_SUFFIX}"
