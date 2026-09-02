#!/bin/sh
set -eu

VERSION=${1:-}
echo "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-(preview|rc)\.[0-9]+)?$' || { echo "usage: $0 vX.Y.Z" >&2; exit 2; }
RELEASE_GO_VERSION=go1.26.6

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "# Generated verifier pins for $VERSION. Commit before creating the tag."
for TARGET in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  GOOS=${TARGET%/*}
  GOARCH=${TARGET#*/}
  EXT=
  [ "$GOOS" = windows ] && EXT=.exe
  NAME="agent-comms-verify-$GOOS-$GOARCH$EXT"
  CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH GOTOOLCHAIN=$RELEASE_GO_VERSION go build -buildvcs=false -trimpath \
    -ldflags="-s -w -X github.com/DhanushSantosh/AgentComms/internal/buildinfo.Version=${VERSION#v}" \
    -o "$TMP/$NAME" ./cmd/agent-comms-verify
  DIGEST=$(sha256sum "$TMP/$NAME" 2>/dev/null | awk '{print $1}' || shasum -a 256 "$TMP/$NAME" | awk '{print $1}')
  echo "$VERSION $NAME $DIGEST"
done
