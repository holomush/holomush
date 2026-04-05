#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Build binary plugins discovered from plugin manifests.
#
# Usage:
#   ./scripts/build-plugins.sh              # build all binary plugins
#   ./scripts/build-plugins.sh core-scenes  # build a single plugin

set -euo pipefail

GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
BUILD_DIR="${BUILD_DIR:-build/plugins}"
PLUGINS_DIR="${PLUGINS_DIR:-plugins}"

build_plugin() {
  local dir="$1"
  local name
  name=$(basename "$dir")
  local manifest="$dir/plugin.yaml"

  if [ ! -f "$manifest" ]; then
    echo "WARN: no plugin.yaml in $dir, skipping" >&2
    return
  fi

  # Check type: binary
  local ptype
  ptype=$(grep -E '^type:\s+' "$manifest" | awk '{print $2}')
  if [ "$ptype" != "binary" ]; then
    return
  fi

  # Read executable name from binary-plugin.executable
  local executable
  executable=$(grep -E '^\s+executable:\s+' "$manifest" | awk '{print $2}')
  if [ -z "$executable" ]; then
    echo "ERROR: binary plugin $name has no executable in manifest" >&2
    exit 1
  fi

  local outdir="$BUILD_DIR/$name"
  mkdir -p "$outdir"

  echo "Building plugin $name → $outdir/$executable (${GOOS}/${GOARCH})"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags="-s -w" \
    -o "$outdir/$executable" "./$dir"

  cp "$manifest" "$outdir/plugin.yaml"
}

# Single-plugin mode
if [ $# -gt 0 ]; then
  target="$PLUGINS_DIR/$1"
  if [ ! -d "$target" ]; then
    echo "ERROR: plugin directory $target does not exist" >&2
    exit 1
  fi
  build_plugin "$target"
  exit 0
fi

# Discover all binary plugins
found=0
for manifest in "$PLUGINS_DIR"/*/plugin.yaml; do
  dir=$(dirname "$manifest")
  build_plugin "$dir"
  found=1
done

if [ "$found" -eq 0 ]; then
  echo "No plugins found in $PLUGINS_DIR" >&2
fi
