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
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import { authState, clearAuth } from '$lib/stores/authStore';
  import TerminalView from '$lib/components/terminal/TerminalView.svelte';
  import CommandInput from '$lib/components/terminal/CommandInput.svelte';
  import StatusBar from '$lib/components/terminal/StatusBar.svelte';
  import Sidebar from '$lib/components/sidebar/Sidebar.svelte';
  import { goto } from '$app/navigation';

  const client = createClient(WebService, transport);

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

    try {
      for await (const response of client.streamEvents(
        { sessionId, replayFromCursor: true },
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
            clearAuth();
            connected = false;
            sessionId = '';
            return;
          }
        } else if (response.frame.case === 'event') {
          routeEvent(response.frame.value, inReplay);
        }
      }
    } catch (e) {
      if (e instanceof Error && e.name !== 'AbortError') {
        connected = false;
        error = 'Connection lost. Click "Reconnect" or refresh the page.';
      }
    }
  }

  async function sendCommand(command: string) {
    try {
      const resp = await client.sendCommand({ sessionId, text: command });
      if (!resp.success) {
        error = resp.errorMessage || 'Command failed';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Command failed';
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
    clearAuth();
    connected = false;
    sessionId = '';
    goto('/');
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
  <div class="terminal-layout" style={themeToCssVars($activeTheme.colors)}>
    <StatusBar
      {characterName}
      {connected}
      syncing={$replayActive}
      onToggleSidebar={toggleSidebar}
      showHamburger={isMobile}
    />
    <div class="main-area">
      <div class="terminal-column">
        <TerminalView />
        <CommandInput {sessionId} onSend={sendCommand} />
      </div>
      {#if !isMobile}
        <Sidebar onExitClick={handleExitClick} />
      {:else}
        <Sidebar onExitClick={handleExitClick} overlay />
      {/if}
    </div>
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
    background: #0d0d1a;
    color: #e0e0e0;
  }
  .login-screen button {
    padding: 8px 24px;
    background: #4fc3f7;
    color: #0d0d1a;
    border: none;
    border-radius: 4px;
    cursor: pointer;
    font-family: inherit;
    font-size: 14px;
  }
  .error { color: #e57373; }
  .login-screen .secondary {
    background: transparent;
    color: #888;
    border: 1px solid #444;
  }
  .terminal-layout {
    display: flex;
    flex-direction: column;
    height: calc(100vh - 32px);
    font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', monospace;
    font-size: 13px;
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
  }
</style>
