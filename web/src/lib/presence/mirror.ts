// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import type { PresenceStore } from './store';

/**
 * Minimal shape of a movement-event-like object accepted by
 * `mirrorMovementPresence`. The terminal passes a `GameEvent` here, but the
 * helper only reads four fields — defining a narrow interface keeps the
 * function easy to test with plain objects.
 */
export interface MovementEvent {
  category?: string;
  type?: string;
  actorId?: string;
  actor?: string; // display name (corev1.EventFrame payload.character_name)
}

/**
 * The legacy sidebarStore surface — name-keyed presence. Optional so the
 * helper is usable in isolation tests and in environments where the legacy
 * store has been retired (T12, holomush-5b2j.14).
 */
export interface LegacyPresenceStore {
  add(name: string): void;
  remove(name: string): void;
}

/**
 * Routes a movement event to both presence stores:
 *
 * - The NEW `PresenceStore` (keyed by character ULID via `actorId`) is the
 *   authoritative future state.
 * - The OLD legacy store (keyed by display name via `actor`) is what the
 *   terminal sidebar still binds to. Kept in sync until T12
 *   (`holomush-5b2j.14`) migrates the UI binding and removes the `legacy`
 *   parameter.
 *
 * Non-movement events are a no-op. Both writes are idempotent on their
 * respective stores (idempotent set / no-op delete-of-missing).
 *
 * Empty `actorId` skips the new store (system / actor-less events).
 * Empty `actor` (name) skips the legacy store.
 */
export function mirrorMovementPresence(
  ev: MovementEvent,
  presence: PresenceStore,
  legacy?: LegacyPresenceStore,
): void {
  if (ev.category !== 'movement') return;
  const actorId = ev.actorId ?? '';
  const actorName = ev.actor ?? '';
  if (ev.type === 'arrive') {
    if (actorId) {
      presence.upsert({ characterId: actorId, name: actorName, state: 'ACTIVE' });
    }
    if (actorName && legacy) {
      legacy.add(actorName);
    }
  } else if (ev.type === 'leave') {
    if (actorId) {
      presence.remove(actorId);
    }
    if (actorName && legacy) {
      legacy.remove(actorName);
    }
  }
}
