#!/bin/sh
set -eu
VERSION="${AGENT_COMMS_VERSION:-}"
INSTALL_DIR="${AGENT_COMMS_INSTALL_DIR:-$HOME/.local/bin}"
REPO="DhanushSantosh/AgentComms"
command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
echo "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-(preview|rc)\.[0-9]+)?$' || { echo "AGENT_COMMS_VERSION must name an exact release (for example v0.6.0)" >&2; exit 1; }
OS=$(uname -s | tr '[:upper:]' '[:lower:]'); [ "$OS" = "darwin" ] || OS=linux
case "$(uname -m)" in arm64|aarch64) ARCH=arm64;; x86_64|amd64) ARCH=amd64;; *) echo "unsupported architecture" >&2; exit 1;; esac
RELEASE_URL="https://api.github.com/repos/$REPO/releases/tags/$VERSION"
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
curl -fsSL "$RELEASE_URL" -o "$TMP/release.json"
python3 - "$TMP/release.json" "$VERSION" > "$TMP/urls" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); version=sys.argv[2]
if not d or d.get('draft') or d.get('tag_name') != version: raise SystemExit('no matching published release')
print(d['tag_name'])
for a in d['assets']: print(a['name']+' '+a['browser_download_url'])
PY
TAG=$(head -1 "$TMP/urls"); NAME="agent-comms-$OS-$ARCH"
VERIFIER="agent-comms-verify-$OS-$ARCH"
asset_url(){ awk -v n="$1" '$1==n{print $2}' "$TMP/urls"; }
for F in "$NAME" "$VERIFIER" checksums.txt "$NAME.bundle"; do U=$(asset_url "$F"); [ -n "$U" ] || { echo "release missing $F" >&2; exit 1; }; curl -fsSL "$U" -o "$TMP/$F"; done
curl -fsSL "https://raw.githubusercontent.com/$REPO/$VERSION/release-verifier-checksums.txt" -o "$TMP/verifier-pins.txt"
checksum_of(){ sha256sum "$TMP/$1" 2>/dev/null | awk '{print $1}' || shasum -a 256 "$TMP/$1" | awk '{print $1}'; }
PINNED=$(awk -v v="$VERSION" -v n="$VERIFIER" '$1==v && $2==n{print $3}' "$TMP/verifier-pins.txt")
case "$PINNED" in *' '*|'') echo "release tag has no unique verifier pin for $VERIFIER" >&2; exit 1;; esac
ACTUAL=$(checksum_of "$VERIFIER")
[ "$PINNED" = "$ACTUAL" ] || { echo "trusted verifier SHA-256 verification failed" >&2; exit 1; }
for F in "$NAME"; do
  EXPECTED=$(awk -v n="$F" '$2==n{print $1}' "$TMP/checksums.txt")
  ACTUAL=$(checksum_of "$F")
  [ "$EXPECTED" = "$ACTUAL" ] || { echo "SHA-256 verification failed for $F" >&2; exit 1; }
done
chmod +x "$TMP/$VERIFIER"
IDENTITY=$(printf '%s' "https://github.com/$REPO/.github/workflows/release.yml@refs/tags/$VERSION" | sed 's/[.[\*^$()+?{|]/\\&/g')
"$TMP/$VERIFIER" --bundle "$TMP/$NAME.bundle" --certificate-identity-regexp "^${IDENTITY}$" --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' "$TMP/$NAME"
mkdir -p "$INSTALL_DIR"; [ ! -f "$INSTALL_DIR/agent-comms" ] || cp "$INSTALL_DIR/agent-comms" "$INSTALL_DIR/agent-comms.previous"
install -m 0755 "$TMP/$NAME" "$INSTALL_DIR/agent-comms"
# agc: a relative, by-name symlink so it keeps resolving after
# `agent-comms update` replaces the binary in place. RFC 0030.
ln -sf agent-comms "$INSTALL_DIR/agc"
echo "Installed Agent Comms $TAG to $INSTALL_DIR/agent-comms (also as $INSTALL_DIR/agc)"
