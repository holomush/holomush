<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { setGuestSession } from '$lib/stores/authStore';
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import { goto } from '$app/navigation';

  const client = createClient(WebService, transport);

  let loading = $state(false);
  let error = $state('');

  async function handleGuest() {
    error = '';
    loading = true;
    try {
      const resp = await client.login({ username: 'guest', password: '' });
      if (resp.success) {
        setGuestSession(resp.sessionId, resp.characterName);
        goto('/terminal');
      } else {
        error = resp.errorMessage || 'Guest login failed.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Guest login failed.';
    } finally {
      loading = false;
    }
  }
</script>

<div class="landing" style={themeToCssVars($activeTheme.colors)}>
  <h1 class="title">HoloMUSH</h1>
  <p class="subtitle">A modern MUSH platform</p>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  <div class="actions">
    <a href="/login" class="btn-primary">Login</a>
    <a href="/register" class="btn-secondary">Register</a>
    <button class="btn-ghost" onclick={handleGuest} disabled={loading}>
      {loading ? 'Connecting…' : 'Try as Guest'}
    </button>
  </div>
</div>

<style>
  .landing {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    min-height: calc(100vh - 32px);
    gap: 16px;
    font-family: 'JetBrains Mono', monospace;
    background: var(--color-background);
    color: var(--color-input-text);
  }

  .title {
    font-size: 36px;
    color: var(--color-say-speaker);
    margin: 0;
    font-weight: 700;
    letter-spacing: 0.05em;
  }

  .subtitle {
    font-size: 14px;
    color: var(--color-status-text);
    margin: 0;
  }

  .error {
    font-size: 12px;
    color: var(--color-command-error);
    margin: 0;
  }

  .actions {
    display: flex;
    gap: 12px;
    margin-top: 8px;
    flex-wrap: wrap;
    justify-content: center;
  }

  .btn-primary {
    padding: 10px 24px;
    background: var(--color-say-speaker);
    color: var(--color-background);
    border-radius: 4px;
    text-decoration: none;
    font-family: inherit;
    font-size: 13px;
    font-weight: 600;
  }

  .btn-secondary {
    padding: 10px 24px;
    background: transparent;
    color: var(--color-say-speaker);
    border: 1px solid var(--color-say-speaker);
    border-radius: 4px;
    text-decoration: none;
    font-family: inherit;
    font-size: 13px;
  }

  .btn-ghost {
    padding: 10px 24px;
    background: transparent;
    color: var(--color-status-text);
    border: 1px solid var(--color-border);
    border-radius: 4px;
    font-family: inherit;
    font-size: 13px;
    cursor: pointer;
    transition: border-color 0.15s, color 0.15s;
  }

  .btn-ghost:hover {
    border-color: var(--color-say-speaker);
    color: var(--color-input-text);
  }

  .btn-ghost:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
</style>
