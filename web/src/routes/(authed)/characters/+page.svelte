<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import type { CharacterSummary } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { setCharacterSession } from '$lib/stores/authStore';
  import { setDefaultCharacter } from '$lib/characters/client';
  import { goto } from '$app/navigation';
  import * as Card from '$lib/components/ui/card';
  import { Badge } from '$lib/components/ui/badge';
  import { Button } from '$lib/components/ui/button';

  const client = createClient(WebService, transport);

  let { data } = $props();

  let characters = $state<CharacterSummary[]>([]);
  let loading = $state(true);
  let error = $state('');

  // The INITIAL default comes from the (authed) layout's webCheckSession call,
  // which already ran — OwnCharacter carries no is-default flag, so there is no
  // per-character source for it and no reason to spend a third round trip.
  // untrack: this is the INITIAL value, and the local state diverges from it the
  // moment a write lands. Reading `data` reactively here would clobber the
  // player's own change on the next layout-data invalidation.
  let defaultCharacterId = $state(untrack(() => data?.defaultCharacterId ?? ''));
  // The id whose request is in flight, or '' when none is.
  let savingDefaultId = $state('');
  let defaultStatus = $state('');
  let defaultError = $state('');

  async function makeDefault(char: CharacterSummary) {
    savingDefaultId = char.characterId;
    defaultError = '';
    try {
      // A GUI button is a machine-initiated structural write, so it takes the
      // typed RPC. The human text-command path is for conversational verbs a
      // player types, and must not be string-built into from here
      // (.claude/rules/gateway-boundary.md).
      const roster = await setDefaultCharacter(char.characterId);
      // The success status makes the requested id the default; the returned
      // roster is server truth for MEMBERSHIP and order. The cards keep their
      // locally-read session overlay because OwnCharacter carries none — the
      // sectioned rewrite that joins both reads is plan 05-08's.
      defaultCharacterId = char.characterId;
      const byId = new Map(characters.map((c) => [c.characterId, c]));
      characters = roster
        .map((own) => byId.get(own.id))
        .filter((c): c is CharacterSummary => c !== undefined);
      defaultStatus = `${char.characterName} is now your default character.`;
    } catch {
      defaultStatus = '';
      defaultError = "Couldn't save. Try again.";
    } finally {
      savingDefaultId = '';
    }
  }

  onMount(async () => {
    try {
      const resp = await client.webListCharacters({});
      characters = [...resp.characters];
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load characters.';
    } finally {
      loading = false;
    }
  });

  async function selectCharacter(charId: string) {
    try {
      const resp = await client.webSelectCharacter({
        characterId: charId,
      });
      if (resp.success) {
        setCharacterSession(resp.sessionId, resp.characterName);
        goto('/terminal');
      } else {
        error = resp.errorMessage || 'Failed to select character.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to select character.';
    }
  }

  function formatDate(timestamp: bigint): string {
    if (!timestamp) return 'Never';
    return new Date(Number(timestamp) * 1000).toLocaleDateString();
  }
</script>

<div class="min-h-[calc(100vh-36px)] flex items-start justify-center p-10">
  <div class="w-full max-w-[720px]">
    <h1 class="text-xl font-semibold text-primary mb-6">Choose Your Character</h1>

    {#if !loading && characters.length === 0}
      <p class="text-xs text-muted-foreground text-center -mt-4 mb-4 leading-relaxed">
        Now pick a name and step into the world. This is your in-character
        identity — who people will see and interact with. You can always
        come back and create more characters later.
      </p>
    {/if}

    {#if error}
      <p class="rounded-md border border-destructive bg-destructive/10 p-3 text-xs text-destructive mb-4">{error}</p>
    {/if}

    <!-- Persistent regions, always in the DOM so a screen reader announces the
         change rather than the region's arrival. -->
    <p role="status" class="text-xs text-muted-foreground mb-4" class:sr-only={!defaultStatus}>
      {defaultStatus}
    </p>
    <p role="alert" class="text-xs text-destructive mb-4" class:sr-only={!defaultError}>
      {defaultError}
    </p>

    {#if loading}
      <p class="text-xs text-muted-foreground">Loading characters…</p>
    {:else}
      <div class="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-3">
        {#each characters as char (char.characterId)}
          <Card.Root
            class="cursor-pointer hover:border-primary transition-colors"
            onclick={() => selectCharacter(char.characterId)}
          >
            <Card.Content class="flex items-start gap-3 px-4 py-4">
              <div class="w-11 h-11 bg-primary text-primary-foreground rounded-md flex items-center justify-center text-xl font-bold shrink-0">
                {char.characterName.charAt(0).toUpperCase()}
              </div>
              <div class="flex flex-col gap-0.5 min-w-0">
                <span class="text-sm font-semibold" data-testid="char-name">{char.characterName}</span>
                <span class="text-xs text-muted-foreground">Last played: {formatDate(char.lastPlayedAt)}</span>
                {#if char.lastLocation && !/^[0-9A-Z]{26}$/.test(char.lastLocation)}
                  <span class="text-xs text-muted-foreground">At: {char.lastLocation}</span>
                {/if}
                {#if char.hasActiveSession}
                  <Badge class="text-[10px] w-fit mt-0.5">Active</Badge>
                {:else}
                  <Badge variant="outline" class="text-[10px] w-fit mt-0.5">{char.sessionStatus || 'Offline'}</Badge>
                {/if}
                {#if char.characterId === defaultCharacterId}
                  <Badge
                    variant="outline"
                    class="text-[10px] w-fit mt-0.5 border-primary text-primary"
                    data-testid="default-badge">Default</Badge
                  >
                {:else}
                  <!-- stopPropagation: the card itself selects a character, and
                       setting a default must not also drop the player into the
                       game. The label does NOT change while in flight (UI-SPEC
                       Loading states); disabled + aria-busy carry that state. -->
                  <Button
                    variant="outline"
                    size="sm"
                    type="button"
                    name="makeDefault"
                    class="text-[10px] h-6 px-2 w-fit mt-1"
                    data-testid="make-default"
                    disabled={savingDefaultId === char.characterId}
                    aria-busy={savingDefaultId === char.characterId}
                    onclick={(e: MouseEvent) => {
                      e.stopPropagation();
                      makeDefault(char);
                    }}>Make default</Button
                  >
                {/if}
              </div>
            </Card.Content>
          </Card.Root>
        {/each}

        <!-- The create card is a LINK, not an inline form (D-87). The identity
             card IDENT-01 collects does not fit a roster tile, and the inline
             form that used to live here read `resp.success` off a response
             shape that no longer exists — /characters/new owns the whole
             submission, including the D-88 echo of the name the server stored.
             The destination page lands in plan 05-06. -->
        <a href="/characters/new" class="contents" data-testid="create-character">
          <Card.Root class="cursor-pointer hover:border-primary transition-colors border-dashed h-full">
            <Card.Content class="flex items-center gap-3 px-4 py-4">
              <div class="w-11 h-11 bg-border text-muted-foreground rounded-md flex items-center justify-center text-xl shrink-0">
                +
              </div>
              <div class="flex flex-col gap-0.5 min-w-0">
                <span class="text-sm font-semibold">Create a character</span>
                <span class="text-xs text-muted-foreground">Names are reserved once taken.</span>
              </div>
            </Card.Content>
          </Card.Root>
        </a>
      </div>
    {/if}
  </div>
</div>
