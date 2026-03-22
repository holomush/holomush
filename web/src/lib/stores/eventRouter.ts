// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { exits } from './sidebarStore';
import type { RoomExit } from './sidebarStore';
import { appendLine, replayActive } from './terminalStore';
import { applyLocationState, addPresence, removePresence } from './sidebarStore';
import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';

// EventChannel values from the generated proto
const CHANNEL_UNSPECIFIED = 0;
const CHANNEL_TERMINAL = 1;
const CHANNEL_STATE = 2;
const CHANNEL_BOTH = 3;

export function routeEvent(event: GameEvent, replayed: boolean) {
  const channel = event.channel ?? CHANNEL_UNSPECIFIED;

  // Route to terminal (scrollback)
  if (channel === CHANNEL_TERMINAL || channel === CHANNEL_BOTH || channel === CHANNEL_UNSPECIFIED) {
    appendLine(event, replayed);
  }

  // Route to sidebar stores. location_state is always applied (it's the
  // authoritative snapshot, including the synthetic one at stream start).
  // arrive/leave deltas are suppressed during replay to avoid applying
  // stale history on top of the snapshot.
  if (channel === CHANNEL_STATE || channel === CHANNEL_BOTH) {
    routeToSidebar(event, replayed);
  }
}

function routeToSidebar(event: GameEvent, replayed: boolean) {
  const data = metadataToPlain(event.metadata);

  switch (event.type) {
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
    case 'arrive':
      if (!replayed && event.characterName) addPresence(event.characterName);
      break;
    case 'leave':
      if (!replayed && event.characterName) removePresence(event.characterName);
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
