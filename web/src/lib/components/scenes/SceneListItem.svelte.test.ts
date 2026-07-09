// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount } from 'svelte';
import defaultLight from '$lib/theme/default-light.json';
import defaultDark from '$lib/theme/default-dark.json';
import warmDark from '$lib/theme/warm-dark.json';
import type { WorkspaceScene } from '$lib/scenes/types';

// SceneListItem derives `isActive` from workspaceStore.selectedSceneId. The real
// store exposes selectedSceneId only via a getter (mutated by an async,
// network-bound select()), so we mock the module and drive selection directly.
const store = vi.hoisted(() => ({
	selectedSceneId: null as string | null,
	unreadBySceneId: {} as Record<string, number>,
}));

vi.mock('$lib/scenes/workspaceStore.svelte', () => ({
	workspaceStore: {
		get selectedSceneId() {
			return store.selectedSceneId;
		},
		get unreadBySceneId() {
			return store.unreadBySceneId;
		},
		select: vi.fn(),
	},
}));

import SceneListItem from './SceneListItem.svelte';

const ACTIVE_ID = 'scene-active';

function makeScene(overrides: Partial<WorkspaceScene> = {}): WorkspaceScene {
	return {
		sceneId: ACTIVE_ID,
		title: 'Moonlit Terrace',
		locationId: 'loc-1',
		state: 'active',
		tags: [],
		role: 'owner',
		ownerId: 'char-1',
		asCharacterId: 'char-1',
		asCharacterName: 'Alice',
		lastActivityMs: 0n,
		entryCount: 0n,
		unread: 0,
		muted: false,
		...overrides,
	};
}

function render(scene: WorkspaceScene): HTMLElement {
	const target = document.createElement('div');
	document.body.appendChild(target);
	mount(SceneListItem, { target, props: { scene } });
	return target;
}

// The "as <character>" subtext line. `text-[12px]` (the chrome/status base size
// per web/CLAUDE.md) is its stable identifying class (unconditional, unique in
// the row — the title is text-sm, the badge text-[10px]), so this query survives
// the active/inactive color swap and any unrelated markup change. Returns null
// when the subtext is not rendered.
function subtextLine(target: HTMLElement): HTMLSpanElement | null {
	return target.querySelector<HTMLSpanElement>('span[class*="text-[12px]"]');
}

afterEach(() => {
	document.body.replaceChildren();
	store.selectedSceneId = null;
	store.unreadBySceneId = {};
});

// WCAG 2.1 relative-luminance contrast ratio between two #rrggbb colors.
function contrastRatio(fg: string, bg: string): number {
	const lin = (v: number) => {
		const c = v / 255;
		return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
	};
	const lum = (hex: string) => {
		const h = hex.replace('#', '');
		const [r, g, b] = [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
		return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
	};
	const [hi, lo] = [lum(fg), lum(bg)].sort((x, y) => y - x);
	return (hi + 0.05) / (lo + 0.05);
}

describe('SceneListItem selected-row subtext legibility', () => {
	it('renders the as-character subtext in accent-foreground (not muted-foreground) when the row is active', () => {
		store.selectedSceneId = ACTIVE_ID;
		const subtext = subtextLine(render(makeScene()))!;

		expect(subtext.textContent?.trim()).toBe('as Alice');
		// muted-foreground is calibrated for the background surface; on the
		// selected row's bg-accent it is ~1:1 (illegible). The active subtext
		// MUST derive from accent-foreground instead.
		expect(subtext.className).not.toContain('text-muted-foreground');
		expect(subtext.className).toContain('text-accent-foreground');
	});

	it('keeps muted-foreground for the subtext when the row is inactive', () => {
		store.selectedSceneId = 'some-other-scene';
		const subtext = subtextLine(render(makeScene()))!;

		// On the non-accent (background) surface, muted-foreground is the correct,
		// legible token — the fix must not regress the inactive row.
		expect(subtext.className).toContain('text-muted-foreground');
		expect(subtext.className).not.toContain('text-accent-foreground');
	});

	it('renders no subtext line when the scene has no as-character name', () => {
		store.selectedSceneId = ACTIVE_ID;
		const target = render(makeScene({ asCharacterName: '' }));

		expect(subtextLine(target)).toBeNull();
	});

	// The active subtext renders text-accent-foreground on the bg-accent surface,
	// so its legibility is exactly the accent / accent-foreground token contrast.
	// Pin it at >= WCAG AA (4.5:1, normal text) for every theme whose palette
	// supports it. warm-light is intentionally excluded: its accent (#f57c00) +
	// accent-foreground (#ffffff) is 2.70:1 — a sub-AA palette pairing that fails
	// app-wide for ANY text on accent (titles, buttons), not just this component.
	// Tracked as holomush-wrwqu; add it here once that palette fix lands.
	it.each([
		['default-light', defaultLight],
		['default-dark', defaultDark],
		['warm-dark', warmDark],
	])('%s accent / accent-foreground pairing is legible (WCAG AA) on the selected surface', (_name, theme) => {
		const c = theme.colors as Record<string, string>;
		expect(contrastRatio(c['accent.foreground'], c['accent'])).toBeGreaterThanOrEqual(4.5);
	});
});
