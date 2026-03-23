<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { LogOut, ArrowLeftRight } from 'lucide-svelte';
  import { authState, clearAuth } from '$lib/stores/authStore';
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { goto } from '$app/navigation';

  const client = createClient(WebService, transport);

  async function handleLogout() {
    try {
      await client.webLogout({ sessionId: $authState.sessionId ?? '' });
    } catch {
      /* best effort — may fail if no server-side session exists */
    }
    clearAuth();
    goto('/');
  }

  function handleSwitchCharacter() {
    goto('/characters');
  }
</script>

<header style={themeToCssVars($activeTheme.colors)}>
  <div class="left">
    <a href="/" class="logo">
      <span class="logo-icon">H</span>
      <span class="logo-text">HoloMUSH</span>
    </a>
  </div>
  <nav class="right">
    {#if !$authState.playerToken && !$authState.sessionId}
      <a href="/login" class="nav-link">Login</a>
      <a href="/register" class="nav-link accent">Register</a>
    {:else if $authState.sessionId && $authState.characterName}
      <span class="char-name">{$authState.characterName}</span>
      <button class="icon-btn" onclick={handleSwitchCharacter} title="Switch character" aria-label="Switch character">
        <ArrowLeftRight size={16} />
      </button>
      <button class="icon-btn" onclick={handleLogout} title="Logout" aria-label="Log out">
        <LogOut size={16} />
      </button>
    {:else if $authState.playerToken}
      <span class="player-name">{$authState.playerName}</span>
      <button class="icon-btn" onclick={handleLogout} title="Logout" aria-label="Log out">
        <LogOut size={16} />
      </button>
    {/if}
  </nav>
</header>

<style>
  header {
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 12px;
    background: var(--color-surface);
    border-bottom: 1px solid var(--color-border);
    flex-shrink: 0;
    font-family: 'JetBrains Mono', monospace;
    font-size: 12px;
  }

  .left {
    display: flex;
    align-items: center;
  }

  .logo {
    display: flex;
    align-items: center;
    gap: 6px;
    text-decoration: none;
    color: var(--color-input-text);
  }

  .logo-icon {
    width: 20px;
    height: 20px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--color-say-speaker);
    color: var(--color-background);
    border-radius: 3px;
    font-weight: bold;
    font-size: 11px;
    flex-shrink: 0;
  }

  .logo-text {
    color: var(--color-say-speaker);
    font-weight: 600;
    letter-spacing: 0.05em;
  }

  .right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .nav-link {
    color: var(--color-input-text);
    text-decoration: none;
    padding: 2px 8px;
    border-radius: 3px;
    border: 1px solid var(--color-border);
    transition: border-color 0.15s;
  }

  .nav-link:hover {
    border-color: var(--color-say-speaker);
  }

  .nav-link.accent {
    background: var(--color-say-speaker);
    color: var(--color-background);
    border-color: var(--color-say-speaker);
  }

  .char-name,
  .player-name {
    color: var(--color-say-speaker);
    font-size: 12px;
  }

  .icon-btn {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--color-status-text);
    display: flex;
    align-items: center;
    padding: 2px;
    border-radius: 3px;
    transition: color 0.15s;
  }

  .icon-btn:hover {
    color: var(--color-input-text);
  }
</style>
