<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { routeEvent } from '$lib/stores/eventRouter';
  import { clearLines } from '$lib/stores/terminalStore';
  import { toggleSidebar } from '$lib/stores/sidebarStore';
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import TerminalView from '$lib/components/terminal/TerminalView.svelte';
  import CommandInput from '$lib/components/terminal/CommandInput.svelte';
  import StatusBar from '$lib/components/terminal/StatusBar.svelte';
  import Sidebar from '$lib/components/sidebar/Sidebar.svelte';

  const client = createClient(WebService, transport);
  const SESSION_KEY = 'holomush-session';

  let sessionId = $state('');
  let characterName = $state('');
  let connected = $state(false);
  let error = $state('');
  let abortController: AbortController | null = null;
  let isMobile = $state(false);

  function saveSession() {
    sessionStorage.setItem(SESSION_KEY, JSON.stringify({ sessionId, characterName }));
  }

  function clearSession() {
    sessionStorage.removeItem(SESSION_KEY);
  }

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

    const saved = sessionStorage.getItem(SESSION_KEY);
    if (saved) {
      try {
        const { sessionId: sid, characterName: name } = JSON.parse(saved);
        if (sid) {
          sessionId = sid;
          characterName = name;
          connected = true;
          startStreaming();
        }
      } catch {
        clearSession();
      }
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

  async function login() {
    try {
      const resp = await client.login({ username: 'guest', password: '' });
      sessionId = resp.sessionId;
      characterName = resp.characterName;
      connected = true;
      saveSession();
      startStreaming();
    } catch (e) {
      error = e instanceof Error ? e.message : 'Login failed';
    }
  }

  async function startStreaming() {
    abortController?.abort();
    abortController = new AbortController();
    clearLines();

    try {
      for await (const response of client.streamEvents(
        { sessionId, replayFromCursor: true },
        { signal: abortController.signal }
      )) {
        routeEvent(response);
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
    clearSession();
    connected = false;
    sessionId = '';
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
      <button class="secondary" onclick={() => { clearSession(); sessionId = ''; error = ''; }}>New Session</button>
    {:else}
      <button onclick={login}>Connect as Guest</button>
    {/if}
  </div>
{:else}
  <div class="terminal-layout" style={themeToCssVars($activeTheme.colors)}>
    <StatusBar
      {characterName}
      {connected}
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
    height: 100vh;
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
    height: 100vh;
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
