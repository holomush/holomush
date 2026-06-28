<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { cn } from '$lib/utils.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import {
    loadSceneSettings,
    settingsMask,
    saveSceneSettings,
    type SceneSettings,
  } from '$lib/scenes/settingsFlow';

  let { sceneId, characterId, onDone }: {
    sceneId: string;
    characterId: string;
    onDone?: () => void;
  } = $props();

  let loading = $state(true);
  let submitting = $state(false);
  let errorMsg = $state('');
  let orig = $state<SceneSettings | null>(null);

  // Editable fields.
  let title = $state('');
  let description = $state('');
  let visibility = $state('open');
  let poseOrderMode = $state('free');
  let tags = $state<string[]>([]);
  let contentWarnings = $state<string[]>([]);
  let tagDraft = $state('');
  let cwDraft = $state('');

  let next = $derived<SceneSettings>({
    title: title.trim(),
    description,
    visibility,
    poseOrderMode,
    tags,
    contentWarnings,
  });
  let mask = $derived(orig ? settingsMask(orig, next) : []);
  let dirty = $derived(mask.length > 0);

  $effect(() => {
    let cancelled = false;
    loading = true;
    loadSceneSettings(characterId, sceneId)
      .then((s) => {
        if (cancelled) return;
        orig = s;
        title = s.title;
        description = s.description;
        visibility = s.visibility || 'open';
        poseOrderMode = s.poseOrderMode || 'free';
        tags = [...s.tags];
        contentWarnings = [...s.contentWarnings];
      })
      .catch((e) => { if (!cancelled) errorMsg = e instanceof Error ? e.message : 'Load failed'; })
      .finally(() => { if (!cancelled) loading = false; });
    return () => { cancelled = true; };
  });

  function addToken(list: string[], draft: string): [string[], string] {
    const t = draft.trim();
    if (!t || list.includes(t) || list.length >= 32) return [list, ''];
    return [[...list, t], ''];
  }

  async function save() {
    if (!orig || submitting || !dirty) return;
    submitting = true;
    errorMsg = '';
    try {
      await saveSceneSettings({ characterId, sceneId, orig, next });
      onDone?.();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : 'Update failed';
    } finally {
      submitting = false;
    }
  }
</script>

{#if loading}
  <p class="px-4 py-3 text-sm text-muted-foreground">Loading settings…</p>
{:else}
  <form class="flex flex-col gap-3 px-4 py-3" onsubmit={(e) => { e.preventDefault(); save(); }}>
    <div class="flex flex-col gap-1">
      <Label for="settings-title">Title</Label>
      <Input id="settings-title" name="settings-title" bind:value={title} disabled={submitting} />
    </div>

    <div class="flex flex-col gap-1">
      <Label for="settings-desc">Description</Label>
      <textarea
        id="settings-desc" name="settings-desc" bind:value={description} disabled={submitting} rows={3}
        class={cn('w-full min-h-[72px] resize-y rounded-md border border-input bg-background px-3 py-2',
          'text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
          'disabled:opacity-50')}
      ></textarea>
    </div>

    <div class="flex flex-col gap-1">
      <Label for="settings-visibility">Visibility</Label>
      <select id="settings-visibility" name="settings-visibility" bind:value={visibility} disabled={submitting}
        class="rounded-md border border-input bg-background px-3 py-2 text-sm">
        <option value="open">Open — listed, any character may join</option>
        <option value="private">Private — unlisted, invitation required</option>
      </select>
    </div>

    <div class="flex flex-col gap-1">
      <Label for="settings-pose-order">Pose order</Label>
      <select id="settings-pose-order" name="settings-pose-order" bind:value={poseOrderMode} disabled={submitting}
        class="rounded-md border border-input bg-background px-3 py-2 text-sm">
        <option value="free">Free</option>
        <option value="strict">Strict</option>
        <option value="3pr">3 per round</option>
        <option value="5pr">5 per round</option>
      </select>
    </div>

    <div class="flex flex-col gap-1">
      <Label for="settings-tag-draft">Tags</Label>
      <div class="flex flex-wrap gap-1">
        {#each tags as tag (tag)}
          <Badge variant="outline" class="text-[10px] h-5 gap-1">
            {tag}
            <button type="button" aria-label={`Remove tag ${tag}`} onclick={() => (tags = tags.filter((x) => x !== tag))}>×</button>
          </Badge>
        {/each}
      </div>
      <Input id="settings-tag-draft" name="settings-tag-draft" bind:value={tagDraft} disabled={submitting}
        placeholder="Add a tag, Enter"
        onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); [tags, tagDraft] = addToken(tags, tagDraft); } }} />
    </div>

    <div class="flex flex-col gap-1">
      <Label for="settings-cw-draft">Content warnings</Label>
      <div class="flex flex-wrap gap-1">
        {#each contentWarnings as cw (cw)}
          <Badge variant="outline" class="text-[10px] h-5 gap-1">
            {cw}
            <button type="button" aria-label={`Remove warning ${cw}`} onclick={() => (contentWarnings = contentWarnings.filter((x) => x !== cw))}>×</button>
          </Badge>
        {/each}
      </div>
      <Input id="settings-cw-draft" name="settings-cw-draft" bind:value={cwDraft} disabled={submitting}
        placeholder="Add a warning, Enter"
        onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); [contentWarnings, cwDraft] = addToken(contentWarnings, cwDraft); } }} />
    </div>

    <div class="flex items-center justify-end gap-2">
      <Button type="button" variant="ghost" size="sm" onclick={() => onDone?.()} disabled={submitting}>Cancel</Button>
      <Button type="submit" variant="default" size="sm" aria-label="Save settings" disabled={submitting || !dirty}>
        {submitting ? 'Saving…' : 'Save changes'}
      </Button>
    </div>

    {#if errorMsg}<p class="text-xs text-destructive">{errorMsg}</p>{/if}
  </form>
{/if}
