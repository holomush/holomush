<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import * as Table from '$lib/components/ui/table';
  import { Skeleton } from '$lib/components/ui/skeleton';
  import { formatLastActive } from '$lib/admin/lastActive';
  import type { CharacterRow, CharacterSortField } from '$lib/admin/client';

  /**
   * The dense operator table. It owns PRESENTATION ONLY — the page owns every
   * fetch and every mutation, and reaches them through the callbacks below.
   *
   * THE CLIENT NEVER RE-SORTS. `rows` is rendered in the order it arrived,
   * index for index. The ordering is a three-clause server contract —
   * (last_active_at = 0) first, then the requested key, then normalized_name
   * ascending as the tiebreak — so equal-key rows already have a specified,
   * stable order. A local re-sort to "fix" an order that looks wrong silently
   * breaks the property a server test proves, and the never-active rows are
   * exactly the ones it would move.
   *
   * SIX COLUMNS at >=768px; `Created` and `Ver` drop below it and `Ver` is NOT
   * relocated. The Sheet header carries v{n} when the operator opens the editor
   * and the conflict alert names both versions when a conflict occurs; those
   * are the only two moments the value is load-bearing.
   */
  interface Props {
    /** The page the server returned, rendered verbatim in its own order. */
    rows: CharacterRow[];
    /** Which column the server ordered by, for the header indicator. */
    sortField?: CharacterSortField;
    descending?: boolean;
    /** The one row whose mutation is in flight, or '' when none is. */
    pendingRowId?: string;
    /** The row that just changed, for the shipped arrival flash. */
    flashRowId?: string;
    /** Draw skeleton rows instead of data. */
    loading?: boolean;
    /** How many skeleton rows to draw while loading. */
    skeletonRows?: number;
    /** Injectable clock, so the relative buckets are testable. */
    now?: Date;
    onsort?: (field: CharacterSortField) => void;
    onedit?: (id: string) => void;
    onlifecycle?: (row: CharacterRow) => void;
    onfilterplayer?: (playerId: string) => void;
  }

  let {
    rows,
    sortField,
    descending = false,
    pendingRowId = '',
    flashRowId = '',
    loading = false,
    skeletonRows = 8,
    now,
    onsort,
    onedit,
    onlifecycle,
    onfilterplayer,
  }: Props = $props();

  /**
   * The five orderable columns, in render order. `Ver` is absent on purpose and
   * is rendered separately: it is a concurrency guard rather than a §11.3
   * field, and the wire enum carries no value that could express an ordering on
   * it. Sorting is CLICK-HEADER ONLY — there is no dropdown and no facet panel,
   * because §11.3 names a control whose options are drawn from the field list
   * as the warning sign, the field list being the privacy-bearing set.
   */
  const columns: { field: CharacterSortField; label: string; cls: string }[] = [
    { field: 'name', label: 'Name', cls: 'cell-name' },
    { field: 'player', label: 'Player', cls: 'cell-player' },
    { field: 'status', label: 'Status', cls: 'cell-status' },
    { field: 'lastActive', label: 'Last active', cls: 'cell-lastactive' },
    { field: 'created', label: 'Created', cls: 'cell-created' },
  ];

  const ariaSort = (field: CharacterSortField): 'ascending' | 'descending' | 'none' =>
    sortField === field ? (descending ? 'descending' : 'ascending') : 'none';

  /** A glyph, not only the accent: no state is carried by colour alone. */
  const caret = (field: CharacterSortField) =>
    sortField === field ? (descending ? '▼' : '▲') : '';

  const NANOS_PER_MS = 1_000_000n;

  /** The creation date, UTC, coarse to the day. */
  function formatCreated(nanos: bigint): string {
    if (nanos === 0n) return '—';
    return new Date(Number(nanos / NANOS_PER_MS)).toISOString().slice(0, 10);
  }

  /**
   * The row's accessible name. The values are the FULL stored ones, so a cell
   * clamped to two visible lines still announces what it holds.
   */
  const rowLabel = (r: CharacterRow) =>
    `${r.name} — player ${r.playerUsername}, ${r.status}, last active ${formatLastActive(r.lastActiveAt, now)}`;
</script>

<div
  class="tablewrap"
  role={loading ? 'status' : undefined}
  aria-label={loading ? 'Loading characters…' : undefined}
>
  <Table.Root class="chartable">
    <Table.Header>
      <Table.Row class="charhead">
        {#each columns as col (col.field)}
          <Table.Head class={`th ${col.cls}`} aria-sort={ariaSort(col.field)}>
            <button type="button" class="sortbtn" onclick={() => onsort?.(col.field)}>
              <span>{col.label}</span>
              {#if sortField === col.field}
                <span class="caret" aria-hidden="true">{caret(col.field)}</span>
              {/if}
            </button>
          </Table.Head>
        {/each}
        <Table.Head class="th cell-ver">Ver</Table.Head>
      </Table.Row>
    </Table.Header>
    <Table.Body>
      {#if loading}
        {#each Array.from({ length: skeletonRows }, (_, i) => i) as i (i)}
          <Table.Row class="charrow is-skeleton">
            {#each columns as col (col.field)}
              <Table.Cell class={col.cls}><Skeleton class="skelbar" /></Table.Cell>
            {/each}
            <Table.Cell class="cell-ver"><Skeleton class="skelbar" /></Table.Cell>
          </Table.Row>
        {/each}
      {:else}
        {#each rows as row (row.id)}
          <Table.Row
            class={`charrow${row.id === flashRowId ? ' is-flash' : ''}${row.id === pendingRowId ? ' is-pending' : ''}`}
            data-row-id={row.id}
            aria-busy={row.id === pendingRowId ? 'true' : undefined}
          >
            <Table.Cell class="cell-name">
              <button type="button" class="rowbtn" onclick={() => onedit?.(row.id)}>
                <span class="clamp2">{row.name}</span>
                <span class="sr-only">{rowLabel(row)}</span>
              </button>
            </Table.Cell>
            <Table.Cell class="cell-player">
              <button
                type="button"
                class="playerbtn clamp2"
                onclick={() => onfilterplayer?.(row.playerId)}>{row.playerUsername}</button
              >
            </Table.Cell>
            <Table.Cell class="cell-status">{row.status}</Table.Cell>
            <Table.Cell class="cell-lastactive">{formatLastActive(row.lastActiveAt, now)}</Table.Cell
            >
            <Table.Cell class="cell-created">{formatCreated(row.createdAt)}</Table.Cell>
            <Table.Cell class="cell-ver">
              <span class="vernum">{row.version}</span>
              <span class="rowactions">
                <button
                  type="button"
                  class="rowaction"
                  disabled={row.id === pendingRowId}
                  onclick={() => onedit?.(row.id)}>Edit</button
                >
                <button
                  type="button"
                  class="rowaction"
                  disabled={row.id === pendingRowId}
                  onclick={() => onlifecycle?.(row)}
                  >{row.status === 'retired' ? 'Un-retire' : 'Retire…'}</button
                >
              </span>
            </Table.Cell>
          </Table.Row>
        {/each}
      {/if}
    </Table.Body>
  </Table.Root>
</div>

<style>
  /* Lets Tailwind resolve theme() inside this scoped style block; the build
     fails loudly without it. */
  @reference "../../../app.css";

  /* Cells are Dense 12/400; headers Label 12/600. The status word is muted
     TEXT and never a badge, a dot or a colour: this table is lifecycle, the
     shipped session badge is session state, and the two vocabularies collide
     on the same word. */
  .tablewrap {
    width: 100%;
  }

  :global(.chartable) {
    font-size: 12px;
    line-height: 1.4;
    table-layout: fixed;
  }

  :global(.chartable th) {
    position: sticky;
    top: 0;
    z-index: 1;
    background: var(--color-card);
    font-size: 12px;
    font-weight: 600;
    color: var(--color-status-text);
    padding: 0 8px;
    white-space: normal;
  }

  :global(.chartable td) {
    padding: 0 8px;
    vertical-align: middle;
    white-space: normal;
    overflow-wrap: anywhere;
  }

  /* 36px minimum at cozy — dense, but above the height at which a 44px hit
     area becomes impossible without overlap. */
  :global(.charrow) {
    position: relative;
    min-height: 36px;
    height: 36px;
  }

  :global(.charrow .cell-status),
  :global(.charrow .cell-lastactive),
  :global(.charrow .cell-created),
  :global(.charrow .cell-ver) {
    color: var(--color-status-text);
  }

  :global(.charrow.is-pending) {
    opacity: 0.6;
  }

  :global(.skelbar) {
    height: 12px;
    width: 100%;
  }

  .sortbtn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    /* 44px of hit area including padding, at every band. */
    min-height: 44px;
    padding: 0 4px;
    margin: 0 -4px;
    background: none;
    border: 0;
    font: inherit;
    color: inherit;
    cursor: pointer;
    text-align: left;
  }
  .sortbtn:focus-visible,
  .rowbtn:focus-visible,
  .playerbtn:focus-visible,
  .rowaction:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
  .caret {
    color: var(--color-primary);
  }

  .rowbtn,
  .playerbtn {
    display: block;
    width: 100%;
    min-height: 36px;
    padding: 0;
    background: none;
    border: 0;
    font: inherit;
    color: inherit;
    text-align: left;
    cursor: pointer;
  }
  .playerbtn {
    color: var(--color-status-text);
  }

  /* Names are bounded server-side at 32 runes and usernames at the stored
     username cap, so two lines then an ellipsis is the whole overflow story
     and the page never scrolls sideways. The full value stays in the row's
     accessible name. */
  .clamp2 {
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
    border: 0;
  }

  /* Exactly two actions, revealed on hover AND on keyboard focus within the
     row. There is no third action in v0.13 — no delete, no rename — so an
     overflow trigger would be an affordance with nothing behind it. */
  .rowactions {
    position: absolute;
    right: 8px;
    top: 50%;
    transform: translateY(-50%);
    display: inline-flex;
    gap: 8px;
    opacity: 0;
    pointer-events: none;
    transition: opacity 120ms;
  }
  :global(.charrow:hover) .rowactions,
  :global(.charrow:focus-within) .rowactions {
    opacity: 1;
    pointer-events: auto;
  }
  .rowaction {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 0 8px;
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 6px;
    font: inherit;
    font-size: 12px;
    color: var(--color-foreground);
    cursor: pointer;
  }
  .rowaction:disabled {
    cursor: default;
    opacity: 0.5;
  }

  @media (prefers-reduced-motion: no-preference) {
    /* The shipped keyframe, reused rather than re-authored, inside the shipped
       guard. Any property that animates is named; never the catch-all. */
    :global(.charrow.is-flash) {
      animation: just-arrived 900ms ease-out;
    }
  }

  /* The phone band, reading the same Tailwind --breakpoint-md token the
     shipped rail reads, so the column drop and the rail collapse cannot be
     given different widths. Four columns here: the last two are gone, and the
     version is NOT relocated. */
  @media (width < theme(--breakpoint-md)) {
    /* phone-band-overlay:start — CharacterTable.svelte.test.ts slices the source
       between this marker and its closing counterpart at the bottom of this
       block, and asserts what it finds. Not decoration: move or delete either
       marker and that test fails by name. The pair replaced an indexOf bind on a
       verbatim copy of the media condition above, which broke on a reformat and
       retargeted onto any sub-md block added ahead of this one.

       Neither marker comment may spell the OTHER marker's token: the test finds
       each by first occurrence, so a mention inside this comment would be found
       ahead of the real one and the slice would invert. */
    :global(.chartable .cell-created),
    :global(.chartable .cell-ver) {
      display: none;
    }

    .rowactions {
      display: none;
    }

    /*
      The containing block for the row-spanning overlay below is the ROW. An
      absolutely positioned element resolves against its nearest positioned
      ancestor, so anchoring this to the primary cell instead would cover only
      that cell and a tap on Player, Status or Last active would hit nothing.
      Every cell stays static.
    */
    :global(.charrow) {
      position: relative;
    }

    .rowbtn::after {
      content: '';
      position: absolute;
      inset: 0;
    }
    /* phone-band-overlay:end — the closing counterpart of the opening marker at
       the top of this block; read its note first. A rule added BELOW this
       marker is outside the slice and therefore outside the "position: relative
       on the row and on no cell" assertion, so keep the containing-block
       declarations between the two. */
  }
</style>
