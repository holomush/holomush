import { exits } from './sidebarStore';
import type { RoomExit } from './sidebarStore';
import { appendLine, replayActive, markReplayComplete } from './terminalStore';
import { applyLocationState, addPresence, removePresence } from './sidebarStore';

// EventChannel values from the generated proto
const CHANNEL_UNSPECIFIED = 0;
const CHANNEL_TERMINAL = 1;
const CHANNEL_STATE = 2;
const CHANNEL_BOTH = 3;

interface EventResponse {
  event?: {
    type: string;
    characterName: string;
    text: string;
    channel?: number;
    metadata?: unknown;
  };
  replayed: boolean;
  replayComplete: boolean;
}

export function routeEvent(response: EventResponse) {
  const event = response.event;
  if (!event) return;


  if (response.replayComplete) {
    markReplayComplete();
    return;
  }

  if (response.replayed) {
    replayActive.set(true);
  }

  const channel = event.channel ?? CHANNEL_UNSPECIFIED;

  // Route to terminal (scrollback)
  if (channel === CHANNEL_TERMINAL || channel === CHANNEL_BOTH || channel === CHANNEL_UNSPECIFIED) {
    appendLine(event, response.replayed);
  }

  // Route to sidebar stores
  if (channel === CHANNEL_STATE || channel === CHANNEL_BOTH) {
    routeToSidebar(event);
  }
}

function routeToSidebar(event: { type: string; characterName: string; metadata?: unknown }) {
  const data = metadataToPlain(event.metadata);

  switch (event.type) {
    case 'location_state':
      if (data) {
        applyLocationState(data);
      } else {
        console.warn('[eventRouter] location_state event received with unparseable metadata');
      }
      break;
    case 'exit_update':
      if (data?.exits) {
        exits.set(data.exits as RoomExit[]);
      }
      break;
    case 'arrive':
      if (event.characterName) addPresence(event.characterName);
      break;
    case 'leave':
      if (event.characterName) removePresence(event.characterName);
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
