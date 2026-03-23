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
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import { goto } from '$app/navigation';

  const client = createClient(WebService, transport);

  let characters = $state<CharacterSummary[]>([]);
  let loading = $state(true);
  let error = $state('');
  let creating = $state(false);
  let newCharName = $state('');
  let createError = $state('');
  let autoDefault = $state(false);

  onMount(async () => {
    if (!$authState.playerToken) {
      goto('/login');
      return;
    }
    try {
      const resp = await client.webListCharacters({ playerToken: $authState.playerToken });
      characters = [...resp.characters];
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load characters.';
    } finally {
      loading = false;
    }
  });

  async function selectCharacter(charId: string) {
    if (!$authState.playerToken) return;
    try {
      const resp = await client.webSelectCharacter({
        playerToken: $authState.playerToken,
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
    if (!$authState.playerToken) return;
    createError = '';
    try {
      const resp = await client.webCreateCharacter({
        playerToken: $authState.playerToken,
        characterName: newCharName.trim(),
      });
      if (resp.success) {
        if (autoDefault) {
          // Create a real game session via SelectCharacter before entering terminal.
          const selectResp = await client.webSelectCharacter({
            playerToken: $authState.playerToken ?? '',
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
          const listResp = await client.webListCharacters({ playerToken: $authState.playerToken });
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

<div class="page" style={themeToCssVars($activeTheme.colors)}>
  <div class="container">
    <h1 class="title">Choose Your Character</h1>

    {#if error}
      <p class="error">{error}</p>
    {/if}

    {#if loading}
      <p class="loading">Loading characters…</p>
    {:else}
      <div class="grid">
        {#each characters as char (char.characterId)}
          <button class="char-card" onclick={() => selectCharacter(char.characterId)}>
            <div class="char-icon">{char.characterName.charAt(0).toUpperCase()}</div>
            <div class="char-info">
              <span class="char-name">{char.characterName}</span>
              <span class="char-meta">Last played: {formatDate(char.lastPlayedAt)}</span>
              {#if char.lastLocation}
                <span class="char-meta">At: {char.lastLocation}</span>
              {/if}
              {#if char.hasActiveSession}
                <span class="status-badge active">Active</span>
              {:else}
                <span class="status-badge offline">{char.sessionStatus || 'Offline'}</span>
              {/if}
            </div>
          </button>
        {/each}

        {#if !creating}
          <button class="char-card new-char" onclick={() => (creating = true)}>
            <div class="char-icon new">+</div>
            <span class="char-name">Create New Character</span>
          </button>
        {:else}
          <div class="char-card create-form">
            <div class="char-icon new">+</div>
            <div class="create-fields">
              {#if createError}
                <p class="create-error">{createError}</p>
              {/if}
              <input
                type="text"
                name="characterName"
                bind:value={newCharName}
                placeholder="Character name"
                onkeydown={(e) => e.key === 'Enter' && createCharacter()}
              />
              <label class="checkbox-label">
                <input type="checkbox" bind:checked={autoDefault} />
                <span>Enter game immediately</span>
              </label>
              <div class="create-actions">
                <button class="btn-sm btn-primary" onclick={createCharacter}>Create</button>
                <button
                  class="btn-sm btn-ghost"
                  onclick={() => {
                    creating = false;
                    newCharName = '';
                    createError = '';
                  }}>Cancel</button
                >
              </div>
            </div>
          </div>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .page {
    min-height: calc(100vh - 32px);
    background: var(--color-background);
    font-family: 'JetBrains Mono', monospace;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding: 40px 16px;
  }

  .container {
    width: 100%;
    max-width: 720px;
  }

  .title {
    font-size: 20px;
    color: var(--color-say-speaker);
    margin: 0 0 24px;
    font-weight: 600;
  }

  .error {
    background: rgba(229, 115, 115, 0.1);
    border: 1px solid var(--color-command-error);
    border-radius: 4px;
    color: var(--color-command-error);
    padding: 8px 12px;
    font-size: 12px;
    margin-bottom: 16px;
  }

  .loading {
    color: var(--color-status-text);
    font-size: 13px;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 12px;
  }

  .char-card {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    padding: 16px;
    display: flex;
    align-items: flex-start;
    gap: 12px;
    cursor: pointer;
    text-align: left;
    font-family: inherit;
    color: var(--color-input-text);
    transition: border-color 0.15s;
    width: 100%;
  }

  .char-card:hover {
    border-color: var(--color-say-speaker);
  }

  .new-char {
    border-style: dashed;
    align-items: center;
    justify-content: flex-start;
    color: var(--color-status-text);
  }

  .new-char:hover {
    color: var(--color-say-speaker);
  }

  .create-form {
    cursor: default;
    flex-direction: column;
    align-items: stretch;
    gap: 8px;
  }

  .create-form:hover {
    border-color: var(--color-say-speaker);
  }

  .char-icon {
    width: 44px;
    height: 44px;
    background: var(--color-say-speaker);
    color: var(--color-background);
    border-radius: 6px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 20px;
    font-weight: bold;
    flex-shrink: 0;
  }

  .char-icon.new {
    background: var(--color-border);
    color: var(--color-status-text);
  }

  .char-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .char-name {
    font-size: 14px;
    font-weight: 600;
    color: var(--color-input-text);
  }

  .char-meta {
    font-size: 11px;
    color: var(--color-status-text);
  }

  .status-badge {
    display: inline-block;
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 3px;
    margin-top: 2px;
  }

  .status-badge.active {
    background: rgba(129, 199, 132, 0.2);
    color: var(--color-pose-actor);
    border: 1px solid var(--color-pose-actor);
  }

  .status-badge.offline {
    background: transparent;
    color: var(--color-status-text);
    border: 1px solid var(--color-border);
  }

  .create-fields {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: 100%;
  }

  .create-error {
    font-size: 11px;
    color: var(--color-command-error);
    margin: 0;
  }

  .create-fields input[type='text'] {
    background: var(--color-input-background);
    border: 1px solid var(--color-border);
    border-radius: 4px;
    color: var(--color-input-text);
    font-family: inherit;
    font-size: 12px;
    padding: 6px 8px;
    outline: none;
    width: 100%;
    box-sizing: border-box;
  }

  .create-fields input[type='text']:focus {
    border-color: var(--color-say-speaker);
  }

  .checkbox-label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    color: var(--color-status-text);
    cursor: pointer;
  }

  .create-actions {
    display: flex;
    gap: 6px;
  }

  .btn-sm {
    padding: 4px 10px;
    border-radius: 3px;
    font-family: inherit;
    font-size: 11px;
    cursor: pointer;
  }

  .btn-primary {
    background: var(--color-say-speaker);
    color: var(--color-background);
    border: none;
    font-weight: 600;
  }

  .btn-ghost {
    background: transparent;
    border: 1px solid var(--color-border);
    color: var(--color-input-text);
  }
</style>
