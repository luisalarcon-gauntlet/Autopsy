#!/usr/bin/env bash
# generate-bundle.sh — Generates a Replicated Troubleshoot support bundle
# from the demo kind cluster created by setup-cluster.sh.
#
# Prerequisites:
#   - kubectl support-bundle plugin (installed if absent via krew or direct download)
#   - An active kind cluster named "autopsy-demo"

set -euo pipefail

CLUSTER_NAME="autopsy-demo"
OUTPUT_FILE="demo-bundle-$(date +%Y%m%d-%H%M%S).tar.gz"

# ── Check cluster context ─────────────────────────────────────────────────────
if ! kubectl config get-contexts "kind-${CLUSTER_NAME}" &>/dev/null; then
  echo "ERROR: Kind cluster '${CLUSTER_NAME}' not found." >&2
  echo "       Run 'make demo-cluster' first." >&2
  exit 1
fi

kubectl config use-context "kind-${CLUSTER_NAME}"

# ── Install support-bundle plugin if missing ──────────────────────────────────
if ! kubectl support-bundle version &>/dev/null 2>&1; then
  echo "==> Installing kubectl support-bundle plugin..."

  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "ERROR: Unsupported architecture: $ARCH" >&2; exit 1 ;;
  esac

  VERSION="v0.99.0"
  URL="https://github.com/replicatedhq/troubleshoot/releases/download/${VERSION}/support-bundle_${OS}_${ARCH}.tar.gz"
  TMP=$(mktemp -d)
  curl -sSL "$URL" | tar xz -C "$TMP"
  chmod +x "$TMP/support-bundle"

  # Install to a directory in PATH.
  if [[ -d "$HOME/.local/bin" ]]; then
    mv "$TMP/support-bundle" "$HOME/.local/bin/kubectl-support_bundle"
    echo "    Installed to $HOME/.local/bin/kubectl-support_bundle"
  else
    sudo mv "$TMP/support-bundle" /usr/local/bin/kubectl-support_bundle
    echo "    Installed to /usr/local/bin/kubectl-support_bundle"
  fi
  rm -rf "$TMP"
fi

# ── Generate the bundle ───────────────────────────────────────────────────────
echo "==> Generating support bundle..."
echo "    Output: $OUTPUT_FILE"

kubectl support-bundle \
  --output "$OUTPUT_FILE" \
  --collect-without-permissions \
  2>&1 | grep -v "^$" || true

if [[ -f "$OUTPUT_FILE" ]]; then
  SIZE=$(du -sh "$OUTPUT_FILE" | cut -f1)
  SHA256=$(sha256sum "$OUTPUT_FILE" 2>/dev/null || shasum -a 256 "$OUTPUT_FILE" | awk '{print $1}')
  echo ""
  echo "==> Bundle generated successfully!"
  echo "    File:   $OUTPUT_FILE"
  echo "    Size:   $SIZE"
  echo "    SHA256: $SHA256"
  echo ""
  echo "Upload $OUTPUT_FILE to http://localhost:8080 to analyse it."
else
  echo "ERROR: Bundle file not created. Check kubectl support-bundle output above." >&2
  exit 1
fi
