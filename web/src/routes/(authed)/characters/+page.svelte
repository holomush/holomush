<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import type { CharacterSummary } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { authState, setCharacterSession } from '$lib/stores/authStore';
  import { goto } from '$app/navigation';
  import * as Card from '$lib/components/ui/card';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Button } from '$lib/components/ui/button';
  import { Checkbox } from '$lib/components/ui/checkbox';

  const client = createClient(WebService, transport);

  let characters = $state<CharacterSummary[]>([]);
  let loading = $state(true);
  let error = $state('');
  let creating = $state(false);
  let newCharName = $state('');
  let createError = $state('');
  let autoDefault = $state(false);

  onMount(async () => {
    if (!$authState.playerSessionToken) {
      goto('/login');
      return;
    }
    try {
      const resp = await client.webListCharacters({ playerSessionToken: $authState.playerSessionToken });
      characters = [...resp.characters];
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load characters.';
    } finally {
      loading = false;
    }
  });

  async function selectCharacter(charId: string) {
    if (!$authState.playerSessionToken) return;
    try {
      const resp = await client.webSelectCharacter({
        playerSessionToken: $authState.playerSessionToken,
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

  async function createCharacter() {
    if (!newCharName.trim()) {
      createError = 'Character name is required.';
      return;
    }
    if (!$authState.playerSessionToken) return;
    createError = '';
    try {
      const resp = await client.webCreateCharacter({
        playerSessionToken: $authState.playerSessionToken,
        characterName: newCharName.trim(),
      });
      if (resp.success) {
        if (autoDefault) {
          // Create a real game session via SelectCharacter before entering terminal.
          const selectResp = await client.webSelectCharacter({
            playerSessionToken: $authState.playerSessionToken ?? '',
            characterId: resp.characterId,
          });
          if (selectResp.success) {
            setCharacterSession(selectResp.sessionId, selectResp.characterName);
            goto('/terminal');
          } else {
            createError = selectResp.errorMessage || 'Failed to enter game.';
          }
        } else {
          // Refresh the character list
          const listResp = await client.webListCharacters({ playerSessionToken: $authState.playerSessionToken });
          characters = [...listResp.characters];
          creating = false;
          newCharName = '';
        }
      } else {
        createError = resp.errorMessage || 'Failed to create character.';
      }
    } catch (e) {
      createError = e instanceof Error ? e.message : 'Failed to create character.';
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
              </div>
            </Card.Content>
          </Card.Root>
        {/each}

        {#if !creating}
          <Card.Root
            class="cursor-pointer hover:border-primary transition-colors border-dashed"
            onclick={() => (creating = true)}
          >
            <Card.Content class="flex items-center gap-3 px-4 py-4">
              <div class="w-11 h-11 bg-border text-muted-foreground rounded-md flex items-center justify-center text-xl shrink-0">
                +
              </div>
              <span class="text-sm font-semibold">Create New Character</span>
            </Card.Content>
          </Card.Root>
        {:else}
          <Card.Root class="border-dashed">
            <Card.Content class="flex flex-col gap-2 px-4 py-4">
              <div class="flex items-center gap-3">
                <div class="w-11 h-11 bg-border text-muted-foreground rounded-md flex items-center justify-center text-xl shrink-0">
                  +
                </div>
                <span class="text-sm font-semibold">New Character</span>
              </div>
              {#if createError}
                <p class="text-xs text-destructive">{createError}</p>
              {/if}
              <Input
                type="text"
                name="characterName"
                bind:value={newCharName}
                placeholder="Character name"
                class="text-xs h-8"
                onkeydown={(e) => e.key === 'Enter' && createCharacter()}
              />
              <label class="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                <Checkbox bind:checked={autoDefault} name="autoDefault" />
                <span>Enter game immediately</span>
              </label>
              <div class="flex gap-1.5">
                <Button size="sm" class="text-xs h-7 px-3" onclick={createCharacter}>Create</Button>
                <Button
                  variant="outline"
                  size="sm"
                  class="text-xs h-7 px-3"
                  onclick={() => {
                    creating = false;
                    newCharName = '';
                    createError = '';
                  }}>Cancel</Button
                >
              </div>
            </Card.Content>
          </Card.Root>
        {/if}
      </div>
    {/if}
  </div>
</div>
