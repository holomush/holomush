#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# scripts/gen-event-docs.sh — regenerate site/docs/reference/events/*.md
# from plugin manifests. Idempotent. Safe to run repeatedly.

set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
plugin_dir="$repo_root/plugins"
out_dir="$repo_root/site/docs/reference/events"

mkdir -p "$out_dir"

# Build the holomush binary into a temp location so we don't depend on
# what's installed.
tmp_bin_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_bin_dir"' EXIT
bin="$tmp_bin_dir/holomush"
( cd "$repo_root" && go build -o "$bin" ./cmd/holomush )

# One page per plugin that declares any crypto.emits.
generated_count=0
for plugin_path in "$plugin_dir"/*/; do
    plugin="$(basename "$plugin_path")"
    if [[ ! -f "$plugin_path/plugin.yaml" ]]; then
        continue
    fi
    # Skip plugins with no crypto.emits — nothing to document.
    listing="$("$bin" plugin events list --plugin-dir "$plugin_dir" --plugin "$plugin" 2>/dev/null || true)"
    if [[ -z "$listing" ]]; then
        # Remove a previously-generated stale page if it exists.
        rm -f "$out_dir/$plugin.md"
        continue
    fi
    out="$out_dir/$plugin.md"
    {
        printf "# %s — events\n\n" "$plugin"
        printf "_Auto-generated from \`plugins/%s/plugin.yaml\` by \`task docs:gen-events\`. Do not edit._\n\n" "$plugin"
        printf "| Event type | Sensitivity | Description |\n"
        printf "| --- | --- | --- |\n"
        echo "$listing" | awk '{
            etype = $1
            sens = $2
            $1 = $2 = ""
            desc = $0
            gsub(/^ +| +$/, "", desc)
            printf "| `%s` | %s | %s |\n", etype, sens, desc
        }'
        printf "\n"
    } > "$out"
    generated_count=$((generated_count + 1))
done

# Re-emit the top-level index.
{
    printf "# Event type reference\n\n"
    printf "Per-plugin event-type catalogues, auto-generated from plugin manifests.\n\n"
    printf "Each event type identifier is qualified with its owning plugin, e.g. \`core-communication:whisper\`.\n\n"
    for plugin_md in "$out_dir"/*.md; do
        [[ -f "$plugin_md" ]] || continue
        plugin="$(basename "$plugin_md" .md)"
        printf -- "- [%s](events/%s.md)\n" "$plugin" "$plugin"
    done
} > "$repo_root/site/docs/reference/events.md"

echo "Generated $generated_count plugin event pages + index"
