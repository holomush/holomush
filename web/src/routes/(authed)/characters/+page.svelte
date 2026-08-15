<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { goto } from '$app/navigation';
  import { client, listMyCharacters, setDefaultCharacter } from '$lib/characters/client';
  import { takeCreatedNotice, type CreatedNotice } from '$lib/characters/createdNotice';
  import { setCharacterSession } from '$lib/stores/authStore';
  import CharacterRoster from '$lib/components/characters/CharacterRoster.svelte';

  /**
   * The owner roster. It owns exactly four things — the two fetches, their
   * join, the default id and the announcement regions — and delegates all
   * layout to CharacterRoster.
   *
   * TWO CALLS, JOINED ON THE CHARACTER ID (Q2). Neither message alone can draw
   * UI-SPEC's badge matrix: `OwnCharacter` carries the lifecycle `status` and
   * no session telemetry at all, and `CharacterSummary` carries the session
   * telemetry and no lifecycle. The join is the answer to that gap, and it is
   * two round trips rather than three because the default character arrives on
   * the (authed) layout's own session-check response, which already ran.
   */
  /*
   * THE OVERLAY IS NARROWED TO THE ONE FIELD THE BADGE RULES READ. The
   * server's `sessionStatus` token deliberately does NOT travel: forwarding
   * it onto a badge is one of the two bugs this phase fixed, and carrying it
   * in scope on the components that draw the badge is the shortest path back
   * to that bug. Leaving it out makes the regression a type error rather than
   * a review finding.
   */
  type SessionOverlay = { hasActiveSession: boolean };
  type Row = { id: string; name: string; status: string; session?: SessionOverlay };

  let { data } = $props();

  let rows = $state<Row[]>([]);
  let loading = $state(true);
  /** The lifecycle read failed — the page has nothing to draw. */
  let loadFailed = $state(false);

  // The INITIAL default comes from the (authed) layout's own session check,
  // which already ran — OwnCharacter carries no is-default flag, so there is no
  // per-character source for it and no reason to spend a third round trip.
  // (The gate that pins "no third round trip" scans this whole file, not only
  // its markup, so a comment may not be what trips it either.)
  // untrack: this is the INITIAL value, and the local state diverges from it the
  // moment a write lands. Reading `data` reactively here would clobber the
  // player's own change on the next layout-data invalidation.
  let defaultCharacterId = $state(untrack(() => data?.defaultCharacterId ?? ''));
  /** The id whose default-change request is in flight, or '' when none is. */
  let savingDefaultId = $state('');
  let defaultStatus = $state('');
  /** Write failures only. A create that SUCCEEDED never lands here. */
  let actionError = $state('');
  let createdNotice = $state<CreatedNotice | null>(null);

  async function load() {
    loading = true;
    loadFailed = false;
    /*
     * Concurrent, and settled rather than raced: the two reads answer
     * different questions and only one of them is load-bearing. A failed
     * session read costs the badges; a failed lifecycle read costs the page.
     */
    const [own, summaries] = await Promise.allSettled([listMyCharacters(), client.webListCharacters({})]);

    if (own.status === 'rejected') {
      loadFailed = true;
      loading = false;
      return;
    }

    const overlay = new Map<string, SessionOverlay>();
    if (summaries.status === 'fulfilled') {
      for (const s of summaries.value.characters) {
        overlay.set(s.characterId, { hasActiveSession: s.hasActiveSession });
      }
    }

    // A character present in the lifecycle result and absent from the session
    // result simply has no overlay, which the badge rules already read as
    // offline. That is also the whole session-read-failed case: every row
    // loses its overlay and the roster still draws.
    rows = own.value.map((c) => ({
      id: c.id,
      name: c.name,
      status: c.status,
      session: overlay.get(c.id),
    }));
    loading = false;
  }

  onMount(() => {
    // Read ONCE and clear, so a later visit does not re-announce an old create.
    createdNotice = takeCreatedNotice();
    void load();
  });

  async function makeDefault(id: string) {
    const char = rows.find((c) => c.id === id);
    if (char === undefined) return;
    savingDefaultId = id;
    actionError = '';
    try {
      // A GUI button is a machine-initiated structural write, so it takes the
      // typed RPC. The human text-command path is for conversational verbs a
      // player types, and must not be string-built into from here
      // (.claude/rules/gateway-boundary.md).
      const roster = await setDefaultCharacter(id);
      // The success status is what makes the requested id the default; the
      // returned roster is server truth for membership and order. Each row
      // keeps its locally-joined session overlay, because OwnCharacter carries
      // none and this write is not a reason to spend a second session read.
      defaultCharacterId = id;
      const overlay = new Map(rows.map((c) => [c.id, c.session]));
      rows = roster.map((own) => ({
        id: own.id,
        name: own.name,
        status: own.status,
        session: overlay.get(own.id),
      }));
      // The newer announcement supersedes the create confirmation rather than
      // stacking beneath it.
      createdNotice = null;
      defaultStatus = `${char.name} is now your default character.`;
    } catch {
      defaultStatus = '';
      // The marker moves only on a success, so a failure leaves it where the
      // server last put it.
      actionError = "Couldn't save. Try again.";
    } finally {
      savingDefaultId = '';
    }
  }

  /** The one authored refusal for a select, whichever leg produced it. */
  const SELECT_FAILED = 'Failed to select character. Try again.';

  async function selectCharacter(id: string) {
    actionError = '';
    try {
      const resp = await client.webSelectCharacter({ characterId: id });
      if (resp.success) {
        setCharacterSession(resp.sessionId, resp.characterName);
        goto('/terminal');
      } else {
        // THE COPY IS AUTHORED HERE, NOT FORWARDED FROM THE WIRE. The
        // producers of `error_message` (internal/grpc/auth_handlers.go)
        // include "character does not belong to this player" — an ownership
        // disclosure phrased in wire vocabulary, on a surface whose whole
        // mutation path collapses ownership causes into one authored literal.
        // Every sibling failure path in this file already authors its own
        // copy; this one is the same rule, and it is one string so the two
        // legs cannot drift apart.
        actionError = SELECT_FAILED;
      }
    } catch {
      actionError = SELECT_FAILED;
    }
  }
</script>

<div class="page">
  <div class="column">
    <h1 class="title">Your characters</h1>

    <!--
      Persistent regions, always in the DOM so a screen reader announces the
      CHANGE rather than the region's arrival.

      The create confirmation lives in the role="status" region in BOTH of its
      forms, including the partial one. The create succeeded — the character
      exists and holds the name — so nothing here is an alert and no copy may
      read as a prompt to create again. The role="alert" region below carries
      write failures only.
    -->
    <p role="status" class="status" class:sr-only={!defaultStatus && createdNotice === null}>
      {#if defaultStatus}
        {defaultStatus}
      {:else if createdNotice !== null}
        {#if createdNotice.profileIncomplete}
          <!--
            The create landed but its second transaction lost one or more of the
            supplied profile values (Q1 option-a). The link is the whole point
            of the variant: it makes the loss a one-click repair on the surface
            whose first section holds exactly the five fields the create
            submits, rather than something to be discovered later as blank
            fields. It targets the notice's characterId, never its name.
          -->
          Created {createdNotice.name}. Some details didn't save — add them on the
          <a class="repair-link" href="/characters/{createdNotice.characterId}">character page</a>.
        {:else}
          Created {createdNotice.name}.
        {/if}
      {/if}
    </p>
    <p role="alert" class="error" class:sr-only={!actionError}>{actionError}</p>

    {#if loading}
      <p class="muted">Loading characters…</p>
    {:else if loadFailed}
      <p class="muted">Couldn't load your characters. Try again.</p>
      <button type="button" class="retry" data-testid="retry-load" onclick={() => void load()}>Try again</button>
    {:else}
      <CharacterRoster
        characters={rows}
        {defaultCharacterId}
        {savingDefaultId}
        onselect={selectCharacter}
        onmakedefault={makeDefault}
      />
    {/if}
  </div>
</div>

<style>
  /* Lets Tailwind resolve theme() inside this scoped style block; the build
     fails loudly without it. */
  @reference "../../../app.css";

  .page {
    display: flex;
    align-items: flex-start;
    justify-content: center;
    min-height: calc(100vh - 36px);
    padding: 32px 16px;
  }
  /* Matches the two sibling owner surfaces (`characters/[id]`, `c/[id]`), which
     both go 16px → 48px at this band. Without it this route sat at a flat 24px,
     off the 4px scale, and the gutter visibly jumped when navigating between
     the three. */
  @media (width >= theme(--breakpoint-md)) {
    .page {
      padding: 32px 48px;
    }
  }
  .column {
    width: 100%;
    max-width: 720px;
  }
  .title {
    margin: 0 0 24px;
    font-size: 20px;
    font-weight: 600;
    line-height: 1.2;
    color: var(--color-foreground);
  }
  .status,
  .error,
  .muted {
    margin: 0 0 16px;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
  }
  .status,
  .muted {
    color: var(--color-status-text);
  }
  .error {
    color: var(--color-destructive);
  }
  .repair-link {
    color: var(--color-primary);
  }
  .retry {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 8px 16px;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    background: transparent;
    color: var(--color-foreground);
    font-size: 14px;
    cursor: pointer;
  }
  .retry:focus-visible,
  .repair-link:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border-width: 0;
  }
</style>
