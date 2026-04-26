#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# scripts/validate-plugin.sh
#
# Manifest validator for plugin authors. Parses the manifest and runs
# the same ValidateCrypto + ResolveCryptoRefs pipeline the loader uses,
# without actually loading the plugin.
#
# Usage: scripts/validate-plugin.sh <plugin-dir-or-yaml-path>

set -euo pipefail

target="${1:?usage: validate-plugin.sh <plugin-dir-or-yaml-path>}"

if [[ -d "$target" ]]; then
    manifest="$target/plugin.yaml"
elif [[ -f "$target" ]]; then
    manifest="$target"
else
    echo "validate-plugin: $target does not exist" >&2
    exit 2
fi

if [[ ! -f "$manifest" ]]; then
    echo "validate-plugin: $manifest not found" >&2
    exit 2
fi

# Invoke the Go-side validator via the holomush CLI.
go run ./cmd/holomush plugin validate "$manifest"
