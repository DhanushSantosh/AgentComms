#!/bin/sh
set -eu
CHANNEL="${AGENT_COMMS_CHANNEL:-stable}"
VERSION="${AGENT_COMMS_VERSION:-}"
INSTALL_DIR="${AGENT_COMMS_INSTALL_DIR:-$HOME/.local/bin}"
REPO="DhanushSantosh/AgentComms"
command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
command -v cosign >/dev/null || { echo "cosign is required: https://docs.sigstore.dev/cosign/system_config/installation/" >&2; exit 1; }
OS=$(uname -s | tr '[:upper:]' '[:lower:]'); [ "$OS" = "darwin" ] || OS=linux
case "$(uname -m)" in arm64|aarch64) ARCH=arm64;; x86_64|amd64) ARCH=amd64;; *) echo "unsupported architecture" >&2; exit 1;; esac
if [ -n "$VERSION" ]; then RELEASE_URL="https://api.github.com/repos/$REPO/releases/tags/$VERSION"; else RELEASE_URL="https://api.github.com/repos/$REPO/releases"; fi
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
curl -fsSL "$RELEASE_URL" -o "$TMP/release.json"
python3 - "$TMP/release.json" "$CHANNEL" "$VERSION" > "$TMP/urls" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); channel,version=sys.argv[2:]
if isinstance(d,list): d=next((r for r in d if not r['draft'] and (channel=='preview' or not r['prerelease'])),None)
if not d: raise SystemExit('no matching release')
print(d['tag_name'])
for a in d['assets']: print(a['name']+' '+a['browser_download_url'])
PY
TAG=$(head -1 "$TMP/urls"); NAME="agent-comms-$OS-$ARCH"
asset_url(){ awk -v n="$1" '$1==n{print $2}' "$TMP/urls"; }
for F in "$NAME" checksums.txt "$NAME.bundle"; do U=$(asset_url "$F"); [ -n "$U" ] || { echo "release missing $F" >&2; exit 1; }; curl -fsSL "$U" -o "$TMP/$F"; done
EXPECTED=$(awk -v n="$NAME" '$2==n{print $1}' "$TMP/checksums.txt")
ACTUAL=$(sha256sum "$TMP/$NAME" 2>/dev/null | awk '{print $1}' || shasum -a 256 "$TMP/$NAME" | awk '{print $1}')
[ "$EXPECTED" = "$ACTUAL" ] || { echo "SHA-256 verification failed" >&2; exit 1; }
cosign verify-blob --bundle "$TMP/$NAME.bundle" --certificate-identity-regexp '^https://github.com/DhanushSantosh/AgentComms/.github/workflows/release.yml@refs/tags/' --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' "$TMP/$NAME"
mkdir -p "$INSTALL_DIR"; [ ! -f "$INSTALL_DIR/agent-comms" ] || cp "$INSTALL_DIR/agent-comms" "$INSTALL_DIR/agent-comms.previous"
install -m 0755 "$TMP/$NAME" "$INSTALL_DIR/agent-comms"
echo "Installed Agent Comms $TAG to $INSTALL_DIR/agent-comms"
