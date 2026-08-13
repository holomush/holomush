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
    {#if !playable}
      <a class="view-profile" data-testid="view-profile" href="/c/{id}">View profile →</a>
    {/if}
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
  .make-default,
  .view-profile {
    display: inline-flex;
    align-items: center;
    width: fit-content;
    margin-top: 4px;
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
  .view-profile:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
</style>
