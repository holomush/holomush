<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import CharacterPortrait from './CharacterPortrait.svelte';

  /**
   * One roster card and its badge matrix. Props-only: it reads no store, makes
   * no request and owns no route state.
   *
   * THE SUPPRESSION IS HERE, IN THE TEMPLATE, NOT UPSTREAM IN THE JOIN. A
   * lifecycle that is not `active` renders the `Retired` badge and no session
   * badge at all, because `Retired · Offline` is meaningless and the two
   * vocabularies collide on the token `active` (sketch 008 Finding, D-96).
   * Clearing the session overlay in the route's join would produce the same
   * pixels today and would hold only for as long as the join kept remembering
   * to do it; a template rule holds for every caller.
   *
   * IT CARRIES THE ONE ROUTE INTO /characters/[id]. The authoring surface owns
   * twelve of the thirteen editable fields and had no ordinary entry point:
   * the roster's repair notice is gated on a partial-write failure, and the
   * public link goes to the read-only /c/[id]. The `Edit profile →` control
   * below is unconditional for that reason — see its own comment.
   */
  let {
    id,
    name,
    status,
    session = undefined,
    isDefault = false,
    playable = false,
    savingDefault = false,
    onselect = undefined,
    onmakedefault = undefined,
  }: {
    id: string;
    name: string;
    /** characters.status — exactly `active`, `retired` or `idle` (01-SPEC §4.2). */
    status: string;
    /**
     * The CharacterSummary half of the join. Absent when the character appears
     * in the owner roster and not in the session-bearing list, which is an
     * ordinary outcome the badge rules already handle as `Offline`.
     */
    session?: { hasActiveSession: boolean };
    isDefault?: boolean;
    playable?: boolean;
    savingDefault?: boolean;
    onselect?: (id: string) => void;
    onmakedefault?: (id: string) => void;
  } = $props();

  /**
   * The lifecycle switch. `idle` has no transition into it in v0.13, which is
   * precisely why its arm may not fall through to something permissive and why
   * it carries no prose of its own — it shares the not-playable marker rather
   * than teaching a player a word for a state they cannot reach. An
   * unrecognised value takes the same DENYING arm: no session badge, no
   * `Default` badge, and the card still renders.
   */
  const showsSession = $derived.by((): boolean => {
    switch (status) {
      case 'active':
        return true;
      case 'retired':
        return false;
      case 'idle':
        return false;
      default:
        return false;
    }
  });

  /*
   * Two authored words, never the server's session token: UI-SPEC's badge
   * table fixes the session vocabulary at `Active` | `Offline`, and
   * forwarding an arbitrary server token here would put wire vocabulary on a
   * player-facing badge. The overlay type above carries only the boolean, so
   * that token is not merely unused here — it is not in scope to be reached
   * for.
   */
  const sessionWord = $derived(session?.hasActiveSession ? 'Active' : 'Offline');

  function select() {
    onselect?.(id);
  }
</script>

{#snippet body()}
  <CharacterPortrait {name} size={44} />
  <div class="body">
    <span class="name" data-testid="char-name">{name}</span>
    <div class="badges">
      {#if showsSession}
        {#if isDefault}
          <span class="badge badge-default" data-testid="default-badge">Default</span>
        {/if}
        <span class="badge" class:badge-online={session?.hasActiveSession} data-testid="session-badge"
          >{sessionWord}</span
        >
      {:else}
        <span class="badge" data-testid="lifecycle-badge">Retired</span>
      {/if}
    </div>
    <div class="controls">
      {#if playable && !isDefault}
        <!--
          stopPropagation: the card itself selects a character, and setting a
          default must not also drop the player into the game. The label does NOT
          change in flight (UI-SPEC Loading states); disabled + aria-busy carry
          that state.
        -->
        <button
          type="button"
          name="makeDefault"
          class="make-default"
          data-testid="make-default"
          disabled={savingDefault}
          aria-busy={savingDefault}
          onclick={(e: MouseEvent) => {
            e.stopPropagation();
            onmakedefault?.(id);
          }}>Make default</button
        >
      {/if}
      <!--
        THE OWNER'S ROUTE INTO THEIR OWN AUTHORING SURFACE, and the only one.
        /characters/[id] owns twelve of the thirteen editable fields, and before
        this control the sole link to it was the one-shot repair notice on the
        roster route — gated on a partial-write FAILURE and cleared after one
        render, so a player whose create succeeded cleanly could not reach it at
        all.

        UNCONDITIONAL, on every card the owner holds. A retired character's
        twelve fields are every bit as editable as an active one's; gating this
        on `playable` would leave that character with a link to the public view
        of a profile its owner cannot edit.

        The affordance is owner-only by construction — this component renders
        only inside the owner roster — which is the case 05-UI-SPEC's absence
        contract admits by name: it varies by viewer, but it tells the owner
        they are the owner and leaks nothing.

        stopPropagation for the same reason `Make default` does it: on a
        playable card the whole surface selects, and following a link must not
        also drop the player into the game.
      -->
      <a
        class="edit-character"
        data-testid="edit-character"
        href="/characters/{id}"
        onclick={(e: MouseEvent) => e.stopPropagation()}>Edit profile →</a
      >
      {#if !playable}
        <a class="view-profile" data-testid="view-profile" href="/c/{id}">View profile →</a>
      {/if}
    </div>
  </div>
{/snippet}

{#if playable}
  <!--
    008-B's decisive property: every card in the Playable grid is uniformly
    clickable across its WHOLE surface, so the grid carries one affordance
    rather than a mix of them.
  -->
  <div
    class="card card-playable"
    role="button"
    tabindex="0"
    data-testid="roster-card"
    onclick={select}
    onkeydown={(e: KeyboardEvent) => {
      /*
        THE CARD ANSWERS ONLY FOR ITS OWN FOCUS. Keyboard events reach this
        handler by bubbling, and the card's arm calls preventDefault() — so
        without this guard, pressing Enter on a control INSIDE the card
        suppresses that control's own activation and selects the character
        instead. The `Edit profile →` link would be mouse-only, and
        `Make default` would enter the game.

        The guard is on the TARGET rather than a stopPropagation on each
        child, so it holds for every control this card ever grows. Only
        focusable elements receive key events, so `target !== currentTarget`
        means exactly "a child control has focus".
      */
      if (e.target !== e.currentTarget) return;
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        select();
      }
    }}
  >
    {@render body()}
  </div>
{:else}
  <div class="card" data-testid="roster-card">
    {@render body()}
  </div>
{/if}

<style>
  .card {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 16px;
    border: 1px solid var(--color-border);
    border-radius: 12px;
    background: var(--color-card);
    color: var(--color-card-foreground);
    /* The interactive-target floor applies to the card itself. */
    min-height: 44px;
    text-align: left;
  }
  .card-playable {
    cursor: pointer;
  }
  .card-playable:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .name {
    font-size: 14px;
    font-weight: 600;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
  .badges {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .badge {
    width: fit-content;
    padding: 1px 6px;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    color: var(--color-status-text);
  }
  /*
   * The `Default` badge is the one once-per-roster accent marker. The session
   * and `Retired` badges are deliberately NOT accent, and no state here is
   * signalled by colour alone — each badge carries its own word.
   */
  .badge-default {
    border-color: var(--color-primary);
    color: var(--color-primary);
  }
  .badge-online {
    border-color: var(--color-status-online);
    color: var(--color-status-online);
  }
  /*
   * One control ROW rather than a stack: the card carries up to three controls
   * and stacking them would push the name and badges into a tall column for no
   * gain. It wraps, so the narrow band keeps them all reachable.
   */
  .controls {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 4px;
  }
  .controls:empty {
    display: none;
  }
  .make-default,
  .edit-character,
  .view-profile {
    display: inline-flex;
    align-items: center;
    width: fit-content;
    /* UI-SPEC:88 makes 44px an unqualified hit-area floor "at every band". The
       4px/12px/1.4 box computes to ~27px on its own, so the floor is stated
       explicitly rather than left to the padding to reach by accident. */
    min-height: 44px;
    padding: 4px 8px;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    background: transparent;
    color: var(--color-card-foreground);
    font-size: 12px;
    font-weight: 400;
    line-height: 1.4;
    cursor: pointer;
    text-decoration: none;
  }
  .make-default:disabled {
    cursor: default;
    opacity: 0.6;
  }
  .make-default:focus-visible,
  .edit-character:focus-visible,
  .view-profile:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
</style>
