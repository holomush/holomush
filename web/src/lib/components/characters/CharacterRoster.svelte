<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  /*
   * RED-PHASE PLACEHOLDER — the ordinary accordion shape.
   *
   * This is the version a developer reaching for a familiar "show/hide extra
   * items" pattern writes: the second section always renders, it starts
   * COLLAPSED, the collapse is a CSS class rather than a markup removal, and
   * the label pluralises unconditionally. Every one of those four is wrong for
   * this surface (D-96, UI-SPEC empty | roster).
   */
  import RosterCard from './RosterCard.svelte';

  type Row = {
    id: string;
    name: string;
    status: string;
    session?: { hasActiveSession: boolean; sessionStatus: string };
  };

  let {
    characters,
    defaultCharacterId = '',
    savingDefaultId = '',
    onselect = undefined,
    onmakedefault = undefined,
  }: {
    characters: Row[];
    defaultCharacterId?: string;
    savingDefaultId?: string;
    onselect?: (id: string) => void;
    onmakedefault?: (id: string) => void;
  } = $props();

  const playable = $derived(characters.filter((c) => c.status === 'active'));
  const notPlayable = $derived(characters.filter((c) => c.status !== 'active'));

  let collapsed = $state(true);
</script>

<section>
  <h2 class="section-heading">Playable</h2>
  {#if characters.length === 0}
    <p>No characters yet</p>
    <p>Create one to step into the world.</p>
  {:else if playable.length === 0}
    <p>Nothing playable right now.</p>
    <p>Create a character to get back in.</p>
  {/if}
  <div class="grid">
    {#each playable as c (c.id)}
      <RosterCard
        id={c.id}
        name={c.name}
        status={c.status}
        session={c.session}
        isDefault={c.id === defaultCharacterId}
        playable={true}
        savingDefault={savingDefaultId === c.id}
        {onselect}
        {onmakedefault}
      />
    {/each}
    <a href="/characters/new" class="create" data-testid="create-character">
      <span class="create-label">Create a character</span>
      <span class="create-sub">Names are reserved once taken.</span>
    </a>
  </div>
</section>

<section>
  <div class="section-head">
    <h2 class="section-heading">Not playable</h2>
    <button
      type="button"
      class="chip"
      data-testid="not-playable-toggle"
      aria-expanded={!collapsed}
      aria-controls="not-playable-grid"
      onclick={() => (collapsed = !collapsed)}>Show {notPlayable.length} characters</button
    >
  </div>
  <div id="not-playable-grid" data-testid="not-playable-grid" class="grid" class:is-collapsed={collapsed}>
    {#each notPlayable as c (c.id)}
      <RosterCard id={c.id} name={c.name} status={c.status} session={c.session} playable={false} />
    {/each}
  </div>
</section>

<style>
  .grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 12px;
  }
  .is-collapsed {
    display: none;
  }
  .section-heading {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--color-status-text);
  }
  .section-head {
    display: flex;
    justify-content: space-between;
  }
  .chip {
    border: 1px solid var(--color-border);
    border-radius: 8px;
    background: transparent;
    color: var(--color-status-text);
    font-size: 12px;
  }
  .create {
    display: flex;
    flex-direction: column;
    padding: 16px;
    border: 1px dashed var(--color-border);
    border-radius: 12px;
    text-decoration: none;
    color: var(--color-card-foreground);
  }
  .create-label {
    font-size: 14px;
    font-weight: 600;
  }
  .create-sub {
    font-size: 12px;
    color: var(--color-status-text);
  }
  @media (min-width: 768px) {
    .grid {
      grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    }
  }
</style>
