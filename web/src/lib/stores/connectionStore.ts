// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable } from 'svelte/store';

export type ConnectionStatus = 'connected' | 'syncing' | 'disconnected';

export const connectionStatus = writable<ConnectionStatus>('disconnected');

export function setConnectionStatus(status: ConnectionStatus) {
  connectionStatus.set(status);
}
