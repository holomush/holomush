<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Publish-vote web: live event delivery — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver live publish-vote tally updates to the web portal by treating `scene_publish_*` events as refetch triggers (never tally data), with the participant tally always read from the gated `GetPublishedScene` snapshot and observers seeing only a binary "vote in progress" indicator.

**Architecture:** A Svelte 5 `$state` publish store orchestrates refetches in response to `scene_publish_*` events; the events are wired in by extending `workspaceStore.ingestEvent` to dispatch them to the store (today they are dropped by `eventFrameToLogEntry`). No Go host, proto, or `plugin.yaml` change — only a thin TS client-wrapper layer, the store, the panel, plus one Go integration test and one Playwright spec. Grounded in ADR holomush-o8gx8 and the design spec `docs/superpowers/specs/2026-06-29-publish-vote-web-live-event-delivery-design.md`.

**Tech Stack:** TypeScript, Svelte 5 runes (`$state`/`$derived`/`$effect`), `@connectrpc/connect`, Vitest (`vi.doMock`), Playwright (multi-context), Go Ginkgo/Gomega + the `internal/testsupport/integrationtest` harness.

---

## File structure

| Path | Responsibility | Action |
| --- | --- | --- |
| `web/src/lib/scenes/client.ts` | Thin Connect client wrappers | Modify — add 4 publish wrappers |
| `web/src/lib/scenes/publishStore.svelte.ts` | Reactive publish state + event→refetch orchestrator | Create |
| `web/src/lib/scenes/publishStore.test.ts` | Vitest unit/boundary tests for the store | Create |
| `web/src/lib/scenes/workspaceStore.svelte.ts` | Scene-event ingestion chokepoint | Modify — `ingestEvent` dispatch |
| `web/src/lib/scenes/workspaceStore.test.ts` | Ingestion tests | Modify — wiring boundary test |
| `web/src/lib/components/scenes/ScenePublishPanel.svelte` | Publish panel (layout C) | Create |
| `web/src/lib/components/scenes/SceneContextRail.svelte` | Context rail host | Modify — mount the panel |
| `web/src/lib/components/scenes/ScenePublishPanel.svelte.test.ts` | Component render boundary tests | Create |
| `test/integration/scenes/publish_web_delivery_test.go` | Tier-2: live event reaches a web subscriber | Create |
| `web/e2e/scene-publish-vote.spec.ts` | Tier-3: participant+observer E2E | Create |

**Web test runner:** unit tests are Vitest — from `web/`, run `pnpm exec vitest run <file>` (npm script `test:unit` = `vitest run`). Go integration: `task test:int -- ./test/integration/scenes/`. E2E: `task test:e2e -- scene-publish-vote.spec.ts`.

**Grounded signatures (verified in the generated bindings):**

- `WebStartScenePublishRequest { sessionId, characterId, sceneId }` → `WebStartScenePublishResponse { publishedSceneId, attemptNumber }`
- `WebCastPublishSceneVoteRequest { sessionId, characterId, publishedSceneId, vote: boolean }`
- `WebWithdrawScenePublishRequest { sessionId, characterId, publishedSceneId }`
- `WebGetPublishedSceneRequest { sessionId, characterId, publishedSceneId }` → `WebGetPublishedSceneResponse { id, sceneId, attemptNumber, status, failureReason, voteSummary?: PublishedSceneVoteSummary }`
- `PublishedSceneVoteSummary { yes, no, pending }` (from `scene_pb`)
- `SceneInfo.activePublishAttemptId: string`, `SceneInfo.publishStatus: string` (`scene_pb.ts:158,167`)
- Client methods: `client.webStartScenePublish`, `client.webCastPublishSceneVote`, `client.webWithdrawScenePublish`, `client.webGetPublishedScene`

---

## Phase 1: Web client — store, wiring, wrappers (re-plans .39)

### Task 1: TypeScript publish client wrappers

**Files:**

- Modify: `web/src/lib/scenes/client.ts` (append wrappers; extend the `web_pb` type import list)
- Test: `web/src/lib/scenes/client.test.ts` (create if absent; otherwise append a `describe`)

- [ ] **Step 1: Write the failing test**

Create/append `web/src/lib/scenes/client.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock the Connect client singleton so wrappers call it without a transport.
const webStartScenePublish = vi.fn();
const webCastPublishSceneVote = vi.fn();
const webWithdrawScenePublish = vi.fn();
const webGetPublishedScene = vi.fn();

vi.mock('@connectrpc/connect', () => ({
	createClient: () => ({
		webStartScenePublish,
		webCastPublishSceneVote,
		webWithdrawScenePublish,
		webGetPublishedScene,
	}),
}));
vi.mock('$lib/transport', () => ({ transport: {} }));

import {
	startScenePublish,
	castPublishSceneVote,
	withdrawScenePublish,
	getPublishedScene,
} from './client';

beforeEach(() => vi.clearAllMocks());

describe('publish client wrappers', () => {
	it('startScenePublish sends sessionId/characterId/sceneId and returns the response', async () => {
		webStartScenePublish.mockResolvedValue({ publishedSceneId: 'att-1', attemptNumber: 1 });
		const res = await startScenePublish('S1', { characterId: 'C1', sceneId: 'SC1' });
		expect(webStartScenePublish).toHaveBeenCalledWith({ sessionId: 'S1', characterId: 'C1', sceneId: 'SC1' });
		expect(res.publishedSceneId).toBe('att-1');
	});

	it('castPublishSceneVote forwards the boolean vote', async () => {
		webCastPublishSceneVote.mockResolvedValue({});
		await castPublishSceneVote('S1', { characterId: 'C1', publishedSceneId: 'att-1', vote: true });
		expect(webCastPublishSceneVote).toHaveBeenCalledWith({
			sessionId: 'S1', characterId: 'C1', publishedSceneId: 'att-1', vote: true,
		});
	});

	it('withdrawScenePublish forwards the attempt id', async () => {
		webWithdrawScenePublish.mockResolvedValue({});
		await withdrawScenePublish('S1', { characterId: 'C1', publishedSceneId: 'att-1' });
		expect(webWithdrawScenePublish).toHaveBeenCalledWith({
			sessionId: 'S1', characterId: 'C1', publishedSceneId: 'att-1',
		});
	});

	it('getPublishedScene returns the full response (with voteSummary)', async () => {
		webGetPublishedScene.mockResolvedValue({
			id: 'att-1', sceneId: 'SC1', attemptNumber: 1, status: 'COLLECTING',
			failureReason: '', voteSummary: { yes: 2, no: 0, pending: 3 },
		});
		const res = await getPublishedScene('S1', { characterId: 'C1', publishedSceneId: 'att-1' });
		expect(res.status).toBe('COLLECTING');
		expect(res.voteSummary?.yes).toBe(2);
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && pnpm exec vitest run src/lib/scenes/client.test.ts`
Expected: FAIL — `startScenePublish is not exported` (functions not defined yet).

- [ ] **Step 3: Implement the wrappers**

In `web/src/lib/scenes/client.ts`, extend the `web_pb` type import block to add the four request types, then append the wrappers (mirror the existing `createScene`/`watchScene` `Pick<...>` style):

```ts
// add to the import { ... } from '$lib/connect/holomush/web/v1/web_pb' block:
	type WebStartScenePublishRequest,
	type WebCastPublishSceneVoteRequest,
	type WebWithdrawScenePublishRequest,
	type WebGetPublishedSceneRequest,
```

```ts
/**
 * Starts a publish vote on the given scene (structural write → typed RPC, not
 * the command path). Returns the new attempt id + number.
 */
export async function startScenePublish(
	sessionId: string,
	opts: Pick<WebStartScenePublishRequest, 'characterId' | 'sceneId'>,
) {
	return client.webStartScenePublish({ sessionId, ...opts });
}

/** Casts or changes the character's vote (true = yes) on an in-flight attempt. */
export async function castPublishSceneVote(
	sessionId: string,
	opts: Pick<WebCastPublishSceneVoteRequest, 'characterId' | 'publishedSceneId' | 'vote'>,
) {
	return client.webCastPublishSceneVote({ sessionId, ...opts });
}

/** Withdraws (cancels) an in-flight publish attempt the character owns. */
export async function withdrawScenePublish(
	sessionId: string,
	opts: Pick<WebWithdrawScenePublishRequest, 'characterId' | 'publishedSceneId'>,
) {
	return client.webWithdrawScenePublish({ sessionId, ...opts });
}

/**
 * Reads the participant-gated published-scene snapshot (status + aggregate
 * yes/no/pending tally). Throws ConnectError(PermissionDenied) for
 * non-participants — callers MUST treat that as observer mode, not an error.
 */
export async function getPublishedScene(
	sessionId: string,
	opts: Pick<WebGetPublishedSceneRequest, 'characterId' | 'publishedSceneId'>,
) {
	return client.webGetPublishedScene({ sessionId, ...opts });
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && pnpm exec vitest run src/lib/scenes/client.test.ts`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

Commit per `references/vcs-preamble.md` (jj): `jj commit -m "feat(scenes): web publish client wrappers (holomush-5rh.24.39)"`

---

### Task 2: Publish store — state shape + cold-start load

**Files:**

- Create: `web/src/lib/scenes/publishStore.svelte.ts`
- Test: `web/src/lib/scenes/publishStore.test.ts`

The store is a Svelte 5 module-level `$state` object (mirrors `workspaceStore.svelte.ts`). This task delivers the state shape and the cold-start loader; the event orchestrator lands in Task 3.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/scenes/publishStore.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest';

const getScene = vi.fn();
const getPublishedScene = vi.fn();
vi.mock('./client', () => ({ getScene, getPublishedScene }));
vi.mock('./altSessions.svelte', () => ({ ensureSession: vi.fn(async () => 'SESS') }));

import { publishStore } from './publishStore.svelte';

beforeEach(() => {
	vi.clearAllMocks();
	publishStore.reset();
});

describe('publishStore cold-start', () => {
	it('participant with active attempt loads the tally', async () => {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValue({
			id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 2, no: 0, pending: 3 },
		});
		await publishStore.loadColdStart('C1', 'SC1');
		expect(publishStore.voteInProgress).toBe(true);
		expect(publishStore.activeAttemptId).toBe('att-1');
		expect(publishStore.isParticipant).toBe(true);
		expect(publishStore.tally).toEqual({ yes: 2, no: 0, pending: 3 });
	});

	it('observer (PermissionDenied on tally) gets existence-only, no counts, no error', async () => {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockRejectedValue({ code: 'permission_denied' });
		await publishStore.loadColdStart('C1', 'SC1');
		expect(publishStore.voteInProgress).toBe(true);
		expect(publishStore.isParticipant).toBe(false);
		expect(publishStore.tally).toBeNull();
	});

	it('no active attempt → not in progress, no tally fetch', async () => {
		getScene.mockResolvedValue({ activePublishAttemptId: '', publishStatus: '' });
		await publishStore.loadColdStart('C1', 'SC1');
		expect(publishStore.voteInProgress).toBe(false);
		expect(getPublishedScene).not.toHaveBeenCalled();
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && pnpm exec vitest run src/lib/scenes/publishStore.test.ts`
Expected: FAIL — cannot find module `./publishStore.svelte`.

- [ ] **Step 3: Implement the store + cold-start**

Create `web/src/lib/scenes/publishStore.svelte.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ensureSession } from './altSessions.svelte';
import { getScene, getPublishedScene } from './client';

export type Tally = { yes: number; no: number; pending: number };

/** PermissionDenied means the caller is a non-participant observer. */
function isPermissionDenied(err: unknown): boolean {
	const code = (err as { code?: unknown })?.code;
	return code === 'permission_denied' || code === 7; // ConnectError string or numeric code
}

let activeAttemptId = $state('');
let phase = $state('');
let tally = $state<Tally | null>(null);
let isParticipant = $state(false);
let stale = $state(false);
let sceneId = $state('');
let characterId = $state('');

async function refetchTally(): Promise<void> {
	if (!activeAttemptId) {
		tally = null;
		return;
	}
	const sessionId = await ensureSession(characterId);
	try {
		const snap = await getPublishedScene(sessionId, { characterId, publishedSceneId: activeAttemptId });
		isParticipant = true;
		phase = snap.status;
		tally = snap.voteSummary
			? { yes: snap.voteSummary.yes, no: snap.voteSummary.no, pending: snap.voteSummary.pending }
			: { yes: 0, no: 0, pending: 0 };
		stale = false;
	} catch (err) {
		if (isPermissionDenied(err)) {
			isParticipant = false; // observer: existence only, never an error
			tally = null;
			return;
		}
		stale = true; // transient: keep last-known tally, retry on next event
	}
}

async function loadColdStart(charId: string, scnId: string): Promise<void> {
	characterId = charId;
	sceneId = scnId;
	const sessionId = await ensureSession(charId);
	const scene = await getScene(sessionId, charId, scnId);
	activeAttemptId = scene?.activePublishAttemptId ?? '';
	phase = scene?.publishStatus ?? '';
	if (!activeAttemptId) {
		isParticipant = false;
		tally = null;
		return;
	}
	await refetchTally();
}

function reset(): void {
	activeAttemptId = '';
	phase = '';
	tally = null;
	isParticipant = false;
	stale = false;
	sceneId = '';
	characterId = '';
}

export const publishStore = {
	get activeAttemptId() { return activeAttemptId; },
	get voteInProgress() { return activeAttemptId !== ''; },
	get phase() { return phase; },
	get tally() { return tally; },
	get isParticipant() { return isParticipant; },
	get stale() { return stale; },
	loadColdStart,
	reset,
	// internal, exported for Task 3 + tests:
	_refetchTally: refetchTally,
	_setActiveAttempt: (id: string) => { activeAttemptId = id; },
};
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && pnpm exec vitest run src/lib/scenes/publishStore.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

`jj commit -m "feat(scenes): publish store state + cold-start load (holomush-5rh.24.39)"`

---

### Task 3: Publish store — event→refetch orchestration (debounce + abort + observer)

**Files:**

- Modify: `web/src/lib/scenes/publishStore.svelte.ts` (add `onEvent`, debounce, abort, lifecycle vs vote_cast routing)
- Test: `web/src/lib/scenes/publishStore.test.ts` (append `describe('publishStore onEvent')`)

Event-type constants (from `plugins/core-scenes/plugin.yaml`, prefix `core-scenes:`): lifecycle = `scene_publish_started`, `scene_publish_resolved`, `scene_publish_withdrawn`, `scene_publish_cooloff_started`, `scene_publish_vote_attempts_extended`; tally-only = `scene_publish_vote_cast`.

- [ ] **Step 1: Write the failing test**

Append to `web/src/lib/scenes/publishStore.test.ts`:

```ts
import { afterEach } from 'vitest';

function makeEvent(type: string, scnId: string): { type: string; metadata: Record<string, unknown> } {
	return { type: `core-scenes:${type}`, metadata: { scene_id: scnId } };
}

describe('publishStore onEvent', () => {
	beforeEach(() => { vi.useFakeTimers(); });
	afterEach(() => { vi.useRealTimers(); });

	it('participant vote_cast triggers a debounced single tally refetch', async () => {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValue({ id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 1, no: 0, pending: 4 } });
		await publishStore.loadColdStart('C1', 'SC1');
		getPublishedScene.mockClear();
		// three rapid votes
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(getPublishedScene).toHaveBeenCalledTimes(1);
	});

	it('observer ignores vote_cast entirely (no fetch)', async () => {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockRejectedValue({ code: 'permission_denied' });
		await publishStore.loadColdStart('C1', 'SC1'); // becomes observer
		getPublishedScene.mockClear();
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(getPublishedScene).not.toHaveBeenCalled();
	});

	it('ignores events for a different scene_id', async () => {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValue({ id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 0, no: 0, pending: 5 } });
		await publishStore.loadColdStart('C1', 'SC1');
		getScene.mockClear(); getPublishedScene.mockClear();
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'OTHER') as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(getPublishedScene).not.toHaveBeenCalled();
	});

	it('lifecycle event refetches GetScene (pointer) and updates active attempt', async () => {
		getScene.mockResolvedValueOnce({ activePublishAttemptId: '', publishStatus: '' }); // cold-start: none
		await publishStore.loadColdStart('C1', 'SC1');
		expect(publishStore.voteInProgress).toBe(false);
		getScene.mockResolvedValueOnce({ activePublishAttemptId: 'att-9', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValue({ id: 'att-9', status: 'COLLECTING', voteSummary: { yes: 0, no: 0, pending: 5 } });
		publishStore.onEvent(makeEvent('scene_publish_started', 'SC1') as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(publishStore.activeAttemptId).toBe('att-9');
		expect(publishStore.voteInProgress).toBe(true);
	});

	it('participant→observer transition: losing participation mid-vote clears the tally', async () => {
		// Start as a participant with a visible tally.
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValueOnce({ id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 1, no: 0, pending: 4 } });
		await publishStore.loadColdStart('C1', 'SC1');
		expect(publishStore.isParticipant).toBe(true);
		expect(publishStore.tally).not.toBeNull();
		// The next refetch is denied (the character lost participation) → tally MUST clear.
		getPublishedScene.mockRejectedValueOnce({ code: 'permission_denied' });
		publishStore.onEvent(makeEvent('scene_publish_vote_cast', 'SC1') as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(publishStore.isParticipant).toBe(false);
		expect(publishStore.tally).toBeNull();
		expect(publishStore.voteInProgress).toBe(true); // still in progress (existence unchanged)
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && pnpm exec vitest run src/lib/scenes/publishStore.test.ts -t onEvent`
Expected: FAIL — `publishStore.onEvent is not a function`.

- [ ] **Step 3: Implement the orchestrator**

In `web/src/lib/scenes/publishStore.svelte.ts`, add the debounce/abort state and `onEvent`, and reload the pointer on lifecycle events. Insert above `loadColdStart`:

```ts
const LIFECYCLE = new Set([
	'core-scenes:scene_publish_started',
	'core-scenes:scene_publish_resolved',
	'core-scenes:scene_publish_withdrawn',
	'core-scenes:scene_publish_cooloff_started',
	'core-scenes:scene_publish_vote_attempts_extended',
]);
const VOTE_CAST = 'core-scenes:scene_publish_vote_cast';
const DEBOUNCE_MS = 300;

let debounceTimer: ReturnType<typeof setTimeout> | null = null;
let inFlight: AbortController | null = null;

function scheduleTallyRefetch(): void {
	if (debounceTimer) clearTimeout(debounceTimer);
	debounceTimer = setTimeout(() => {
		debounceTimer = null;
		inFlight?.abort();           // cancel any stale in-flight refetch
		inFlight = new AbortController();
		void refetchTally(inFlight.signal);
	}, DEBOUNCE_MS);
}

async function reloadPointer(): Promise<void> {
	const sessionId = await ensureSession(characterId);
	const scene = await getScene(sessionId, characterId, sceneId);
	activeAttemptId = scene?.activePublishAttemptId ?? '';
	phase = scene?.publishStatus ?? '';
	if (activeAttemptId) {
		scheduleTallyRefetch();
	} else {
		// Attempt is gone (resolved/withdrawn) — cancel any pending or in-flight
		// tally refetch so a late response cannot repopulate the tally.
		if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = null; }
		inFlight?.abort();
		inFlight = null;
		tally = null;
		isParticipant = false;
	}
}

function onEvent(ev: { type: string; metadata?: Record<string, unknown> }): void {
	const evScene = typeof ev.metadata?.['scene_id'] === 'string' ? ev.metadata['scene_id'] : '';
	if (evScene !== sceneId) return;                 // cross-scene isolation
	if (LIFECYCLE.has(ev.type)) { void reloadPointer(); return; }
	if (ev.type === VOTE_CAST) {
		if (!isParticipant) return;                  // observer: ignore vote_cast
		scheduleTallyRefetch();
	}
}
```

Change `refetchTally` to accept an optional `AbortSignal` and pass it to the RPC, and ignore an aborted result:

```ts
async function refetchTally(signal?: AbortSignal): Promise<void> {
	if (!activeAttemptId) { tally = null; return; }
	const sessionId = await ensureSession(characterId);
	try {
		const snap = await getPublishedScene(
			sessionId,
			{ characterId, publishedSceneId: activeAttemptId },
			signal,
		);
		if (signal?.aborted) return;                 // a newer refetch superseded us
		isParticipant = true;
		phase = snap.status;
		tally = snap.voteSummary
			? { yes: snap.voteSummary.yes, no: snap.voteSummary.no, pending: snap.voteSummary.pending }
			: { yes: 0, no: 0, pending: 0 };
		stale = false;
	} catch (err) {
		if (isPermissionDenied(err)) { isParticipant = false; tally = null; return; }
		if (!signal?.aborted) stale = true;
	}
}
```

Add `onEvent` to the exported `publishStore` object, and clear the timer in `reset()`:

```ts
// in reset():
	if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = null; }
	inFlight?.abort(); inFlight = null;
// in the exported object:
	onEvent,
```

Update the `getPublishedScene` wrapper from Task 1 to accept and forward an optional `signal` (Connect calls take `{ signal }` as the 2nd arg):

```ts
export async function getPublishedScene(
	sessionId: string,
	opts: Pick<WebGetPublishedSceneRequest, 'characterId' | 'publishedSceneId'>,
	signal?: AbortSignal,
) {
	return client.webGetPublishedScene({ sessionId, ...opts }, { signal });
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && pnpm exec vitest run src/lib/scenes/publishStore.test.ts`
Expected: PASS (cold-start 3 + onEvent 4 = 7 tests).

- [ ] **Step 5: Commit**

`jj commit -m "feat(scenes): publish store event-refetch orchestration with debounce+abort (holomush-5rh.24.39)"`

---

### Task 4: Wire `scene_publish_*` into the publish store via `ingestEvent`

**Files:**

- Modify: `web/src/lib/scenes/workspaceStore.svelte.ts` (`ingestEvent`, ~line 218)
- Test: `web/src/lib/scenes/workspaceStore.test.ts` (append a `describe`)

- [ ] **Step 1: Write the failing test**

Append to `web/src/lib/scenes/workspaceStore.test.ts`:

```ts
import { vi } from 'vitest';
const onEvent = vi.fn();
vi.mock('./publishStore.svelte', () => ({ publishStore: { onEvent } }));

describe('ingestEvent publish-event wiring', () => {
	it('dispatches scene_publish_* to the publish store AND keeps it out of the IC log', async () => {
		const ev = makeGameEvent('core-scenes:scene_publish_vote_cast', {
			eventId: 'PUB_EV_1', metadata: { scene_id: 'SCENE_P' },
		});
		const before = workspaceStore.logsBySceneId['SCENE_P']?.length ?? 0;
		workspaceStore.ingestEvent('SESSION_1', ev);
		expect(onEvent).toHaveBeenCalledTimes(1);
		const after = workspaceStore.logsBySceneId['SCENE_P']?.length ?? 0;
		expect(after).toBe(before); // not added to the IC log
	});

	it('does not dispatch a normal IC pose to the publish store', async () => {
		const ev = makeGameEvent('core-scenes:scene_pose', {
			eventId: 'POSE_EV_1', metadata: { scene_id: 'SCENE_P', text: 'nods.' },
		});
		onEvent.mockClear();
		workspaceStore.ingestEvent('SESSION_1', ev);
		expect(onEvent).not.toHaveBeenCalled();
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && pnpm exec vitest run src/lib/scenes/workspaceStore.test.ts -t "publish-event wiring"`
Expected: FAIL — `onEvent` never called (no dispatch yet).

- [ ] **Step 3: Implement the wiring**

In `web/src/lib/scenes/workspaceStore.svelte.ts`, add the import and dispatch at the top of `ingestEvent`, **before** the `if (!entry) return` early-return:

```ts
// near the other imports:
import { publishStore } from './publishStore.svelte';
```

```ts
function ingestEvent(_sessionId: string, ev: GameEvent): void {
	// Publish lifecycle/vote events are NOT IC log entries — fan them to the
	// publish store, then fall through (eventFrameToLogEntry returns null for
	// them, so the log path below is a no-op). scene_id rides ev.metadata,
	// stamped by translate.go's sceneIDFromSubject for all scene IC events.
	if (ev.type.startsWith('core-scenes:scene_publish_')) {
		publishStore.onEvent(ev as unknown as { type: string; metadata?: Record<string, unknown> });
	}

	const entry = eventFrameToLogEntry(ev);
	if (!entry) return;
	// ... existing routing unchanged ...
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && pnpm exec vitest run src/lib/scenes/workspaceStore.test.ts`
Expected: PASS (existing tests + 2 new).

- [ ] **Step 5: Commit**

`jj commit -m "feat(scenes): wire scene_publish_* events into the publish store via ingestEvent (holomush-5rh.24.39)"`

---

## Phase 2: Publish panel (re-plans .40)

### Task 5: Publish panel component (layout C) — participant vs observer

**Files:**

- Create: `web/src/lib/components/scenes/ScenePublishPanel.svelte`
- Modify: `web/src/lib/components/scenes/SceneContextRail.svelte` (mount the panel)
- Test: `web/src/lib/components/scenes/ScenePublishPanel.svelte.test.ts`

The panel renders off `publishStore`. **Participant** sees the tally + phase + affordances; **observer** sees only a "publication vote in progress" badge (no counts, no phase). Tests use the repo's native Svelte 5 component-test pattern — `mount`/`unmount`/`flushSync` from `'svelte'`, asserting against the mount target's DOM (see `web/src/lib/components/scenes/CharacterMultiSelect.svelte.test.ts`). The repo does **not** use `@testing-library/svelte`, and component tests are named `*.svelte.test.ts`.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/components/scenes/ScenePublishPanel.svelte.test.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount, unmount, flushSync } from 'svelte';
import ScenePublishPanel from './ScenePublishPanel.svelte';

// Drive the panel by mocking the store getters.
let state: Record<string, unknown>;
vi.mock('$lib/scenes/publishStore.svelte', () => ({
	publishStore: new Proxy({}, { get: (_t, k) => state[k as string] }),
}));

function renderPanel() {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const comp = mount(ScenePublishPanel, { target });
	flushSync();
	return { target, comp };
}

beforeEach(() => { state = { voteInProgress: false, isParticipant: false, phase: '', tally: null }; });

describe('ScenePublishPanel', () => {
	it('renders nothing when no vote is in progress', () => {
		state = { voteInProgress: false, isParticipant: false, phase: '', tally: null };
		const { target, comp } = renderPanel();
		expect(target.textContent?.trim()).toBe('');
		unmount(comp); target.remove();
	});

	it('observer sees only the in-progress badge, NO counts', () => {
		state = { voteInProgress: true, isParticipant: false, phase: 'COLLECTING', tally: null };
		const { target, comp } = renderPanel();
		expect(target.textContent).toMatch(/publication vote in progress/i);
		expect(target.textContent).not.toMatch(/\d+\s*(yes|no|pending)/i); // no counts
		expect(target.textContent).not.toMatch(/COLLECTING/);              // no phase
		unmount(comp); target.remove();
	});

	it('participant sees the yes/no/pending tally and phase', () => {
		state = { voteInProgress: true, isParticipant: true, phase: 'COLLECTING', tally: { yes: 2, no: 1, pending: 3 } };
		const { target, comp } = renderPanel();
		expect(target.textContent).toMatch(/2/);
		expect(target.textContent).toMatch(/COLLECTING/);
		unmount(comp); target.remove();
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && pnpm exec vitest run src/lib/components/scenes/ScenePublishPanel.svelte.test.ts`
Expected: FAIL — cannot find `./ScenePublishPanel.svelte`.

- [ ] **Step 3: Implement the panel**

Create `web/src/lib/components/scenes/ScenePublishPanel.svelte`:

```svelte
<!-- SPDX-License-Identifier: Apache-2.0 -->
<script lang="ts">
	import { publishStore } from '$lib/scenes/publishStore.svelte';
</script>

{#if publishStore.voteInProgress}
	{#if publishStore.isParticipant && publishStore.tally}
		<section class="publish-panel" aria-label="Publication vote">
			<header>Publication vote — {publishStore.phase}</header>
			<ul class="tally">
				<li>Yes <strong>{publishStore.tally.yes}</strong></li>
				<li>No <strong>{publishStore.tally.no}</strong></li>
				<li>Pending <strong>{publishStore.tally.pending}</strong></li>
			</ul>
		</section>
	{:else}
		<section class="publish-panel" aria-label="Publication vote">
			<span class="badge">A publication vote is in progress</span>
		</section>
	{/if}
{/if}
```

> Affordances: this panel renders **only while a vote is in progress** (`{#if publishStore.voteInProgress}`), so its participant controls are **vote / withdraw only** — gate them on `publishStore.phase === 'COLLECTING'`, wired to the `publishFlow` cast/withdraw actions (button pattern per `SceneSettingsForm.svelte`). **Starting** a publish vote is a *pre-vote* action (there is no active attempt yet), so the start-publish control lives with the other scene-lifecycle actions (end / pause / resume / publish), **not** in this panel — a start button placed inside the `voteInProgress` branch could never render.

Mount it in `SceneContextRail.svelte` near the other rail sections:

```svelte
<script lang="ts">
	import ScenePublishPanel from './ScenePublishPanel.svelte';
	// ...existing imports
</script>
<!-- inside the rail layout, in the layout-C slot: -->
<ScenePublishPanel />
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && pnpm exec vitest run src/lib/components/scenes/ScenePublishPanel.svelte.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

`jj commit -m "feat(scenes): publish panel (layout C) participant/observer views (holomush-5rh.24.40)"`

---

## Phase 3: Integration coverage (new task)

### Task 6: Tier-2 Go integration — live event reaches a web subscriber

**Files:**

- Create: `test/integration/scenes/publish_web_delivery_test.go`

The participant/non-participant tally gate is already covered by `publish_e2e_test.go` ("Charlie non-participant → PermissionDenied"). This task adds the one missing assertion: a `scene_publish_vote_cast` event arrives on a focused participant's stream as a frame carrying its type + scene id (the frames the `ingestEvent` wiring consumes). Uses `Session.WaitForEvent` (grounded at `internal/testsupport/integrationtest/harness_smoke_test.go:147` and `test/integration/scenes/real_scene_join_subscription_test.go:104`) and the command-drive sequence from `publish_e2e_test.go:70-91`.

- [ ] **Step 1: Write the failing test**

Create `test/integration/scenes/publish_web_delivery_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// Verifies: INV-SCENE-62
//
// The web live-tally update path depends on scene_publish_* events arriving on
// a participant's event stream so the client (workspaceStore.ingestEvent →
// publishStore) can refetch. This pins that the vote-cast notice is actually
// delivered as an event frame carrying its type, role-agnostically to
// FocusMembership holders, with the scene id on the subject (the routing key
// translate.go stamps into GameEvent.metadata.scene_id for the web client).
var _ = Describe("scene_publish_vote_cast is delivered to a focused scene subscriber", func() {
	var (
		ts         *integrationtest.Server
		ctx        context.Context
		alice, bob *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 120*time.Second)
		DeferCleanup(cancel)
		ts = integrationtest.Start(suiteT, integrationtest.WithInTreePlugins())
		alice = ts.ConnectAuthed(ctx, "Alice")
		bob = ts.ConnectAuthed(ctx, "Bob")
	})

	AfterEach(func() {
		if bob != nil {
			bob.Logout(ctx)
		}
		if alice != nil {
			alice.Logout(ctx)
		}
		ts.Stop()
	})

	It("delivers the vote_cast frame (type + scene subject) to a joined participant", func() {
		// CreateScene takes a ulid.ULID and returns the new scene's ulid.ULID
		// (internal/testsupport/integrationtest/session.go:517). The command path
		// takes the stringified id.
		sceneID := alice.CreateScene(ctx, alice.LocationID)
		sceneRef := sceneID.String()

		Expect(alice.SendCommand(ctx, "scene invite "+sceneRef+" "+bob.CharacterID.String())).To(Succeed())
		Expect(bob.SendCommand(ctx, "scene join "+sceneRef)).To(Succeed())
		Expect(alice.SendCommand(ctx, "scene end "+sceneRef)).To(Succeed())
		Expect(alice.SendCommand(ctx, "scene publish #"+sceneRef)).To(Succeed())
		Expect(alice.SendCommand(ctx, "scene publish vote yes #"+sceneRef)).To(Succeed())

		// Bob (joined participant) MUST receive the vote_cast notice as an event
		// frame on his scene stream — this is the frame the web client's
		// ingestEvent wiring consumes.
		frame := bob.WaitForEvent(ctx, "core-scenes:scene_publish_vote_cast")
		Expect(frame).NotTo(BeNil(), "bob must receive the vote_cast frame")
		Expect(frame.GetType()).To(Equal("core-scenes:scene_publish_vote_cast"))
		// The subject carries the scene id (translate.go's sceneIDFromSubject
		// extracts it into GameEvent.metadata.scene_id for the web client).
		Expect(frame.GetStream()).To(ContainSubstring(sceneID.String()))
	})
})
```

> If `alice.LocationID` / `alice.CreateScene` / `Session.SendCommand` / `Session.WaitForEvent` differ from the grounded call sites, match the exact signatures in `publish_e2e_test.go:66-91` and `harness_smoke_test.go:141-147` — those are the canonical exemplars.

- [ ] **Step 2: Run the test to verify it passes**

Run: `task test:int -- ./test/integration/scenes/ -ginkgo.focus "focused scene subscriber"`
Expected: first the file must compile and the spec run; PASS once delivery is confirmed. (Requires Docker; `task test:int` builds plugin artifacts first.)

- [ ] **Step 3: Confirm the invariant binding**

`bob.WaitForEvent` proving the vote_cast frame reaches a non-emitting FocusMembership holder genuinely asserts the INV-SCENE-62 fan-out for this event type — keep the `// Verifies: INV-SCENE-62` annotation. If the assembled assertion ends up weaker, remove the annotation rather than fabricate a binding.

- [ ] **Step 4: Commit**

`jj commit -m "test(scenes): integration — scene_publish_vote_cast delivered to web subscriber (holomush-5rh.24)"`

---

## Phase 4: Telnet-free E2E (re-plans .38)

### Task 7: Tier-3 Playwright E2E — participant + observer

**Files:**

- Create: `web/e2e/scene-publish-vote.spec.ts`

Telnet-free, two browser contexts (the `{ browser }` multi-context pattern in `web/e2e/multi-tab-session.spec.ts`). Use the auth + scene fixtures in `web/e2e/helpers/fixtures.ts` and `web/e2e/helpers/db.ts`.

- [ ] **Step 1: Write the spec**

Create `web/e2e/scene-publish-vote.spec.ts`:

```ts
import { test, expect } from '@playwright/test';
// Reuse the existing auth/scene helpers — match the imports used by
// web/e2e/character-switcher.spec.ts / multi-tab-session.spec.ts.

test('participant sees live tally; observer sees only "in progress"', async ({ browser }) => {
	// 1. Set up a scene with a participant (Alice) and an observer (Bob) via the
	//    e2e DB/auth helpers (helpers/db.ts seedScene + helpers/fixtures.ts login).
	// 2. Alice (participant context) opens the scene, starts a publication vote.
	// 3. Observer context (Bob, watch-only) asserts the panel shows
	//    "A publication vote is in progress" and NO numeric counts:
	//      await expect(observerPanel.getByText(/publication vote in progress/i)).toBeVisible();
	//      await expect(observerPanel).not.toHaveText(/\d+\s*(yes|no|pending)/i);
	// 4. Alice casts a vote; assert the participant panel tally updates live
	//    (Pending decrements / Yes increments) WITHOUT a manual reload.
	// 5. Resolve the vote; assert each role sees the role-appropriate end state.
});

test('participant reload mid-vote resyncs the tally from cold-start', async ({ browser }) => {
	// Start a vote, cast some votes, reload the participant page, and assert the
	// panel re-renders the correct current tally (cold-start GetScene +
	// GetPublishedScene), proving missed-event recovery.
});
```

- [ ] **Step 2: Fill in the helpers**

Read `web/e2e/helpers/fixtures.ts`, `web/e2e/helpers/db.ts`, and `web/e2e/multi-tab-session.spec.ts` for the exact login/seed/locator helpers, and replace the comment scaffolds with real Playwright steps. Add a stable `data-testid` (e.g. `scene-publish-panel`) to `ScenePublishPanel.svelte` (Task 5) so the spec can scope assertions to the panel.

- [ ] **Step 3: Run the spec to verify it passes**

Run: `task test:e2e -- scene-publish-vote.spec.ts`
Expected: PASS. (Runs the full Docker stack via `compose.e2e.yaml`.)

- [ ] **Step 4: Commit**

`jj commit -m "test(scenes): telnet-free E2E for web publish vote (holomush-5rh.24.38)"`

---

## Verification (whole feature)

- [ ] `cd web && pnpm exec vitest run src/lib/scenes/ src/lib/components/scenes/ScenePublishPanel.svelte.test.ts` — all unit/component green.
- [ ] `task test:int -- ./test/integration/scenes/` — Tier-2 green (Docker).
- [ ] `task test:e2e -- scene-publish-vote.spec.ts` — Tier-3 green (Docker).
- [ ] `task pr-prep` fast lane green (lint/fmt/build/unit); run `task pr-prep:full` since the diff touches int+E2E surface.
- [ ] No `translate.go` / proto / `plugin.yaml` changes in the diff (Approach B invariant).

## Notes for the implementer

- **Component tests** (Task 5) use native Svelte 5 `mount`/`unmount`/`flushSync` from `'svelte'` (per `CharacterMultiSelect.svelte.test.ts`); the repo does NOT use `@testing-library/svelte`.
- **Connect `signal` arg** (Task 3): `@connectrpc/connect` unary calls accept `{ signal }` as the second argument; verify against an existing call site if the option name differs in this version.
- **Task 7 (E2E) is a fixture-referencing scaffold deliberately** — it requires reading `web/e2e/helpers/{fixtures,db}.ts` + `multi-tab-session.spec.ts` to assemble the UI flow, which is less error-prone than guessing selectors here. The scaffold MUST be filled (not committed green) before the bead closes — Step 3 "run and verify PASS" enforces it.
<!-- adr-capture: sha256=70ce37c174f82da9; session=cli; ts=2026-06-30T12:08:46Z; adrs=holomush-uqrr7 -->
