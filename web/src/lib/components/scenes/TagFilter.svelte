<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { Badge } from '$lib/components/ui/badge';

  /**
   * TagFilter renders a search-by-title input and a set of tag-chip toggles.
   * - titleQuery: client-side filter on scene titles (two-way bound).
   * - activeTags: set of tag strings that drive the server-side tags filter.
   * - availableTags: full list of tags to offer as chips.
   * - onTagsChange: fired when the selected tag set changes (triggers server refetch).
   */
  let {
    titleQuery = $bindable(''),
    activeTags = $bindable<string[]>([]),
    availableTags = [],
    onTagsChange,
  }: {
    titleQuery?: string;
    activeTags?: string[];
    availableTags?: string[];
    onTagsChange?: (tags: string[]) => void;
  } = $props();

  function toggleTag(tag: string) {
    const next = activeTags.includes(tag)
      ? activeTags.filter((t) => t !== tag)
      : [...activeTags, tag];
    activeTags = next;
    onTagsChange?.(next);
  }
</script>

<div class="flex flex-col gap-2 px-4 py-3 border-b border-border">
  <input
    type="search"
    name="scene-search"
    placeholder="Search scenes…"
    bind:value={titleQuery}
    aria-label="Filter scenes by title"
    class="w-full rounded border border-input bg-background px-3 py-1.5 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
  />
  {#if availableTags.length > 0}
    <div class="flex flex-wrap gap-1.5" role="group" aria-label="Filter by tag">
      {#each availableTags as tag (tag)}
        <button
          onclick={() => toggleTag(tag)}
          aria-pressed={activeTags.includes(tag)}
          class="cursor-pointer"
        >
          <Badge
            variant={activeTags.includes(tag) ? 'default' : 'outline'}
            class="text-[11px] py-0 px-2 cursor-pointer"
          >
            {tag}
          </Badge>
        </button>
      {/each}
    </div>
  {/if}
</div>
