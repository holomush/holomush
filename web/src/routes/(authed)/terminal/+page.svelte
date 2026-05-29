<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { ControlSignal } from '$lib/connect/holomush/web/v1/web_pb';
  import { routeEvent } from '$lib/stores/eventRouter';
  import { appendLine, clearLines, replayActive } from '$lib/stores/terminalStore';
  import { presenceStore as presence, type PresenceState } from '$lib/presence/store';
  import { mirrorMovementPresence as mirrorMovementPresenceFn } from '$lib/presence/mirror';
  import { WebPresenceState, type WebPresenceEntry } from '$lib/connect/holomush/web/v1/web_pb';
  import { themePreferences, terminalBlackOverrideVars } from '$lib/stores/themeStore';
  import { setConnectionStatus } from '$lib/stores/connectionStore';
  import { uiPrefs, setSidebarWidthPx } from '$lib/stores/uiPrefsStore';
  import {
    composerDraft,
    setComposerDraft,
    registerComposerSubmit,
  } from '$lib/stores/composerBridge';
  import { pushCommand } from '$lib/stores/commandHistoryStore';
  import * as Resizable from '$lib/components/ui/resizable';
  import { authState, clearAuth, clearCharacterSession } from '$lib/stores/authStore';
  import { get } from 'svelte/store';
  import TerminalView from '$lib/components/terminal/TerminalView.svelte';
  import CommandInput from '$lib/components/terminal/CommandInput.svelte';
  import Rail from '$lib/components/terminal/Rail.svelte';
  import Sidebar from '$lib/components/sidebar/Sidebar.svelte';
  import { goto } from '$app/navigation';
  import { trace, type Span } from '@opentelemetry/api';
  import { commandRoundtripAttributes } from '$lib/commandSpan';
  import { backfillStreams } from '$lib/backfill/streamBackfill';
  import { isUnimplementedError } from '$lib/connect/errors';
  import { isStaleSession } from '$lib/util/stale';
  import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';

  const client = createClient(WebService, transport);

  function presenceStateFromProto(s: WebPresenceState): PresenceState {
    switch (s) {
      case WebPresenceState.ACTIVE: return 'ACTIVE';
      case WebPresenceState.DETACHED: return 'DETACHED';
      case WebPresenceState.INACTIVE: return 'INACTIVE';
      default: return 'UNSPECIFIED';
    }
  }

  // Bind the testable mirrorMovementPresence helper to the singleton presence
  // store. Legacy dual-write removed: PresenceList.svelte now reads from
  // presenceStore directly (T12, holomush-5b2j.14).
  function mirrorMovementPresence(ev: GameEvent) {
    mirrorMovementPresenceFn(ev, presence);
  }

  async function handleStaleSession() {
    // Abort any in-flight stream/backfill before redirecting so post-logout
    // async work does not mutate connected/sessionId/error after navigation.
    abortController?.abort();
    setConnectionStatus('disconnected');
    connected = false;
    sessionId = '';
    clearCharacterSession();
    clearAuth();
    await goto('/');
  }

  const tracer = trace.getTracer('holomush-web');
  let pendingCommandSpan: Span | null = null;
  let streamSpan: Span | null = null;
  let streamGeneration = 0;
  let streamReadyGate:
    | {
        generation: number;
        promise: Promise<void>;
        resolve: () => void;
        reject: (reason?: unknown) => void;
      }
    | null = null;

  let sessionId = $state('');
  // connectionId is set when the StreamEvents subscription opens via a
  // STREAM_OPENED ControlFrame. Passed back to SendCommand so the
  // gateway can route per-connection commands (Phase 5 scene-focus
  // autofocus) to THIS tab's stream rather than racing with other tabs.
  let connectionId = $state('');
  let connected = $state(false);
  let error = $state('');
  let abortController: AbortController | null = null;

  let injectText = $state<string | undefined>(undefined);
  function handleInject(cmd: string) { injectText = cmd; }
  function handleInjectConsumed() { injectText = undefined; }

  let paneGroupEl: HTMLDivElement | undefined = $state(undefined);
  let containerWidth = $state(0);

  function pctFromPx(px: number, cw: number): number {
    if (cw <= 0) return 25;
    // px is already clamped by uiPrefsStore; just divide
    return (px / cw) * 100;
  }

  let widthCommitTimer: ReturnType<typeof setTimeout> | undefined;
  function handleSidebarResize(pct: number) {
    clearTimeout(widthCommitTimer);
    widthCommitTimer = setTimeout(() => {
      if (containerWidth > 0) setSidebarWidthPx(Math.round((pct / 100) * containerWidth));
    }, 200);
  }

  $effect(() => {
    if (!paneGroupEl) return;
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) containerWidth = e.contentRect.width;
    });
    ro.observe(paneGroupEl);
    return () => ro.disconnect();
  });

  let sidebarDefaultPct = $derived(pctFromPx($uiPrefs.sidebarWidthPx, containerWidth || 1120));

  // Composer draft bridge: on open, seed composer from the saved CommandInput
  // draft for this session; on close, inject the (possibly edited) text back
  // into CommandInput via injectText.
  let wasComposerOpen = $state(false);
  $effect(() => {
    const isOpen = $uiPrefs.composerOpen;
    if (isOpen && !wasComposerOpen) {
      if (sessionId) {
        const saved = localStorage.getItem(`holomush-draft:${sessionId}`) ?? '';
        setComposerDraft(saved);
      }
    } else if (!isOpen && wasComposerOpen) {
      injectText = $composerDraft;
    }
    wasComposerOpen = isOpen;
  });

  // Persist composer draft to the SAME localStorage key that CommandInput uses,
  // so composer edits survive reload even before composer closes.
  let composerPersistTimer: ReturnType<typeof setTimeout> | undefined;
  $effect(() => {
    const draft = $composerDraft;
    const sid = sessionId;
    if (!sid || !$uiPrefs.composerOpen) return;
    clearTimeout(composerPersistTimer);
    composerPersistTimer = setTimeout(() => {
      try {
        if (draft) {
          localStorage.setItem(`holomush-draft:${sid}`, draft);
        } else {
          localStorage.removeItem(`holomush-draft:${sid}`);
        }
      } catch { /* quota / privacy mode — best effort */ }
    }, 500);
  });

  onDestroy(() => clearTimeout(composerPersistTimer));

  onMount(() => {
    registerComposerSubmit((cmd) => {
      pushCommand(cmd);
      sendCommand(cmd);
    });

    const sid = $authState.sessionId;

    if (sid) {
      sessionId = sid;
      connected = true;
      hydrateAndStream();
    } else {
      // Auth guard should have prevented this, but redirect as fallback
      goto('/login');
    }
  });

  onDestroy(() => {
    registerComposerSubmit(null);
    abortController?.abort();
    // Best-effort server disconnect on component unmount (SPA navigation)
    if (sessionId) {
      client.disconnect({ sessionId }).catch(() => {});
    }
    pendingCommandSpan?.end();
    pendingCommandSpan = null;
    streamSpan?.end();
    streamSpan = null;
  });

  function createStreamReadyGate(generation: number) {
    let resolve!: () => void;
    let reject!: (reason?: unknown) => void;
    const promise = new Promise<void>((innerResolve, innerReject) => {
      resolve = innerResolve;
      reject = innerReject;
    });
    streamReadyGate = { generation, promise, resolve, reject };
  }

  function resolveStreamReady(generation: number) {
    if (streamReadyGate?.generation !== generation) return;
    streamReadyGate.resolve();
    streamReadyGate = null;
  }

  function rejectStreamReady(generation: number, reason: unknown) {
    if (streamReadyGate?.generation !== generation) return;
    streamReadyGate.reject(reason);
    streamReadyGate = null;
  }

  async function waitForStreamReady() {
    const gate = streamReadyGate;
    if (!gate) return;
    await Promise.race([
      gate.promise,
      new Promise<void>((_, reject) => {
        window.setTimeout(() => reject(new Error('Event stream is still syncing')), 10000);
      }),
    ]);
  }

  async function hydrateAndStream() {
    abortController?.abort();
    abortController = new AbortController();
    // Clear the module-level singleton presence store at the start of every
    // hydrate so a reconnect / new-session does not render stale entries from
    // a prior session. snapshot.seed() also clears internally as its first
    // step, but it can be seconds away (backfill in parallel + RPC roundtrip
    // + the 3s timeout fallback). Clearing here closes that visible-stale
    // window.
    presence.clear();
    // Snapshot the controller to a local so handlers resolving after a
    // concurrent re-invocation replaced the shared `abortController` do
    // not read a stale/foreign signal. Side-effect writes below are also
    // gated on generation + localController.signal.aborted.
    const localController = abortController;
    clearLines();
    setConnectionStatus('syncing');
    replayActive.set(true);
    const generation = ++streamGeneration;
    createStreamReadyGate(generation);

    if (streamSpan) {
      streamSpan.addEvent('reconnect');
      streamSpan.end();
    }
    const localSpan = tracer.startSpan('stream.lifecycle');
    streamSpan = localSpan;

    const liveBuffer: GameEvent[] = [];
    const seenEventIds = new Set<string>();
    let backfillDone = false;
    let replayComplete = false;
    // attachMomentMs is captured from the REPLAY_COMPLETE ControlFrame
    // (holomush-iu8j cursor-bounded backfill). Passed as notAfterMs on
    // subsequent backfill calls so backfill returns ONLY events that
    // existed before the live Subscribe stream attached — eliminating
    // the connect-time race fujt Fix A worked around. 0n is the
    // back-compat sentinel: a legacy server (or pre-iu8j build) sends
    // 0 → client treats as "no upper bound" and falls back to fujt
    // Fix A's gate-on-both behavior (still correct, just slower UX).
    let attachMomentMs = 0n;
    // attachMomentReady resolves when REPLAY_COMPLETE arrives (so
    // attachMomentMs has been captured), or rejects if the Subscribe
    // stream terminates without ever emitting REPLAY_COMPLETE. The
    // backfill flow awaits this so its notAfterMs reflects the actual
    // server-side attach moment. On rejection, backfill falls through
    // to its existing error handling.
    let attachMomentResolve!: () => void;
    let attachMomentReject!: (reason?: unknown) => void;
    const attachMomentReady = new Promise<void>((resolve, reject) => {
      attachMomentResolve = resolve;
      attachMomentReject = reject;
    });
    // Reset connectionId so a stale value from a prior stream doesn't
    // leak into SendCommand requests during the brief window before
    // the new stream's STREAM_OPENED ControlFrame arrives.
    connectionId = '';

    // Subscribe runs in parallel with backfill. Subscribe events arriving
    // before backfill completes are buffered and drained afterward.
    // Subscribe's replay-phase events are events the user MISSED while detached,
    // so they render as replayed=false after draining (NOT dimmed).
    //
    // streamReadyGate resolves on Subscribe REPLAY_COMPLETE — backfillDone
    // is NOT required because cursor-bounded backfill (holomush-iu8j)
    // closes the race that fujt Fix A worked around: backfill carries
    // notAfterMs = attach_moment_ms so it can ONLY return events older
    // than the live stream's attach moment. A user command sent during
    // the connect window goes through Subscribe (live), is buffered
    // until backfill drains, and renders LIVE — backfill can never see
    // it because its timestamp is > attachMomentMs.
    //
    // backfillDone is kept as a closure-local flag (NOT a gate input)
    // because the `if (!backfillDone) liveBuffer.push(ev)` Subscribe
    // path below still needs it: events arriving DURING backfill are
    // buffered so they render AFTER the dimmed scrollback (preserves
    // the LIVE-separator-then-live ordering invariant). The race
    // protection is now structural, not behavioral.
    //
    // Generation gating: resolveStreamReady / rejectStreamReady are already
    // generation-scoped (they no-op if streamReadyGate.generation !== generation),
    // so a stale Subscribe from a prior invocation cannot poison a fresh gate.

    // maybeMarkReady resolves the gate and flips connection status to
    // 'connected' once replayComplete is set. Idempotent: resolveStreamReady
    // no-ops if the gate is already resolved (or stale-generation). The
    // explicit generation+abort check mirrors the surrounding hydrateAndStream
    // gating sites.
    const maybeMarkReady = () => {
      if (!replayComplete) return;
      if (generation !== streamGeneration || localController.signal.aborted) return;
      // holomush-87qu: phase-transition event on the stream.lifecycle
      // span so Tempo / Grafana show when the stream-ready transition
      // fires. iu8j makes this fire at REPLAY_COMPLETE rather than the
      // later replay+backfill-done point, restoring fast connect UX.
      localSpan.addEvent('stream.ready');
      resolveStreamReady(generation);
      setConnectionStatus('connected');
    };
    const subscribePromise = (async () => {
      try {
        // NOTE: replayFromCursor field was removed from SubscribeRequest
        // (reserved in proto per focus substrate clean break). Subscribe
        // is now server-driven replay-from-cursor + live.
        for await (const response of client.streamEvents(
          { sessionId },
          { signal: localController.signal },
        )) {
          if (response.frame.case === 'control') {
            const ctrl = response.frame.value;
            if (ctrl.signal === ControlSignal.STREAM_OPENED) {
              // Capture per-stream connection_id for SendCommand routing.
              // Set BEFORE replayComplete so the first command can route
              // correctly even before REPLAY_COMPLETE has fired.
              // Generation guard: a stale frame from a superseded stream
              // MUST NOT clobber the active stream's connectionId — same
              // pattern as REPLAY_COMPLETE / STREAM_CLOSED downstream.
              if (generation !== streamGeneration || localController.signal.aborted) {
                continue;
              }
              connectionId = ctrl.connectionId;
            } else if (ctrl.signal === ControlSignal.REPLAY_COMPLETE) {
              // 87qu: time-from-hydrate-start to this event is the
              // server-side Subscribe-phase wall time as observed by
              // the client. Pairs with backfill.done below.
              localSpan.addEvent('subscribe.replay_complete');
              // iu8j: capture the server-side attach moment so the
              // bounded backfill below can scope its query. Production
              // servers stamp this; legacy/iu8j-pre servers send 0
              // which the client treats as "no upper bound".
              attachMomentMs = ctrl.attachMomentMs;
              attachMomentResolve();
              replayComplete = true;
              maybeMarkReady();
            } else if (ctrl.signal === ControlSignal.STREAM_CLOSED) {
              // Stale-generation guard: if a later hydrate has started,
              // skip mutating shared state (connected, sessionId) and just
              // reject our own gate. Span teardown is handled in finally.
              if (generation !== streamGeneration) {
                rejectStreamReady(generation, new Error(ctrl.message || 'Stream closed'));
                return;
              }
              if (ctrl.message) {
                appendLine(
                  { type: 'system', category: 'system', format: 'text', actor: '', text: ctrl.message },
                  false,
                );
              }
              // If TopBar's logout already cleared player auth, that handler
              // owns the navigation (to /). Skipping our own goto('/characters')
              // here prevents a navigation race that strands the user on
              // /characters with the (authed) layout's auth check still
              // racing the server-side session teardown (zhjl).
              const isLoggingOut = !get(authState).isPlayerAuthenticated;
              clearCharacterSession();
              connected = false;
              sessionId = '';
              rejectStreamReady(generation, new Error(ctrl.message || 'Stream closed'));
              setConnectionStatus('disconnected');
              if (!isLoggingOut) {
                goto('/characters');
              }
              return;
            }
          } else if (response.frame.case === 'event') {
            const ev = response.frame.value;
            if (pendingCommandSpan && ev.type === 'command_response') {
              pendingCommandSpan.end();
              pendingCommandSpan = null;
            }
            if (!backfillDone) {
              liveBuffer.push(ev);
            } else {
              if (ev.eventId && seenEventIds.has(ev.eventId)) continue;
              if (ev.eventId) seenEventIds.add(ev.eventId);
              mirrorMovementPresence(ev);
              routeEvent(ev, false);
            }
          }
        }
      } catch (e) {
        if (isStaleSession(e)) {
          await handleStaleSession();
          return;
        }
        // Gate shared-state writes: if a newer hydrate is running or our
        // own controller was aborted, only reject our own gate.
        const isStale = generation !== streamGeneration || localController.signal.aborted;
        if (!isStale && e instanceof Error && e.name !== 'AbortError') {
          connected = false;
          error = 'Connection lost. Click "Reconnect" or refresh the page.';
          setConnectionStatus('disconnected');
        }
        rejectStreamReady(generation, e);
      } finally {
        if (streamReadyGate?.generation === generation) {
          rejectStreamReady(generation, new Error('Event stream ended before replay completed'));
        }
        // iu8j: if Subscribe terminated before REPLAY_COMPLETE, signal
        // the same to the backfill flow so it doesn't hang awaiting an
        // attach moment that will never arrive. The backfill flow
        // catches this rejection and proceeds with notAfterMs=0n
        // (legacy unbounded behavior; the gate-rejection above will
        // surface the connect failure to the user anyway).
        attachMomentReject(new Error('Subscribe ended before REPLAY_COMPLETE'));
        // Only null out the shared streamSpan if it is still OUR span; a
        // stale invocation must not clobber a newer span the caller owns.
        if (streamSpan === localSpan) {
          streamSpan = null;
        }
        localSpan.end();
      }
    })();

    // Fetch presence snapshot in parallel with backfill (T11 of holomush-5b2j).
    // Failures are swallowed — empty presence is the safe fallback.
    //
    // snapshotWinner gates the race-arm event emission so only the
    // arm that fires first records its event on the span. Without
    // this, a timeout-then-late-fetch sequence would emit both
    // `snapshot.timeout` and `snapshot.received`, making winner
    // disambiguation impossible in Tempo (87qu code-review finding).
    let snapshotWinner: 'received' | 'timeout' | null = null;
    const snapshotFetchPromise = client
      .webListFocusPresence({ sessionId }, { signal: localController.signal })
      .then((resp) => {
        // 87qu: distinguish snapshot-arm wins from timeout-arm wins in
        // the race below. If the connect-latency dominator is a hung
        // webListFocusPresence call, the timeout arm will fire after
        // SNAPSHOT_TIMEOUT_MS and Tempo will show snapshot.timeout
        // instead of snapshot.received.
        if (snapshotWinner === null) {
          snapshotWinner = 'received';
          localSpan.addEvent('snapshot.received');
        }
        return resp;
      })
      .catch((err: unknown) => {
        if (isUnimplementedError(err)) {
          console.debug('[presence] snapshot unavailable (scene focus not implemented)');
        } else if (err instanceof Error && err.name === 'AbortError') {
          // Controller was aborted (reconnect or unmount) — silent.
        } else {
          console.warn('[presence] snapshot fetch failed', err);
        }
        return { entries: [] as WebPresenceEntry[] };
      });

    // Bound the wait so a hanging snapshot RPC can't stall the live-buffer
    // drain. After SNAPSHOT_TIMEOUT_MS we proceed with an empty seed; the
    // late response is discarded by the `.catch()` above (no unhandled
    // rejection). Three seconds is well below the gateway's rpcTimeout
    // (10s) so the timeout only matters when the gateway/server hangs.
    const SNAPSHOT_TIMEOUT_MS = 3000;
    const snapshotPromise = Promise.race<{ entries: WebPresenceEntry[] }>([
      snapshotFetchPromise,
      new Promise((resolve) => {
        const timer = window.setTimeout(() => {
          // 87qu: paired with snapshot.received above to disambiguate
          // race-arm winners. Seeing this event in a trace means
          // webListFocusPresence took >SNAPSHOT_TIMEOUT_MS to respond.
          if (snapshotWinner === null) {
            snapshotWinner = 'timeout';
            localSpan.addEvent('snapshot.timeout');
          }
          console.warn('[presence] snapshot timed out; seeding empty');
          resolve({ entries: [] });
        }, SNAPSHOT_TIMEOUT_MS);
        // Clear the timer if the fetch wins so we don't leave a dangling
        // handle for the remainder of SNAPSHOT_TIMEOUT_MS.
        void snapshotFetchPromise.finally(() => window.clearTimeout(timer));
      }),
    ]);

    // 87qu: time-from-hydrate-start to this event is "how long did
    // initial JS-side setup take before any backfill RPC dispatched".
    // The two backfill HTTP RPCs that follow are auto-spanned by
    // FetchInstrumentation, so the gap between this event and the
    // next webListSessionStreams span is pure JS wall time.
    localSpan.addEvent('backfill.start');

    // Backfill: enumerate streams via WebListSessionStreams, fan-out
    // WebQueryStreamHistory per stream, render as replayed=true. Failures
    // are logged and swallowed — the terminal still works with Subscribe only.
    try {
      let streams: string[] = [];
      try {
        const resp = await client.webListSessionStreams(
          { sessionId },
          { signal: localController.signal },
        );
        streams = resp.streams;
      } catch (e) {
        if (isStaleSession(e)) {
          await handleStaleSession();
          return;
        }
        if (isUnimplementedError(e)) {
          console.info('[backfill] WebListSessionStreams not available; skipping backfill');
        } else {
          console.warn('[backfill] stream enumeration failed', e);
        }
      }
      // iu8j: wait for the Subscribe attach moment so we can scope
      // backfill to events <= attachMomentMs. If the Subscribe stream
      // terminated before REPLAY_COMPLETE, attachMomentReady rejects;
      // we proceed with notAfterMs=0n (legacy unbounded) and let the
      // gate-rejection from the subscribePromise surface the connect
      // failure to the user.
      try {
        await attachMomentReady;
      } catch {
        // attachMomentMs stays at its default 0n — backfill runs
        // unbounded, matching pre-iu8j behavior.
      }
      try {
        const { events, failedStreams } = await backfillStreams(client, sessionId, streams, {
          signal: localController.signal,
          notAfterMs: attachMomentMs,
        });
        if (failedStreams.length > 0) {
          console.warn('[backfill] streams failed', { failedStreams });
        }
        for (const ev of events) {
          if (generation !== streamGeneration || localController.signal.aborted) break;
          if (ev.eventId) seenEventIds.add(ev.eventId);
          routeEvent(ev, true);
        }
      } catch (e) {
        if (isStaleSession(e)) {
          await handleStaleSession();
          return;
        }
        // backfillStreams rejects on abort — component unmount, not an
        // error worth surfacing to the user.
        if (!localController.signal.aborted) {
          console.warn('[backfill] fetch failed', e);
        }
      }
    } finally {
      // Await the snapshot (it was kicked off in parallel with backfill) and
      // seed the presence store BEFORE draining liveBuffer. This ensures that
      // Subscribe-buffered arrive/leave events merge INTO the seeded store.
      //
      // Guard against stale/aborted invocations: a reconnect or unmount can
      // race ahead of an older hydrateAndStream() that hasn't reached this
      // finally block yet — without the guard, the stale snapshot would
      // overwrite the newer generation's presence state.
      const snapshot = await snapshotPromise;
      if (generation === streamGeneration && !localController.signal.aborted) {
        presence.seed(
          snapshot.entries.map((e) => ({
            characterId: e.characterId,
            name: e.characterName,
            state: presenceStateFromProto(e.state),
          })),
        );
      }

      // 87qu: time-from-hydrate-start to this event tells us how long
      // the WebListSessionStreams + WebQueryStreamHistory fan-out took
      // (the individual RPCs are auto-spanned by FetchInstrumentation;
      // this aggregate is the JS-side completion signal). Guarded by
      // the same stale-generation check that gates the surrounding
      // presence.seed and liveBuffer drain — a stale invocation's
      // backfill.done would skew the live trace's latency baseline.
      if (generation === streamGeneration && !localController.signal.aborted) {
        localSpan.addEvent('backfill.done');
      }
      backfillDone = true;
      // iu8j: no maybeMarkReady() call here. backfillDone is no longer
      // a gate input — streamReadyGate resolves at REPLAY_COMPLETE on
      // its own. Keeping backfillDone as a closure-local flag is still
      // needed below for liveBuffer drain coordination.
      if (generation === streamGeneration && !localController.signal.aborted) {
        replayActive.set(false);
        // Drain Subscribe events that arrived during backfill, deduping.
        for (const ev of liveBuffer) {
          // Mirror movement events to the PresenceStore. The mirror call
          // covers the dedup branch (where routeEvent is skipped) and
          // ensures the PresenceStore sees every movement regardless of branch.
          mirrorMovementPresence(ev);
          if (ev.eventId && seenEventIds.has(ev.eventId)) {
            // Terminal dedup: suppress duplicate output. Presence sidebar
            // update already happened via mirrorMovementPresence above.
            continue;
          }
          if (ev.eventId) seenEventIds.add(ev.eventId);
          routeEvent(ev, false);
        }
      }
      liveBuffer.length = 0;
    }

    await subscribePromise;
  }

  async function sendCommand(command: string) {
    // command.roundtrip span — tracks from send to first command_response event
    if (pendingCommandSpan) {
      pendingCommandSpan.setStatus({ code: 2, message: 'timeout' });
      pendingCommandSpan.end();
    }
    // Capture a local handle: the module-level pendingCommandSpan can be nulled
    // or replaced across the awaits below — by a command_response event ending
    // it, or by a concurrent sendCommand starting a new one. All mutations go
    // through `span`; the module var is only cleared if it still points here.
    // command.input is set at span start so it survives a stream-ready timeout.
    const span = tracer.startSpan('command.roundtrip', {
      attributes: { 'command.input': command },
    });
    pendingCommandSpan = span;

    try {
      await waitForStreamReady();
      // Attach the connection_id actually sent to the gateway (post stream-ready)
      // plus command.name. An empty connection_id here is the holomush-dble7
      // scene-focus signal — recording it makes that bug class visible on the span.
      span.setAttributes(commandRoundtripAttributes(command, connectionId));
      const resp = await client.sendCommand({ sessionId, text: command, connectionId });
      if (!resp.success) {
        error = resp.errorMessage || 'Command failed';
        span.setStatus({ code: 2, message: error });
        span.end();
        if (pendingCommandSpan === span) pendingCommandSpan = null;
      }
    } catch (e) {
      if (isStaleSession(e)) {
        await handleStaleSession();
        return;
      }
      error = e instanceof Error ? e.message : 'Command failed';
      span.setStatus({ code: 2, message: error });
      span.end();
      if (pendingCommandSpan === span) pendingCommandSpan = null;
    }
  }

  function handleExitClick(direction: string) {
    sendCommand(direction);
  }

  async function disconnect() {
    abortController?.abort();
    try {
      await client.disconnect({ sessionId });
    } catch { /* best effort */ }
    clearCharacterSession();
    connected = false;
    sessionId = '';
    setConnectionStatus('disconnected');
    goto('/characters');
  }

  async function reconnect() {
    error = '';
    connected = true;
    hydrateAndStream();
  }
</script>

{#if !connected}
  <div class="login-screen">
    <h1>HoloMUSH</h1>
    {#if error}<p class="error">{error}</p>{/if}
    {#if sessionId}
      <button onclick={reconnect}>Reconnect</button>
      <button class="secondary" onclick={() => { clearAuth(); goto('/'); }}>New Session</button>
    {:else}
      <button onclick={() => goto('/login')}>Sign In</button>
    {/if}
  </div>
{:else}
  <div class="terminal-layout" style={$themePreferences.terminalBlackBackground ? terminalBlackOverrideVars() : ''}>
    <Rail />
    <div class="main-area" bind:this={paneGroupEl}>
      <Resizable.PaneGroup direction="horizontal">
        <Resizable.Pane defaultSize={100 - sidebarDefaultPct} class="terminal-column">
          <TerminalView />
          <CommandInput
            {sessionId}
            onSend={sendCommand}
            {injectText}
            onInjectConsumed={handleInjectConsumed}
          />
        </Resizable.Pane>
        <Resizable.Handle withHandle />
        <Resizable.Pane
          defaultSize={sidebarDefaultPct}
          onResize={handleSidebarResize}
        >
          <Sidebar onExitClick={handleExitClick} onInject={handleInject} resizable />
        </Resizable.Pane>
      </Resizable.PaneGroup>
    </div>
  </div>
{/if}

<style>
  .login-screen {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: calc(100vh - var(--topbar-h));
    gap: 16px;
    font-family: 'JetBrains Mono', monospace;
    background: var(--color-background, #0d0d1a);
    color: var(--color-input-text, #e0e0e0);
  }
  .login-screen button {
    padding: 8px 24px;
    background: var(--color-input-prompt, #4fc3f7);
    color: var(--color-background, #0d0d1a);
    border: none;
    border-radius: 4px;
    cursor: pointer;
    font-family: inherit;
    font-size: 14px;
  }
  .error { color: var(--mush-system, #e57373); }
  .login-screen .secondary {
    background: transparent;
    color: var(--color-status-text, #888);
    border: 1px solid var(--color-border, #444);
  }
  .terminal-layout {
    display: flex;
    flex-direction: row;
    height: calc(100vh - var(--topbar-h));
    font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', monospace;
    font-size: 15px;
    background: var(--color-background);
    color: var(--color-input-text);
  }
  .main-area {
    flex: 1;
    display: flex;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }
  .main-area > :global(*) {
    flex: 1;
    min-width: 0;
    min-height: 0;
  }
  :global(.terminal-column) {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    height: 100%;
  }
</style>
