<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import RosterCard from './RosterCard.svelte';

  /**
   * The sectioned roster body. Props-only: it reads no store, makes no request
   * and owns no route state beyond the one piece of local view state the
   * collapse chip needs.
   *
   * SECTIONING IS WHAT DEFUSES SKETCH 008'S SHARP EDGE. A retired character can
   * no longer float to the top of an ordering, because it is in the other
   * section — which is why this component asserts nothing about the order
   * within `Playable` and simply renders what the roster call returned.
   *
   * THE SECOND SECTION IS OMITTED ENTIRELY WHEN ITS SET IS EMPTY. A heading
   * with no body is a count of one, and it is still announced. The section is
   * not merely styled away — it is not rendered.
   */
  type Row = {
    id: string;
    name: string;
    /** characters.status — exactly `active`, `retired` or `idle` (01-SPEC §4.2). */
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

  /*
   * `active` is the ONE playable lifecycle; everything else — `retired`,
   * `idle`, and any value this client does not recognise — is not playable.
   * The partition is deliberately expressed that way round rather than as a
   * list of not-playable values, so a fourth status the server one day adds
   * lands in the section that grants nothing.
   */
  const playable = $derived(characters.filter((c) => c.status === 'active'));
  const notPlayable = $derived(characters.filter((c) => c.status !== 'active'));

  /*
   * EXPANDED on first paint (D-96): these are the player's own characters, and
   * a default that hides them presumes they are clutter. The chip is therefore
   * a COLLAPSE control, and its label may not carry a word that presumes the
   * opposite default.
   */
  let expanded = $state(true);

  const NOT_PLAYABLE_GRID_ID = 'roster-not-playable';

  const chipLabel = $derived.by(() => {
    const n = notPlayable.length;
    const noun = n === 1 ? 'character' : 'characters';
    return `${expanded ? 'Hide' : 'Show'} ${n} ${noun}`;
  });
</script>

<section class="section">
  {#if characters.length === 0}
    <h2 class="section-heading">No characters yet</h2>
    <p class="section-body">Create one to step into the world.</p>
  {:else}
    <h2 class="section-heading">Playable</h2>
    {#if playable.length === 0}
      <!--
        008-B's degradation property: the section says IN WORDS that nothing
        can be played, rather than leaving a grid of dead cards to be inferred
        from.
      -->
      <p class="section-body">Nothing playable right now.</p>
      <p class="section-body">Create a character to get back in.</p>
    {/if}
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

    <!--
      The create affordance is a LINK to /characters/new (D-87), never an
      inline input: the identity card IDENT-01 collects does not fit a roster
      tile. The sub-line says RESERVED, which is the property that holds — a
      retired character keeps its name. The stronger claim a reader reaches for
      first is false, and this file's acceptance gate scans the whole file
      rather than only its markup on purpose: a gate that has to be suppressed
      to stay green stops being a gate, so a comment may not be what trips it.
    -->
    <a href="/characters/new" class="card create" data-testid="create-character">
      <span class="create-plate" aria-hidden="true">+</span>
      <span class="create-body">
        <span class="create-label">Create a character</span>
        <span class="create-sub">Names are reserved once taken.</span>
      </span>
    </a>
  </div>
</section>

{#if notPlayable.length > 0}
  <section class="section">
    <div class="section-head">
      <h2 class="section-heading">Not playable</h2>
      <button
        type="button"
        class="chip"
        data-testid="not-playable-toggle"
        aria-expanded={expanded}
        aria-controls={NOT_PLAYABLE_GRID_ID}
        onclick={() => (expanded = !expanded)}>{chipLabel}</button
      >
    </div>
    {#if expanded}
      <!--
        Removed from the markup rather than hidden: a display:none grid is
        still a grid the chip's aria-expanded="false" would be lying about,
        and collapsing is meant to take the cards OUT of the flow rather than
        park a scroll region beneath the heading.
      -->
      <div id={NOT_PLAYABLE_GRID_ID} data-testid="not-playable-grid" class="grid">
        {#each notPlayable as c (c.id)}
          <RosterCard id={c.id} name={c.name} status={c.status} session={c.session} playable={false} />
        {/each}
      </div>
    {/if}
  </section>
{/if}

<style>
  .section {
    margin-top: 32px;
  }
  .section:first-of-type {
    margin-top: 0;
  }
  .section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .section-heading {
    margin: 0 0 8px;
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--color-status-text);
  }
  .section-body {
    margin: 0 0 8px;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
    color: var(--color-status-text);
  }
  .chip {
    display: inline-flex;
    align-items: center;
    /* The interactive-target floor, padding included. */
    min-height: 44px;
    margin-bottom: 8px;
    padding: 4px 12px;
    border: 1px solid var(--color-border);
    border-radius: 999px;
    background: transparent;
    color: var(--color-status-text);
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    cursor: pointer;
  }
  .chip:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
  /*
   * Single column below 768px; the multi-column rule is the one that is
   * conditional, so the narrow band needs no override.
   */
  .grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 12px;
  }
  .card {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 44px;
    padding: 16px;
    border-radius: 12px;
  }
  .create {
    border: 1px dashed var(--color-border);
    background: var(--color-card);
    color: var(--color-card-foreground);
    text-decoration: none;
  }
  .create:hover {
    border-color: var(--color-primary);
  }
  .create:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
  .create-plate {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 44px;
    height: 44px;
    border: 1px dashed var(--color-border);
    border-radius: 12px;
    font-size: 20px;
    line-height: 1;
    color: var(--color-status-text);
  }
  .create-body {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .create-label {
    font-size: 14px;
    font-weight: 600;
    line-height: 1.5;
  }
  .create-sub {
    font-size: 12px;
    font-weight: 400;
    line-height: 1.4;
    color: var(--color-status-text);
  }
  @media (min-width: 768px) {
    .grid {
      grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    }
  }
</style>
