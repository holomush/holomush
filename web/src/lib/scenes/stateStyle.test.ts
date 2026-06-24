// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect } from 'vitest';

import { sceneStateDotClass } from './stateStyle';

describe('sceneStateDotClass', () => {
	it('maps active to the green emerald indicator', () => {
		expect(sceneStateDotClass('active')).toBe('bg-emerald-500');
	});

	it('maps paused to amber', () => {
		expect(sceneStateDotClass('paused')).toBe('bg-amber-400');
	});

	it('maps ended to muted-foreground', () => {
		expect(sceneStateDotClass('ended')).toBe('bg-muted-foreground');
	});

	it('maps published to blue', () => {
		expect(sceneStateDotClass('published')).toBe('bg-blue-400');
	});

	it('falls back to muted-foreground for an unknown state', () => {
		expect(sceneStateDotClass('totally-unknown')).toBe('bg-muted-foreground');
	});

	it('falls back to muted-foreground for an empty state', () => {
		expect(sceneStateDotClass('')).toBe('bg-muted-foreground');
	});
});
