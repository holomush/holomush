// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import type { LogEntry } from '$lib/scenes/types';

// CommKind is the logical communication kind, vocabulary-independent.
// `emit` covers system/GM narration (pemit-styled).
export type CommKind = 'say' | 'pose' | 'ooc' | 'emit';

// CommLine is the normalized model every web communication surface renders.
// Optional fields are populated by the richer terminal vocabulary; scene
// events leave them undefined and the primitive applies defaults.
export interface CommLine {
  kind: CommKind;
  actor: string;
  text: string;
  label?: string; // say verb override; default "says"
  noSpace?: boolean; // semipose (no space before action)
  oocStyle?: 'say' | 'pose' | 'semipose'; // default "say"
  oocPrefix?: string; // default "[OOC]"
  channel?: string;
}

// CommEvent mirrors the terminal event shape consumed by EventRenderer /
// CommunicationRenderer (internal/web/translate.go produces it).
export interface CommEvent {
  type: string;
  category: string;
  format: string;
  actor: string;
  text: string;
  metadata?: Record<string, unknown>;
}

// commEventToLine adapts a core-communication:* terminal event into a CommLine.
// Preserves the exact branching CommunicationRenderer used: ooc by type or
// ooc_prefix; speech→say; action→pose; otherwise emit (pemit).
export function commEventToLine(event: CommEvent): CommLine {
  const md = event.metadata ?? {};
  const oocPrefix = md['ooc_prefix'] as string | undefined;
  const channel = md['channel'] as string | undefined;
  const isOoc = event.type === 'core-communication:ooc' || !!oocPrefix;

  if (isOoc) {
    return {
      kind: 'ooc',
      actor: event.actor,
      text: event.text,
      oocStyle: (md['style'] as 'say' | 'pose' | 'semipose') ?? 'say',
      oocPrefix: oocPrefix ?? '[OOC]',
      channel,
    };
  }
  if (event.format === 'speech') {
    return { kind: 'say', actor: event.actor, text: event.text, label: md['label'] as string | undefined, channel };
  }
  if (event.format === 'action') {
    return { kind: 'pose', actor: event.actor, text: event.text, noSpace: md['no_space'] as boolean | undefined, channel };
  }
  return { kind: 'emit', actor: event.actor, text: event.text };
}

// logEntryToLine adapts a scene LogEntry into a CommLine. Scene events are
// coarser (no label/no_space/ooc style metadata), so optional fields stay
// undefined and the primitive's defaults apply. `system` → `emit`.
export function logEntryToLine(entry: LogEntry): CommLine {
  const kind: CommKind = entry.kind === 'system' ? 'emit' : entry.kind;
  return { kind, actor: entry.actorName || entry.actorId || 'Unknown', text: entry.text };
}
