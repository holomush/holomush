// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { exits } from './sidebarStore';
import type { RoomExit } from './sidebarStore';
import { appendLine, replayActive } from './terminalStore';
import { applyLocationState } from './sidebarStore';
import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';

// DisplayTarget values from the GameEvent proto (renamed from EventChannel).
const DISPLAY_UNSPECIFIED = 0;
const DISPLAY_TERMINAL = 1;
const DISPLAY_STATE = 2;
const DISPLAY_BOTH = 3;

export function routeEvent(event: GameEvent, replayed: boolean) {
  const target = (event as Record<string, unknown>).displayTarget as number ?? DISPLAY_UNSPECIFIED;

  // Route to terminal (scrollback)
  if (target === DISPLAY_TERMINAL || target === DISPLAY_BOTH || target === DISPLAY_UNSPECIFIED) {
    // GameEvent.timestamp is bigint Unix millis; convert to number for Date construction.
    const ms = Number(event.timestamp ?? 0n);
    appendLine(event, replayed, ms);
  }

  // Route to sidebar stores. location_state is always applied (it's the
  // authoritative snapshot, including the synthetic one at stream start).
  // arrive/leave deltas are suppressed during replay to avoid applying
  // stale history on top of the snapshot.
  if (target === DISPLAY_STATE || target === DISPLAY_BOTH) {
    routeToSidebar(event, replayed);
  }
}

function routeToSidebar(event: GameEvent, replayed: boolean) {
  const data = metadataToPlain(event.metadata);
  const category = (event as Record<string, unknown>).category as string | undefined;

  switch (category) {
    case 'state':
      routeStateEvent(event.type, data, replayed);
      break;
    // 'movement': no-op here — mirrorMovementPresence in +page.svelte is the
    // sole writer for live movement events to the PresenceStore (T12, holomush-5b2j.14).
  }
}

function routeStateEvent(type: string, data: Record<string, unknown> | null, replayed: boolean) {
  switch (type) {
    case 'location_state':
      // Always apply — this is the authoritative snapshot (including synthetic at stream start).
      if (data) {
        applyLocationState(data);
      } else {
        console.warn('[eventRouter] location_state event received with unparseable metadata');
      }
      break;
    case 'exit_update':
      if (!replayed && data?.exits) {
        exits.set(data.exits as RoomExit[]);
      }
      break;
  }
}

function metadataToPlain(metadata: unknown): Record<string, unknown> | null {
  if (!metadata) return null;
  // protobuf-es v2: Struct messages have a toJson() method
  if (typeof (metadata as { toJson?: unknown }).toJson === 'function') {
    return (metadata as { toJson: () => Record<string, unknown> }).toJson();
  }
  // Fallback for plain objects (e.g., in tests)
  return metadata as Record<string, unknown>;
}

// Re-export replayActive so callers that previously imported it from here still work.
export { replayActive };
