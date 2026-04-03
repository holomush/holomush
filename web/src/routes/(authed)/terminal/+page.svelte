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
  import { toggleSidebar } from '$lib/stores/sidebarStore';
  import { themePreferences, terminalBlackOverrideVars } from '$lib/stores/themeStore';
  import * as Resizable from '$lib/components/ui/resizable';
  import { authState, clearAuth, clearCharacterSession } from '$lib/stores/authStore';
  import TerminalView from '$lib/components/terminal/TerminalView.svelte';
  import CommandInput from '$lib/components/terminal/CommandInput.svelte';
  import StatusBar from '$lib/components/terminal/StatusBar.svelte';
  import Sidebar from '$lib/components/sidebar/Sidebar.svelte';
  import { goto } from '$app/navigation';
  import { trace, type Span } from '@opentelemetry/api';

  const client = createClient(WebService, transport);
  const tracer = trace.getTracer('holomush-web');
  let pendingCommandSpan: Span | null = null;
  let streamSpan: Span | null = null;

  let sessionId = $state('');
  let characterName = $state('');
  let connected = $state(false);
  let error = $state('');
  let abortController: AbortController | null = null;
  let isMobile = $state(false);

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
    checkMobile();
    window.addEventListener('resize', checkMobile);
    window.addEventListener('keydown', onKeydown);

    const sid = $authState.sessionId;
    const name = $authState.characterName;

    if (sid) {
      sessionId = sid;
      characterName = name ?? '';
      connected = true;
      startStreaming();
    } else {
      // Auth guard should have prevented this, but redirect as fallback
      goto('/login');
    }
  });

  onDestroy(() => {
    window.removeEventListener('resize', checkMobile);
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

  function checkMobile() {
    isMobile = window.innerWidth < 768;
  }

  async function startStreaming() {
    abortController?.abort();
    abortController = new AbortController();
    clearLines();
    replayActive.set(true);
    let inReplay = true;

    // stream.lifecycle span — tracks the lifetime of the event stream connection
    if (streamSpan) {
      streamSpan.addEvent('reconnect');
      streamSpan.end();
    }
    streamSpan = tracer.startSpan('stream.lifecycle');

    try {
      for await (const response of client.streamEvents(
        { sessionId, replayFromCursor: false },
        { signal: abortController.signal }
      )) {
        if (response.frame.case === 'control') {
          const ctrl = response.frame.value;
          if (ctrl.signal === ControlSignal.REPLAY_COMPLETE) {
            inReplay = false;
            replayActive.set(false);
          } else if (ctrl.signal === ControlSignal.STREAM_CLOSED) {
            if (ctrl.message) {
              appendLine(
                { type: 'system', characterName: '', text: ctrl.message, channel: 0 },
                false,
              );
            }
            clearCharacterSession();
            connected = false;
            sessionId = '';
            streamSpan?.end();
            streamSpan = null;
            goto('/characters');
            return;
          }
        } else if (response.frame.case === 'event') {
          // Resolve any pending command.roundtrip span on first response after a command
          if (pendingCommandSpan && response.frame.value.type === 'command_response') {
            pendingCommandSpan.end();
            pendingCommandSpan = null;
          }
          routeEvent(response.frame.value, inReplay);
        }
      }
    } catch (e) {
      if (e instanceof Error && e.name !== 'AbortError') {
        connected = false;
        error = 'Connection lost. Click "Reconnect" or refresh the page.';
      }
    } finally {
      streamSpan?.end();
      streamSpan = null;
    }
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
    goto('/characters');
  }

  async function reconnect() {
    error = '';
    connected = true;
    startStreaming();
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
    <StatusBar
      {characterName}
      {connected}
      syncing={$replayActive}
      onToggleSidebar={toggleSidebar}
      showHamburger={isMobile}
    />
    {#if !isMobile}
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
    {:else}
      <div class="main-area">
        <div class="terminal-column">
          <TerminalView />
          <CommandInput {sessionId} onSend={sendCommand} />
        </div>
        <Sidebar onExitClick={handleExitClick} overlay />
      </div>
    {/if}
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
  .main-area {
    flex: 1;
    display: flex;
    overflow: hidden;
    position: relative;
  }
  .terminal-column {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    height: 100%;
  }
</style>
