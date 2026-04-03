<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { setPlayerAuth, setCharacterSession } from '$lib/stores/authStore';
  import { goto } from '$app/navigation';
  import MarkdownContent from '$lib/components/MarkdownContent.svelte';
  import { Button } from '$lib/components/ui/button';
  import * as Card from '$lib/components/ui/card';
  import type { ContentItem } from '$lib/stores/contentStore';

  let { data }: { data: { hero?: ContentItem; pitch?: ContentItem; features?: ContentItem[]; connectInfo?: ContentItem } } = $props();

  const hero = $derived(data.hero);
  const pitch = $derived(data.pitch);
  const features = $derived(data.features ?? []);
  const connectInfo = $derived(data.connectInfo);

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
      const resp = await client.webCreateGuest({});
      if (resp.success) {
        setPlayerAuth('Guest');
        const charId = resp.defaultCharacterId || resp.characters[0]?.characterId;
        if (charId) {
          const selectResp = await client.webSelectCharacter({ characterId: charId });
          if (selectResp.success) {
            setCharacterSession(selectResp.sessionId, selectResp.characterName);
            goto('/terminal');
            return;
          }
        }
        goto('/characters');
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

<div class="flex flex-col items-center min-h-[calc(100vh-36px)] px-6 pb-12" data-testid="landing">
  <!-- Hero -->
  <section class="flex flex-col items-center justify-center gap-4 py-16 pb-12" data-testid="hero">
    <h1 class="text-[38px] font-bold tracking-wider text-primary" data-testid="hero-title">{heroTitle}</h1>
    <p class="text-[15px] text-muted-foreground" data-testid="hero-tagline">{heroTagline}</p>

    {#if error}
      <p class="text-sm text-destructive" data-testid="hero-error">{error}</p>
    {/if}

    <div class="flex gap-3 mt-2 flex-wrap justify-center" data-testid="hero-actions">
      <Button href="/login" data-testid="login-link">Login</Button>
      <Button variant="outline" href="/register" data-testid="register-link">Register</Button>
      <Button variant="ghost" onclick={handleGuest} disabled={loading} data-testid="guest-button">
        {loading ? 'Connecting…' : 'Try as Guest'}
      </Button>
    </div>
  </section>

  {#if hasContent}
    <!-- Pitch -->
    {#if pitch}
      <section class="max-w-[680px] w-full text-center text-sm leading-relaxed pb-10 border-b border-border mb-10" data-testid="pitch">
        <MarkdownContent content={pitch.body} />
      </section>
    {/if}

    <!-- Feature grid -->
    {#if features.length > 0}
      <section class="w-full max-w-[900px] pb-10" data-testid="features">
        <div class="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-4" data-testid="feature-grid">
          {#each features as feature (feature.key)}
            <Card.Root>
              <Card.Header>
                <Card.Title>{feature.metadata?.title ?? feature.key}</Card.Title>
              </Card.Header>
              <Card.Content>
                <MarkdownContent content={feature.body} />
              </Card.Content>
            </Card.Root>
          {/each}
        </div>
      </section>
    {/if}

    <!-- Connect info -->
    {#if connectInfo}
      <section class="max-w-[680px] w-full text-center text-sm text-muted-foreground leading-relaxed pt-2 border-t border-border" data-testid="connect">
        <MarkdownContent content={connectInfo.body} />
      </section>
    {/if}
  {/if}
</div>
