<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';

  const client = createClient(WebService, transport);

  let email = $state('');
  let submitted = $state(false);
  let loading = $state(false);

  async function handleSubmit() {
    loading = true;
    try {
      await client.webRequestPasswordReset({ email });
    } catch {
      /* always show success to avoid email enumeration */
    } finally {
      loading = false;
      submitted = true;
    }
  }
</script>

<div class="page" style={themeToCssVars($activeTheme.colors)}>
  <div class="card">
    <h1 class="title">Reset Password</h1>

    {#if submitted}
      <p class="success">
        If that email is registered, you'll receive a reset link shortly.
      </p>
      <a href="/login" class="back-link">← Back to login</a>
    {:else}
      <p class="hint">Enter your email address and we'll send you a reset link.</p>

      <label class="field">
        <span class="label">Email</span>
        <input
          type="email"
          bind:value={email}
          placeholder="you@example.com"
          autocomplete="email"
          onkeydown={(e) => e.key === 'Enter' && handleSubmit()}
        />
      </label>

      <button class="btn-primary" onclick={handleSubmit} disabled={loading}>
        {loading ? 'Sending…' : 'Send Reset Link'}
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

  .hint {
    font-size: 12px;
    color: var(--color-status-text);
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

  input[type='email'] {
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

  input[type='email']:focus {
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
