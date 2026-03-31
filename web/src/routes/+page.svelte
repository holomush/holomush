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
  import MarkdownContent from '$lib/components/MarkdownContent.svelte';
  import FeatureCard from '$lib/components/FeatureCard.svelte';
  import type { ContentItem } from '$lib/stores/contentStore';

  let {
    hero,
    pitch,
    features,
    connectInfo,
  }: {
    hero?: ContentItem;
    pitch?: ContentItem;
    features: ContentItem[];
    connectInfo?: ContentItem;
  } = $props();

  const client = createClient(WebService, transport);

  let loading = $state(false);
  let error = $state('');

  const hasContent = $derived(!!hero || !!pitch || features.length > 0 || !!connectInfo);
  const heroTitle = $derived(hero?.metadata?.title ?? 'HoloMUSH');
  const heroTagline = $derived(hero?.metadata?.tagline ?? 'A modern MUSH platform');

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

<div class="landing" style={themeToCssVars($activeTheme.colors)} data-testid="landing">
  <!-- Hero -->
  <section class="hero" data-testid="hero">
    <h1 class="title" data-testid="hero-title">{heroTitle}</h1>
    <p class="subtitle" data-testid="hero-tagline">{heroTagline}</p>

    {#if error}
      <p class="error" data-testid="hero-error">{error}</p>
    {/if}

    <div class="actions">
      <a href="/login" class="btn-primary" data-testid="login-link">Login</a>
      <a href="/register" class="btn-secondary" data-testid="register-link">Register</a>
      <button class="btn-ghost" onclick={handleGuest} disabled={loading} data-testid="guest-button">
        {loading ? 'Connecting…' : 'Try as Guest'}
      </button>
    </div>
  </section>

  {#if hasContent}
    <!-- Pitch -->
    {#if pitch}
      <section class="pitch" data-testid="pitch">
        <MarkdownContent content={pitch.body} />
      </section>
    {/if}

    <!-- Feature grid -->
    {#if features.length > 0}
      <section class="features" data-testid="features">
        <div class="feature-grid" data-testid="feature-grid">
          {#each features as feature (feature.key)}
            <FeatureCard
              title={feature.metadata?.title ?? feature.key}
              body={feature.body}
            />
          {/each}
        </div>
      </section>
    {/if}

    <!-- Connect info -->
    {#if connectInfo}
      <section class="connect" data-testid="connect">
        <MarkdownContent content={connectInfo.body} />
      </section>
    {/if}
  {/if}
</div>

<style>
  .landing {
    display: flex;
    flex-direction: column;
    align-items: center;
    min-height: calc(100vh - 32px);
    font-family: 'JetBrains Mono', monospace;
    background: var(--color-background);
    color: var(--color-input-text);
    padding: 0 24px 48px;
    box-sizing: border-box;
  }

  /* Hero */
  .hero {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 16px;
    padding: 64px 0 48px;
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

  /* Pitch */
  .pitch {
    max-width: 680px;
    width: 100%;
    text-align: center;
    font-size: 14px;
    color: var(--color-input-text);
    line-height: 1.7;
    padding-bottom: 40px;
    border-bottom: 1px solid var(--color-border);
    margin-bottom: 40px;
  }

  .pitch :global(p) {
    margin: 0 0 12px;
  }

  .pitch :global(p:last-child) {
    margin-bottom: 0;
  }

  /* Features */
  .features {
    width: 100%;
    max-width: 900px;
    padding-bottom: 40px;
  }

  .feature-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
    gap: 16px;
  }

  /* Connect info */
  .connect {
    max-width: 680px;
    width: 100%;
    text-align: center;
    font-size: 13px;
    color: var(--color-status-text);
    line-height: 1.6;
    padding-top: 8px;
    border-top: 1px solid var(--color-border);
  }

  .connect :global(p) {
    margin: 0 0 8px;
  }

  .connect :global(code) {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: 3px;
    padding: 1px 5px;
    font-family: inherit;
    font-size: 12px;
  }
</style>
