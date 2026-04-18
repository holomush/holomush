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
  import { themePreferences, terminalBlackOverrideVars } from '$lib/stores/themeStore';
  import { setConnectionStatus } from '$lib/stores/connectionStore';
  import { toggleSidebar } from '$lib/stores/uiPrefsStore';
  import * as Resizable from '$lib/components/ui/resizable';
  import { authState, clearAuth, clearCharacterSession } from '$lib/stores/authStore';
  import TerminalView from '$lib/components/terminal/TerminalView.svelte';
  import CommandInput from '$lib/components/terminal/CommandInput.svelte';
  import Sidebar from '$lib/components/sidebar/Sidebar.svelte';
  import { goto } from '$app/navigation';
  import { trace, type Span } from '@opentelemetry/api';
  import { backfillStreams } from '$lib/backfill/streamBackfill';
  import { isUnimplementedError } from '$lib/connect/errors';
  import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';

  const client = createClient(WebService, transport);
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
  let connected = $state(false);
  let error = $state('');
  let abortController: AbortController | null = null;

  function onKeydown(e: KeyboardEvent) {
    if (e.ctrlKey && e.key === 'b') {
      e.preventDefault();
      toggleSidebar();
    }
    if (e.ctrlKey && e.key === 'l') {
      e.preventDefault();
      clearLines();
    }
  }

  onMount(() => {
    window.addEventListener('keydown', onKeydown);

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
    window.removeEventListener('keydown', onKeydown);
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

    // Subscribe runs in parallel with backfill. Subscribe events arriving
    // before backfill completes are buffered and drained afterward.
    // Subscribe's replay-phase events are events the user MISSED while detached,
    // so they render as replayed=false after draining (NOT dimmed).
    //
    // streamReadyGate resolves on REPLAY_COMPLETE (not backfill). Commands
    // sent during backfill get a response via Subscribe which is buffered
    // and drains as live after the dimmed scrollback.
    //
    // Generation gating: resolveStreamReady / rejectStreamReady are already
    // generation-scoped (they no-op if streamReadyGate.generation !== generation),
    // so a stale Subscribe from a prior invocation cannot poison a fresh gate.
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
            if (ctrl.signal === ControlSignal.REPLAY_COMPLETE) {
              resolveStreamReady(generation);
              setConnectionStatus('connected');
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
                  { type: 'system', characterName: '', text: ctrl.message, channel: 0 },
                  false,
                );
              }
              clearCharacterSession();
              connected = false;
              sessionId = '';
              rejectStreamReady(generation, new Error(ctrl.message || 'Stream closed'));
              setConnectionStatus('disconnected');
              goto('/characters');
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
              routeEvent(ev, false);
            }
          }
        }
      } catch (e) {
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
        // Only null out the shared streamSpan if it is still OUR span; a
        // stale invocation must not clobber a newer span the caller owns.
        if (streamSpan === localSpan) {
          streamSpan = null;
        }
        localSpan.end();
      }
    })();

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
        if (isUnimplementedError(e)) {
          console.info('[backfill] WebListSessionStreams not available; skipping backfill');
        } else {
          console.warn('[backfill] stream enumeration failed', e);
        }
      }
      try {
        const { events, failedStreams } = await backfillStreams(client, sessionId, streams, {
          signal: localController.signal,
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
        // backfillStreams rejects on abort — component unmount, not an
        // error worth surfacing to the user.
        if (!localController.signal.aborted) {
          console.warn('[backfill] fetch failed', e);
        }
      }
    } finally {
      backfillDone = true;
      if (generation === streamGeneration && !localController.signal.aborted) {
        replayActive.set(false);
        // Drain Subscribe events that arrived during backfill, deduping.
        for (const ev of liveBuffer) {
          if (ev.eventId && seenEventIds.has(ev.eventId)) continue;
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
    pendingCommandSpan = tracer.startSpan('command.roundtrip', {
      attributes: { 'command.input': command },
    });

    try {
      await waitForStreamReady();
      const resp = await client.sendCommand({ sessionId, text: command });
      if (!resp.success) {
        error = resp.errorMessage || 'Command failed';
        pendingCommandSpan?.setStatus({ code: 2, message: error });
        pendingCommandSpan?.end();
        pendingCommandSpan = null;
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Command failed';
      pendingCommandSpan?.setStatus({ code: 2, message: error });
      pendingCommandSpan?.end();
      pendingCommandSpan = null;
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
    <Resizable.PaneGroup direction="horizontal" class="main-area">
      <Resizable.Pane defaultSize={75} class="terminal-column">
        <TerminalView />
        <CommandInput {sessionId} onSend={sendCommand} />
      </Resizable.Pane>
      <Resizable.Handle withHandle />
      <Resizable.Pane defaultSize={25}>
        <Sidebar onExitClick={handleExitClick} resizable />
      </Resizable.Pane>
    </Resizable.PaneGroup>
  </div>
{/if}

<style>
  .login-screen {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: calc(100vh - 32px);
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
    flex-direction: column;
    height: calc(100vh - 32px);
    font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', monospace;
    font-size: 15px;
    background: var(--color-background);
    color: var(--color-input-text);
  }
</style>
