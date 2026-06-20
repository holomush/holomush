<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { cn } from '$lib/utils.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { submitCreateScene } from '$lib/scenes/createFlow';

  let {
    characters = [],
    onDone,
  }: {
    characters?: { characterId: string; name?: string; characterName?: string }[];
    onDone?: () => void;
  } = $props();

  let title = $state('');
  let description = $state('');
  let actingCharacterId = $state(characters[0]?.characterId ?? '');
  let submitting = $state(false);
  let errorMsg = $state('');

  function charLabel(c: { characterId: string; name?: string; characterName?: string }): string {
    return c.characterName ?? c.name ?? c.characterId;
  }

  async function create() {
    const t = title.trim();
    if (!t || submitting) return;
    submitting = true;
    errorMsg = '';
    try {
      await submitCreateScene({
        characterId: actingCharacterId,
        title: t,
        description: description.trim(),
        characters,
      });
      onDone?.();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : 'Create failed';
    } finally {
      submitting = false;
    }
  }
</script>

<form class="flex flex-col gap-3 px-4 py-3" onsubmit={(e) => { e.preventDefault(); create(); }}>
  {#if characters.length > 1}
    <div class="flex flex-col gap-1">
      <Label for="create-scene-as">Create as</Label>
      <select
        id="create-scene-as"
        aria-label="Create scene as"
        bind:value={actingCharacterId}
        class="rounded-md border border-input bg-background px-3 py-2 text-sm"
      >
        {#each characters as c (c.characterId)}
          <option value={c.characterId}>{charLabel(c)}</option>
        {/each}
      </select>
    </div>
  {/if}

  <div class="flex flex-col gap-1">
    <Label for="create-scene-title">Title</Label>
    <Input id="create-scene-title" bind:value={title} placeholder="Scene title…" disabled={submitting} />
  </div>

  <div class="flex flex-col gap-1">
    <Label for="create-scene-desc">Description <span class="text-muted-foreground">(optional)</span></Label>
    <textarea
      id="create-scene-desc"
      bind:value={description}
      disabled={submitting}
      rows={3}
      placeholder="What's this scene about?"
      class={cn(
        'w-full min-h-[72px] resize-y rounded-md border border-input bg-background px-3 py-2',
        'text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
        'disabled:opacity-50',
      )}
    ></textarea>
  </div>

  <div class="flex items-center justify-end gap-2">
    <Button type="button" variant="ghost" size="sm" onclick={() => onDone?.()} disabled={submitting}>
      Cancel
    </Button>
    <Button type="submit" variant="default" size="sm" aria-label="Create scene" disabled={submitting || !title.trim()}>
      {submitting ? 'Creating…' : 'Create scene'}
    </Button>
  </div>

  {#if errorMsg}
    <p class="text-xs text-destructive">{errorMsg}</p>
  {/if}
</form>
