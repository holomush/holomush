<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { setPlayerAuth } from '$lib/stores/authStore';
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import { goto } from '$app/navigation';

  const client = createClient(WebService, transport);

  let username = $state('');
  let email = $state('');
  let password = $state('');
  let confirmPassword = $state('');
  let error = $state('');
  let loading = $state(false);

  function validate(): string {
    if (!username) return 'Username is required.';
    if (password.length < 8) return 'Password must be at least 8 characters.';
    if (password !== confirmPassword) return 'Passwords do not match.';
    return '';
  }

  async function handleRegister() {
    const validationError = validate();
    if (validationError) {
      error = validationError;
      return;
    }
    error = '';
    loading = true;
    try {
      const resp = await client.webCreatePlayer({ username, password, email });
      if (resp.success) {
        // TODO: Once web RPCs read playerToken from the httpOnly cookie exclusively,
        // stop storing it in authStore and just set playerName for display.
        setPlayerAuth(resp.playerToken, username);
        goto('/characters');
      } else {
        error = resp.errorMessage || 'Registration failed.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Registration failed.';
    } finally {
      loading = false;
    }
  }
</script>

<div class="page" style={themeToCssVars($activeTheme.colors)}>
  <div class="card">
    <h1 class="title">Create Account</h1>

    {#if error}
      <p class="error">{error}</p>
    {/if}

    <label class="field">
      <span class="label">Username</span>
      <input type="text" name="username" bind:value={username} placeholder="username" autocomplete="username" />
    </label>

    <label class="field">
      <span class="label">Email (optional)</span>
      <input type="email" name="email" bind:value={email} placeholder="you@example.com" autocomplete="email" />
    </label>

    <label class="field">
      <span class="label">Password</span>
      <input
        type="password"
        name="password"
        bind:value={password}
        placeholder="min 8 characters"
        autocomplete="new-password"
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
      />
    </label>

    <button class="btn-primary" type="submit" onclick={handleRegister} disabled={loading}>
      {loading ? 'Creating account…' : 'Create Account'}
    </button>

    <p class="login-link">
      Already have an account? <a href="/login">Sign in</a>
    </p>
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

  input[type='text'],
  input[type='email'],
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

  input:focus {
    border-color: var(--color-say-speaker);
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

  .login-link {
    font-size: 12px;
    color: var(--color-status-text);
    text-align: center;
    margin: 0;
  }

  .login-link a {
    color: var(--color-say-speaker);
    text-decoration: none;
  }
</style>
