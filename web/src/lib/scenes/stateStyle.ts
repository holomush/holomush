// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * Single source of truth for a scene's status-dot color, shared by the scenes
 * center-header title bar and the SceneContextRail so the two indicators never
 * drift (holomush-5rh.27): an `active` scene reads green in both. Returns a
 * Tailwind background-color class; unknown states fall back to muted.
 */
const stateDotClass: Record<string, string> = {
	active: 'bg-emerald-500',
	paused: 'bg-amber-400',
	ended: 'bg-muted-foreground',
	published: 'bg-blue-400',
};

export function sceneStateDotClass(state: string): string {
	return stateDotClass[state] ?? 'bg-muted-foreground';
}
