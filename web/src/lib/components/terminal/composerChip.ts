// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import type { CommandListState } from '$lib/stores/commandListStore';

export type ChipKind = 'say' | 'pose' | 'ooc' | 'command';
export interface ComposerChip {
  kind: ChipKind;
  label: string; // lowercase; ModeChip uppercases via CSS
}

const SPEECH = new Set(['say', 'pose', 'ooc']);
// Single-char sigils that attach to text without a space (tier-2 prefix aliases).
const SIGILS = new Set(['"', ':', ';']);

// resolveComposerChip maps composer text to a chip via the server-sourced command
// set + alias map. Replaces the former hardcoded prefix matcher (design INV-4).
// Tier-3 (player aliases) seam: a future server ResolveInput RPC would slot in here.
export function resolveComposerChip(text: string, state: CommandListState): ComposerChip | null {
  const v = text.trimStart();
  if (v === '') return null;

  // Sigil-prefix aliases attach directly to text (":waves", '"hi'). Resolve the
  // leading sigil char before falling back to whitespace tokenization.
  const first = v[0];
  let token: string;
  if (SIGILS.has(first)) {
    token = first;
  } else {
    token = v.split(/\s+/, 1)[0];
  }

  // Guard the alias lookup against prototype-chain keys ("constructor",
  // "__proto__", "toString", …): bracket-indexing a plain object reaches
  // inherited members, which would otherwise yield a ghost chip with a
  // non-string label. Require an OWN property, then confirm the resolved
  // command is actually in the visible set — which also rejects a stale alias
  // whose target is absent from names (defensive against a server contract slip).
  const alias = Object.hasOwn(state.aliases, token) ? state.aliases[token] : undefined;
  const canonical = state.names.has(token) ? token : alias;
  if (canonical === undefined || !state.names.has(canonical)) return null;

  if (SPEECH.has(canonical)) {
    return { kind: canonical as ChipKind, label: canonical };
  }
  return { kind: 'command', label: canonical };
}
