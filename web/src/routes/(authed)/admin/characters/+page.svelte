<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { toast } from 'svelte-sonner';
  import * as Empty from '$lib/components/ui/empty';
  import * as Pagination from '$lib/components/ui/pagination';
  import CharacterFilterBar from '$lib/components/admin/CharacterFilterBar.svelte';
  import CharacterTable from '$lib/components/admin/CharacterTable.svelte';
  import EditCharacterSheet from '$lib/components/admin/EditCharacterSheet.svelte';
  import LifecycleConfirmDialog from '$lib/components/admin/LifecycleConfirmDialog.svelte';
  import {
    ADMIN_PAGE_SIZE,
    listAdminCharacters,
    searchAdminCharacters,
    updateAdminCharacter,
    retireAdminCharacter,
    unretireAdminCharacter,
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

  /** The one row whose mutation is in flight; the rest stay interactive. */
  let pendingRowId = $state('');
  /** The row that just changed, for the shipped arrival flash. */
  let flashRowId = $state('');
  /**
   * The character the operator reached for, and through which entry point.
   * `edit` opens the Sheet; `lifecycle` opens the confirmation. The Sheet's own
   * transition picker also resolves here, so BOTH entrances reach one confirm
   * and one RPC.
   */
  let selected = $state<{ id: string; intent: 'edit' | 'lifecycle' } | null>(null);
  /** Which direction the open confirmation is confirming. */
  let transition = $state<'retire' | 'unretire'>('retire');

  const selectedRow = $derived(
    selected ? (rows.find((r) => r.id === selected!.id) ?? null) : null,
  );

  function onedit(id: string) {
    selected = { id, intent: 'edit' };
  }

  function onlifecycle(row: CharacterRow) {
    transition = row.status === 'retired' ? 'unretire' : 'retire';
    selected = { id: row.id, intent: 'lifecycle' };
  }

  /** The Sheet's picker routes here: it selects a direction, never applies one. */
  function onsheetlifecycle(intent: 'retire' | 'unretire') {
    if (!selected) return;
    transition = intent;
    selected = { id: selected.id, intent: 'lifecycle' };
  }

  /**
   * Six seconds rather than the four-second default: this is a receipt, and the
   * `Undo` on a retire needs a window an operator can actually reach. It is
   * never the sole carrier of the outcome — the row already changed in place.
   */
  const TOAST_MS = 6000;

  /**
   * The receipt's `Undo` is the one entrance to a mutation that no overlay
   * wraps — the confirmation catches for its own button, but a toast action is
   * dismissed the moment it is clicked, and svelte-sonner ignores a returned
   * promise. So a refusal here has to carry its own surface or it has none at
   * all, and the operator reads the dismissal as success.
   *
   * AUTHORED, like every other failure on this page: the wire value selects
   * nothing and appears nowhere. It also says what is still true of the row,
   * because the row itself did not move.
   */
  const UNDO_FAILED = "Couldn't undo that. The character is still retired.";

  /**
   * The row updates IN PLACE from the response and the table is never re-read.
   * The mutation answered with the post-write row, so a second request could
   * only disagree with it — and would cost the operator their place in a sorted,
   * filtered, paginated list to learn something it already knows.
   */
  function applyRow(updated: CharacterRow | undefined) {
    if (!updated) return;
    rows = rows.map((r) => (r.id === updated.id ? updated : r));
    flashRowId = updated.id;
  }

  /**
   * D-110's binding sequence for an edit. The Sheet has already disabled its
   * submit and marked it busy; this marks the row pending, awaits the write,
   * closes the Sheet, updates the row from the response and fires the receipt.
   *
   * IT DOES NOT CATCH. A refusal belongs to the Sheet, which is still open and
   * still holds the operator's typing: an `Aborted` renders the conflict there
   * and NO toast fires, because a toast is the receipt for something that
   * finished.
   */
  async function saveEdit(args: {
    paths: string[];
    values: Record<string, string>;
    expectedVersion: number;
  }) {
    const id = selected?.id;
    if (!id) return;
    pendingRowId = id;
    try {
      const updated = await updateAdminCharacter({ characterId: id, ...args });
      selected = null;
      applyRow(updated);
      toast(
        `AdminUpdateCharacter · update_mask: ${args.paths.length} paths · ` +
          `v${args.expectedVersion} → v${updated?.version}`,
        { duration: TOAST_MS },
      );
    } finally {
      pendingRowId = '';
    }
  }

  /**
   * The one lifecycle code path. The confirmation's button, and the retire
   * receipt's `Undo`, both come through here — so `Undo` is the same RPC, with
   * the same guard, as the transition picker's, and never a status value.
   */
  async function applyLifecycle(id: string, intent: 'retire' | 'unretire', expectedVersion: number) {
    pendingRowId = id;
    try {
      const updated =
        intent === 'retire'
          ? await retireAdminCharacter(id, expectedVersion)
          : await unretireAdminCharacter(id, expectedVersion);
      selected = null;
      applyRow(updated);
      const rpc = intent === 'retire' ? 'AdminRetireCharacter' : 'AdminUnretireCharacter';
      const message = `${rpc} · ${updated?.name} · v${expectedVersion} → v${updated?.version}`;
      if (intent === 'retire' && updated) {
        const undoAt = updated.version;
        const undoId = updated.id;
        toast(message, {
          duration: TOAST_MS,
          action: {
            label: 'Undo',
            // The NEW version from the retire response: the row moved, and the
            // guard has to be composed against where it moved to — which is
            // also why this can be refused, since a second operator may have
            // moved it again since.
            onClick: () => {
              void applyLifecycle(undoId, 'unretire', undoAt).catch(() => {
                toast(UNDO_FAILED, { duration: TOAST_MS });
              });
            },
          },
        });
      } else {
        toast(message, { duration: TOAST_MS });
      }
    } finally {
      pendingRowId = '';
    }
  }

  async function confirmLifecycle() {
    const row = selectedRow;
    if (!row) return;
    await applyLifecycle(row.id, transition, row.version);
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

  /**
   * ONLY THE NEWEST READ MAY COMMIT. The 250ms debounce bounds a keystroke
   * burst but not the network, and a sort click, a status change or a page turn
   * fires with no debounce at all — so two reads in flight is ordinary, and
   * nothing makes the older one answer first. A stale answer that committed
   * would render one request's rows beneath another request's term, status,
   * player filter, sort and page number; a stale REFUSAL that committed would
   * flip a good page into the load-failure screen. Both are silent.
   *
   * `loading` is stamped the same way, or the first response to arrive would
   * clear it while a newer read is still outstanding.
   */
  let readSeq = 0;

  async function reload() {
    const seq = ++readSeq;
    loading = true;
    flashRowId = '';
    const query = { sortField, descending, status, playerId, page: pageNum };
    const wanted = term;
    try {
      const page = wanted.trim() === ''
        ? await listAdminCharacters(query)
        : await searchAdminCharacters(query, wanted);
      if (seq !== readSeq) return;
      rows = page.rows;
      totalCount = page.totalCount;
      failure = '';
    } catch {
      // The caught value is deliberately not bound: there is nothing this page
      // may learn from it and nothing it may show. A search that fails and a
      // list that fails are the two states an operator can act on.
      if (seq !== readSeq) return;
      rows = [];
      totalCount = 0n;
      failure = wanted.trim() === '' ? 'load' : 'search';
    } finally {
      if (seq === readSeq) loading = false;
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
    {flashRowId}
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

<!--
  The two overlays. Both entrances to a lifecycle transition — the table's row
  action and the Sheet's own picker — resolve into ONE confirmation and ONE
  RPC; the Sheet never sends a transition itself.

  Keyed on the character id so switching rows rebuilds the form rather than
  carrying one character's draft onto another.
-->
{#if selectedRow && selected?.intent === 'edit'}
  {#key selectedRow.id}
    <EditCharacterSheet
      row={selectedRow}
      save={saveEdit}
      onlifecycle={onsheetlifecycle}
      onclose={() => (selected = null)}
    />
  {/key}
{:else if selectedRow && selected?.intent === 'lifecycle'}
  <LifecycleConfirmDialog
    name={selectedRow.name}
    intent={transition}
    onconfirm={confirmLifecycle}
    oncancel={() => (selected = null)}
  />
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
