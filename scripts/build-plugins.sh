#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Build binary plugins discovered from plugin manifests.
#
# Builds for multiple platforms: host native + linux/amd64 + linux/arm64.
# Output layout:
#   build/plugins/<name>/plugin.yaml
#   build/plugins/<name>/<os>-<arch>/<executable>
#
# The plugin loader resolves the correct binary at runtime using GOOS/GOARCH.
#
# Platform selection:
#   Default (PLUGIN_PLATFORMS unset) → host native + linux/amd64 + linux/arm64,
#   the full matrix the Docker/release path needs.
#   Override with PLUGIN_PLATFORMS (space- or comma-separated); the literal
#   token `host` expands to the native GOOS-GOARCH. The integration/E2E test
#   path passes PLUGIN_PLATFORMS=host to skip the cross-compiles it never loads.
#
# Usage:
#   ./scripts/build-plugins.sh              # build all binary plugins
#   ./scripts/build-plugins.sh core-scenes  # build a single plugin
#   PLUGIN_PLATFORMS=host ./scripts/build-plugins.sh   # native target only

set -euo pipefail

PLUGINS_DIR="${PLUGINS_DIR:-plugins}"
BUILD_DIR="${BUILD_DIR:-build/plugins}"

# Platforms to build: host native + linux targets, unless PLUGIN_PLATFORMS
# narrows the set.
HOST_OS="$(go env GOOS)"
HOST_ARCH="$(go env GOARCH)"

declare -A PLATFORMS
if [ -n "${PLUGIN_PLATFORMS:-}" ]; then
  IFS=', ' read -r -a _requested_platforms <<< "$PLUGIN_PLATFORMS"
  for _platform in "${_requested_platforms[@]}"; do
    [ -z "$_platform" ] && continue
    [ "$_platform" = "host" ] && _platform="${HOST_OS}-${HOST_ARCH}"
    PLATFORMS["$_platform"]=1
  done
else
  PLATFORMS["${HOST_OS}-${HOST_ARCH}"]=1
  PLATFORMS["linux-amd64"]=1
  PLATFORMS["linux-arm64"]=1
fi

build_plugin_platform() {
  local dir="$1"
  local target_os="$2"
  local target_arch="$3"
  local name="$4"
  local executable="$5"

  local outdir="$BUILD_DIR/$name/${target_os}-${target_arch}"
  mkdir -p "$outdir"

  echo "  ${target_os}/${target_arch} → $outdir/$executable"
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -ldflags="-s -w" \
    -o "$outdir/$executable" "./$dir"
}

build_plugin() {
  local dir="$1"
  local name
  name=$(basename "$dir")
  local manifest="$dir/plugin.yaml"

  if [ ! -f "$manifest" ]; then
    echo "WARN: no plugin.yaml in $dir, skipping" >&2
    return
  fi

  local ptype
  ptype=$(grep -E '^type:\s+' "$manifest" | awk '{print $2}')
  if [ "$ptype" != "binary" ]; then
    return
  fi

  local executable
  executable=$(grep -E '^\s+executable:\s+' "$manifest" | awk '{print $2}')
  if [ -z "$executable" ]; then
    echo "ERROR: binary plugin $name has no executable in manifest" >&2
    exit 1
  fi

  echo "Building plugin: $name"

  # Copy manifest to plugin build dir
  mkdir -p "$BUILD_DIR/$name"
  cp "$manifest" "$BUILD_DIR/$name/plugin.yaml"

  # Also copy migrations if they exist
  if [ -d "$dir/migrations" ]; then
    cp -r "$dir/migrations" "$BUILD_DIR/$name/migrations"
  fi

  # Build for all target platforms
  for platform in "${!PLATFORMS[@]}"; do
    local os="${platform%-*}"
    local arch="${platform#*-}"
    build_plugin_platform "$dir" "$os" "$arch" "$name" "$executable"
  done
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
  [ -f "$manifest" ] || continue
  dir=$(dirname "$manifest")
  build_plugin "$dir"
  found=1
done

if [ "$found" -eq 0 ]; then
  echo "No plugins found in $PLUGINS_DIR" >&2
fi
