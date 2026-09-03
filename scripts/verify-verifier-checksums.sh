#!/bin/sh
set -eu

VERSION=${1:-}
DIST=${2:-dist}
MANIFEST=${3:-release-verifier-checksums.txt}
EXPECTED_ASSETS="agent-comms-verify-windows-amd64.exe agent-comms-verify-windows-arm64.exe agent-comms-verify-linux-amd64 agent-comms-verify-linux-arm64 agent-comms-verify-darwin-amd64 agent-comms-verify-darwin-arm64"

[ "$(awk -v v="$VERSION" '$1==v{count++} END{print count+0}' "$MANIFEST")" -eq 6 ] || { echo "$VERSION must have exactly six committed verifier pins" >&2; exit 1; }
for ASSET in $EXPECTED_ASSETS; do
  MATCHES=$(awk -v v="$VERSION" -v a="$ASSET" '$1==v && $2==a{print $3}' "$MANIFEST")
  [ "$(printf '%s\n' "$MATCHES" | grep -c .)" -eq 1 ] || { echo "$VERSION must have one pin for $ASSET" >&2; exit 1; }
  echo "$MATCHES" | grep -Eq '^[0-9a-f]{64}$' || { echo "invalid verifier pin for $ASSET" >&2; exit 1; }
  ACTUAL=$(sha256sum "$DIST/$ASSET" 2>/dev/null | awk '{print $1}' || shasum -a 256 "$DIST/$ASSET" | awk '{print $1}')
  [ "$ACTUAL" = "$MATCHES" ] || { echo "verifier pin mismatch for $ASSET" >&2; exit 1; }
done
