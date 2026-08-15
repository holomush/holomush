<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import * as Empty from '$lib/components/ui/empty';
  import * as Pagination from '$lib/components/ui/pagination';
  import CharacterFilterBar from '$lib/components/admin/CharacterFilterBar.svelte';
  import CharacterTable from '$lib/components/admin/CharacterTable.svelte';
  import {
    ADMIN_PAGE_SIZE,
    listAdminCharacters,
    searchAdminCharacters,
    type CharacterRow,
    type CharacterSortField,
    type CharacterStatusFilter,
  } from '$lib/admin/client';

  /**
   * The character-administration surface: filter bar, table, pagination, and
   * the four states the list can be in.
   *
   * FAILURE RESOLVES INTO A SMALL AUTHORED UNION and never into a server
   * string. No internal code, no wrapped error text, no message field reaches
   * the screen — an error carries an operator-facing meaning only after this
   * page has assigned it one.
   */
  let { data }: { data: { rows: CharacterRow[]; totalCount: bigint; loadFailed: boolean } } =
    $props();

  // untrack: these are the INITIAL values from the load. Local state diverges
  // the moment the operator sorts, filters or turns a page, and reading `data`
  // reactively here would clobber that on the next layout-data invalidation.
  let rows = $state<CharacterRow[]>(untrack(() => data.rows));
  let totalCount = $state<bigint>(untrack(() => data.totalCount));

  let loading = $state(false);
  /**
   * The whole failure vocabulary of this page. Two authored members and the
   * absence of one; nothing derived from what arrived on the wire.
   */
  let failure = $state<'' | 'load' | 'search'>(untrack(() => (data.loadFailed ? 'load' : '')));

  let term = $state('');
  let status = $state<CharacterStatusFilter>('all');
  let playerId = $state('');
  let sortField = $state<CharacterSortField>('name');
  let descending = $state(false);
  let pageNum = $state(1);

  /**
   * The one row whose mutation is in flight. Nothing on this page writes yet —
   * the edit Sheet and the lifecycle confirm are plan 06.1-04's — so it stays
   * empty here and the table's per-row pending idiom is already wired for it.
   */
  let pendingRowId = $state('');
  /**
   * The character the operator reached for, and through which entry point.
   *
   * Both row affordances resolve here and no further IN THIS PLAN: the edit
   * Sheet and the lifecycle confirm are plan 06.1-04's artifacts and do not
   * exist yet, so activating a row records the intent and renders nothing. The
   * seam is deliberate — 06.1-04 adds the overlay and reads this — and it is
   * recorded as a known stub rather than left to be discovered.
   */
  let selected = $state<{ id: string; intent: 'edit' | 'lifecycle' } | null>(null);

  function onedit(id: string) {
    selected = { id, intent: 'edit' };
  }

  function onlifecycle(row: CharacterRow) {
    selected = { id: row.id, intent: 'lifecycle' };
  }

  /**
   * Emptiness by a first-element probe. The total on this surface is the
   * server's own scalar COUNT over the filter, and nothing here may compute a
   * figure of its own from what came back — the two would disagree on the last
   * page and on any request the core clamped.
   */
  const noRows = $derived(rows[0] === undefined);
  const filtered = $derived(term.trim() !== '' || status !== 'all' || playerId !== '');
  const searching = $derived(term.trim() !== '');

  const total = $derived(Number(totalCount));
  const manyPages = $derived(total > ADMIN_PAGE_SIZE);
  const rangeStart = $derived((pageNum - 1) * ADMIN_PAGE_SIZE + 1);
  /** The end is clamped to the total, not to a full page width. */
  const rangeEnd = $derived(Math.min(pageNum * ADMIN_PAGE_SIZE, total));

  async function reload() {
    loading = true;
    const query = { sortField, descending, status, playerId, page: pageNum };
    const wanted = term;
    try {
      const page = wanted.trim() === ''
        ? await listAdminCharacters(query)
        : await searchAdminCharacters(query, wanted);
      rows = page.rows;
      totalCount = page.totalCount;
      failure = '';
    } catch {
      // The caught value is deliberately not bound: there is nothing this page
      // may learn from it and nothing it may show. A search that fails and a
      // list that fails are the two states an operator can act on.
      rows = [];
      totalCount = 0n;
      failure = wanted.trim() === '' ? 'load' : 'search';
    } finally {
      loading = false;
    }
  }

  function onsearch(next: string) {
    term = next;
    pageNum = 1;
    void reload();
  }

  function onstatus(next: CharacterStatusFilter) {
    status = next;
    pageNum = 1;
    void reload();
  }

  function onsort(field: CharacterSortField) {
    if (sortField === field) descending = !descending;
    else {
      sortField = field;
      descending = false;
    }
    pageNum = 1;
    void reload();
  }

  /** Click-to-filter on the player column: an equality filter, never an ordering. */
  function onfilterplayer(id: string) {
    playerId = id;
    pageNum = 1;
    void reload();
  }

  function onPageChange(next: number) {
    pageNum = next;
    void reload();
  }

  function clearFilters() {
    term = '';
    status = 'all';
    playerId = '';
    pageNum = 1;
    void reload();
  }
</script>

<nav class="crumb" aria-label="Breadcrumb">Admin › Characters</nav>
<h1 class="sectiontitle">Characters</h1>

<CharacterFilterBar {term} bind:status {onsearch} {onstatus} />

{#if loading}
  <CharacterTable
    rows={[]}
    loading
    {sortField}
    {descending}
    {onsort}
    {onedit}
    {onlifecycle}
    {onfilterplayer}
  />
{:else if failure === 'load'}
  <Empty.Root data-failure="load" class="liststate">
    <Empty.Title>Couldn't load characters. Try again.</Empty.Title>
    <Empty.Content>
      <button type="button" class="stateaction" onclick={() => reload()}>Try again</button>
    </Empty.Content>
  </Empty.Root>
{:else if failure === 'search'}
  <Empty.Root data-failure="search" class="liststate">
    <Empty.Title>Couldn't run that search. Try again.</Empty.Title>
    <Empty.Content>
      <button type="button" class="stateaction" onclick={() => reload()}>Try again</button>
    </Empty.Content>
  </Empty.Root>
{:else if noRows && filtered}
  <!--
    A statement about THE FILTERS, not about existence. The client cannot tell a
    legitimate zero-row page from a search the server answered emptily, so it
    must not upgrade the render into a claim it has no grounds for.
  -->
  <Empty.Root data-empty-state="no-results" class="liststate">
    <Empty.Title>No characters match those filters.</Empty.Title>
    <Empty.Description>Try a different search or clear the status filter.</Empty.Description>
    <Empty.Content>
      <button type="button" class="stateaction" onclick={clearFilters}>Clear filters</button>
    </Empty.Content>
  </Empty.Root>
{:else if noRows}
  <!--
    A different state from the one above, with different copy and NO action: an
    admin cannot create a character for a player, so a call to action here would
    be an invented promise.
  -->
  <Empty.Root data-empty-state="zero" class="liststate">
    <Empty.Title>No characters yet.</Empty.Title>
    <Empty.Description>Characters appear here once players create them.</Empty.Description>
  </Empty.Root>
{:else}
  {#if manyPages}
    <p class="range">{rangeStart}–{rangeEnd} of {total}</p>
  {/if}
  <CharacterTable
    {rows}
    {sortField}
    {descending}
    {pendingRowId}
    {onsort}
    {onedit}
    {onlifecycle}
    {onfilterplayer}
  />
  {#if manyPages}
    <Pagination.Root count={total} perPage={ADMIN_PAGE_SIZE} page={pageNum} {onPageChange}>
      {#snippet children({ pages, currentPage })}
        <Pagination.Content>
          <Pagination.Item>
            <Pagination.PrevButton />
          </Pagination.Item>
          {#each pages as page (page.key)}
            {#if page.type === 'ellipsis'}
              <Pagination.Item><Pagination.Ellipsis /></Pagination.Item>
            {:else}
              <Pagination.Item>
                <Pagination.Link {page} isActive={currentPage === page.value}>
                  {page.value}
                </Pagination.Link>
              </Pagination.Item>
            {/if}
          {/each}
          <Pagination.Item>
            <Pagination.NextButton />
          </Pagination.Item>
        </Pagination.Content>
      {/snippet}
    </Pagination.Root>
  {/if}
{/if}

<style>
  .crumb {
    font-size: 12px;
    font-weight: 600;
    color: var(--color-status-text);
    padding: 0 0 4px;
  }
  .sectiontitle {
    font-size: 20px;
    font-weight: 600;
    line-height: 1.2;
    margin: 0 0 24px;
  }
  .range {
    font-size: 12px;
    line-height: 1.4;
    color: var(--color-status-text);
    margin: 0 0 8px;
  }
  .stateaction {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 0 12px;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    background: var(--color-card);
    color: var(--color-foreground);
    font-size: 14px;
    cursor: pointer;
  }
  .stateaction:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
  :global(.liststate) {
    border: none;
    padding: 32px;
  }
</style>
