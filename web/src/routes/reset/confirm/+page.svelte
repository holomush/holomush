<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import { goto } from '$app/navigation';

  const client = createClient(WebService, transport);

  let token = $state('');
  let newPassword = $state('');
  let confirmPassword = $state('');
  let error = $state('');
  let loading = $state(false);
  let success = $state(false);

  onMount(() => {
    token = $page.url.searchParams.get('token') ?? '';
    if (!token) {
      error = 'Invalid or missing reset token.';
    }
  });

  async function handleReset() {
    if (newPassword.length < 8) {
      error = 'Password must be at least 8 characters.';
      return;
    }
    if (newPassword !== confirmPassword) {
      error = 'Passwords do not match.';
      return;
    }
    error = '';
    loading = true;
    try {
      const resp = await client.webConfirmPasswordReset({ token, newPassword });
      if (resp.success) {
        success = true;
        setTimeout(() => goto('/login'), 2000);
      } else {
        error = resp.errorMessage || 'Reset failed. The link may have expired.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Reset failed.';
    } finally {
      loading = false;
    }
  }
</script>

<div class="page" style={themeToCssVars($activeTheme.colors)}>
  <div class="card">
    <h1 class="title">New Password</h1>

    {#if success}
      <p class="success">Password changed! Redirecting to login…</p>
    {:else}
      {#if error}
        <p class="error">{error}</p>
      {/if}

      <label class="field">
        <span class="label">New Password</span>
        <input
          type="password"
          name="password"
          bind:value={newPassword}
          placeholder="min 8 characters"
          autocomplete="new-password"
          disabled={!token}
        />
      </label>

      <label class="field">
        <span class="label">Confirm Password</span>
        <input
          type="password"
          name="confirmPassword"
          bind:value={confirmPassword}
          placeholder="••••••••"
          autocomplete="new-password"
          disabled={!token}
          onkeydown={(e) => e.key === 'Enter' && handleReset()}
        />
      </label>

      <button class="btn-primary" onclick={handleReset} disabled={loading || !token}>
        {loading ? 'Saving…' : 'Set New Password'}
      </button>

      <a href="/login" class="back-link">← Back to login</a>
    {/if}
  </div>
</div>

<style>
  .page {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: calc(100vh - 32px);
    background: var(--color-background);
    font-family: 'JetBrains Mono', monospace;
  }

  .card {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    padding: 32px;
    width: 100%;
    max-width: 360px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .title {
    font-size: 20px;
    color: var(--color-say-speaker);
    margin: 0;
    font-weight: 600;
    text-align: center;
  }

  .error {
    background: rgba(229, 115, 115, 0.1);
    border: 1px solid var(--color-command-error);
    border-radius: 4px;
    color: var(--color-command-error);
    padding: 8px 12px;
    font-size: 12px;
    margin: 0;
  }

  .success {
    font-size: 13px;
    color: var(--color-pose-actor);
    margin: 0;
    padding: 12px;
    background: rgba(129, 199, 132, 0.1);
    border: 1px solid var(--color-pose-actor);
    border-radius: 4px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .label {
    font-size: 11px;
    color: var(--color-status-text);
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  input[type='password'] {
    background: var(--color-input-background);
    border: 1px solid var(--color-border);
    border-radius: 4px;
    color: var(--color-input-text);
    font-family: inherit;
    font-size: 13px;
    padding: 8px 10px;
    outline: none;
    transition: border-color 0.15s;
    width: 100%;
    box-sizing: border-box;
  }

  input[type='password']:focus {
    border-color: var(--color-say-speaker);
  }

  input:disabled {
    opacity: 0.5;
  }

  .btn-primary {
    background: var(--color-say-speaker);
    color: var(--color-background);
    border: none;
    border-radius: 4px;
    padding: 10px;
    font-family: inherit;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    transition: opacity 0.15s;
  }

  .btn-primary:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .back-link {
    font-size: 12px;
    color: var(--color-status-text);
    text-decoration: none;
    text-align: center;
  }

  .back-link:hover {
    color: var(--color-say-speaker);
  }
</style>
