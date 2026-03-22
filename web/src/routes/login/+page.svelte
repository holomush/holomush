<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { setPlayerAuth, setCharacterSession, setGuestSession } from '$lib/stores/authStore';
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import { goto } from '$app/navigation';

  const client = createClient(WebService, transport);

  let username = $state('');
  let password = $state('');
  let rememberMe = $state(false);
  let error = $state('');
  let loading = $state(false);

  async function handleLogin() {
    if (!username || !password) {
      error = 'Username and password are required.';
      return;
    }
    error = '';
    loading = true;
    try {
      const resp = await client.webAuthenticatePlayer({ username, password, rememberMe });
      if (resp.success) {
        setPlayerAuth(resp.playerToken, username);
        const autoCharId = resp.defaultCharacterId || (resp.characters.length === 1 ? resp.characters[0].characterId : '');
        if (autoCharId) {
          const selectResp = await client.webSelectCharacter({
            playerToken: resp.playerToken,
            characterId: autoCharId,
          });
          if (selectResp.success) {
            setCharacterSession(selectResp.sessionId, selectResp.characterName);
            goto('/terminal');
            return;
          }
        }
        goto('/characters');
      } else {
        error = resp.errorMessage || 'Login failed.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Login failed.';
    } finally {
      loading = false;
    }
  }

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

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') handleLogin();
  }
</script>

<div class="page" style={themeToCssVars($activeTheme.colors)}>
  <div class="card">
    <h1 class="title">Sign In</h1>

    {#if error}
      <p class="error">{error}</p>
    {/if}

    <label class="field">
      <span class="label">Username</span>
      <input
        type="text"
        name="username"
        bind:value={username}
        placeholder="username"
        autocomplete="username"
        onkeydown={handleKeydown}
      />
    </label>

    <label class="field">
      <span class="label">Password</span>
      <input
        type="password"
        name="password"
        bind:value={password}
        placeholder="••••••••"
        autocomplete="current-password"
        onkeydown={handleKeydown}
      />
    </label>

    <div class="row">
      <label class="checkbox-label">
        <input type="checkbox" bind:checked={rememberMe} />
        <span>Remember me</span>
      </label>
      <a href="/reset" class="forgot">Forgot password?</a>
    </div>

    <button class="btn-primary" type="submit" onclick={handleLogin} disabled={loading}>
      {loading ? 'Signing in…' : 'Sign In'}
    </button>

    <p class="register-link">
      New here? <a href="/register">Create an account</a>
    </p>

    <div class="separator"><span>or</span></div>

    <button class="btn-ghost" onclick={handleGuest} disabled={loading}>
      Try as Guest →
    </button>
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

  input[type='text']:focus,
  input[type='password']:focus {
    border-color: var(--color-say-speaker);
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .checkbox-label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--color-input-text);
    cursor: pointer;
  }

  .forgot {
    font-size: 12px;
    color: var(--color-status-text);
    text-decoration: none;
  }

  .forgot:hover {
    color: var(--color-say-speaker);
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

  .register-link {
    font-size: 12px;
    color: var(--color-status-text);
    text-align: center;
    margin: 0;
  }

  .register-link a {
    color: var(--color-say-speaker);
    text-decoration: none;
  }

  .separator {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--color-status-text);
    font-size: 11px;
  }

  .separator::before,
  .separator::after {
    content: '';
    flex: 1;
    border-top: 1px solid var(--color-border);
  }

  .btn-ghost {
    background: transparent;
    border: 1px solid var(--color-border);
    border-radius: 4px;
    color: var(--color-input-text);
    font-family: inherit;
    font-size: 13px;
    padding: 10px;
    cursor: pointer;
    transition: border-color 0.15s;
  }

  .btn-ghost:hover {
    border-color: var(--color-say-speaker);
  }

  .btn-ghost:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
</style>
