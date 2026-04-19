// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect } from 'vitest';
import { get } from 'svelte/store';
import { connectionStatus, setConnectionStatus } from './connectionStore';

describe('connectionStore', () => {
  it('defaults to disconnected', () => {
    expect(get(connectionStatus)).toBe('disconnected');
  });

  it('transitions through all states', () => {
    setConnectionStatus('syncing');
    expect(get(connectionStatus)).toBe('syncing');
    setConnectionStatus('connected');
    expect(get(connectionStatus)).toBe('connected');
    setConnectionStatus('disconnected');
    expect(get(connectionStatus)).toBe('disconnected');
  });
});
