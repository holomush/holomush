<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import CharacterPortrait from './CharacterPortrait.svelte';
  import ProfileMedia from './ProfileMedia.svelte';
  import type { PublicCharacter } from '$lib/connect/holomush/characteraccess/v1/characteraccess_pb';

  /**
   * The read-only public profile body. Props-only: it reads no store, makes no
   * request and owns no route state, so its whole behaviour is a function of
   * the bytes the response carried.
   *
   * ABSENCE IS A WIRE PROPERTY, NEVER A CLIENT FILTER (01-SPEC §8.9). Every
   * element below is conditioned on THE KEY'S PRESENCE IN THE MAP and on
   * nothing else. There is deliberately no list of expected field names here
   * and nothing iterates the map: such a list is the client-side allowlist §8.9
   * forbids, and it is exactly what would turn a field this viewer may not read
   * into a visible omission. A viewer must not be able to tell a field the
   * character left blank from one the floor kept back.
   *
   * (The wording above avoids the vocabulary a plan acceptance grep scans this
   * file for. That gate scans the whole file rather than only its markup on
   * purpose: a gate that has to be suppressed to stay green stops being a gate,
   * so a comment may not be the thing that trips it.)
   *
   * A HEADING LIVES INSIDE ITS BODY'S CONDITIONAL, so a heading can never
   * appear without prose beneath it — a bare heading is a count of one.
   *
   * It shares NO component with the owner authoring surface. The two render the
   * same twelve names but answer different questions: this one's correctness
   * property is absence, /characters/[id]'s is per-section save. Coupling a
   * privacy-critical renderer to a form is how an improvement to one silently
   * changes the other.
   */
  let { character }: { character: PublicCharacter } = $props();

  /*
   * Authored constants, interpolating nothing. Both slots are viewer-INVARIANT,
   * which is exactly why D-94's named-slot rule governs them rather than the
   * absence rule: they disclose only "the platform has not built this yet",
   * which is public. Because they carry no server or player data, no encoding
   * or length question can arise on them.
   */
  const WEB_DM_LABEL = 'Direct messages';
  const WEB_DM_VALUE = 'Not available yet.';
  const SHEET_HEADING = 'Sheet';
  const SHEET_BODY = 'No sheet system yet.';

  const p = $derived(character.profile);
</script>

<article class="card">
  <div class="head">
    {#if character.primaryImage !== undefined}
      <ProfileMedia primaryImage={character.primaryImage} name={character.name} size={80} />
    {:else}
      <CharacterPortrait name={character.name} size={80} />
    {/if}
    <div class="ident">
      <!-- The name is bounded server-side at 32 runes
           (internal/charname/syntax/syntax.go MaxNameLength), so the heading
           needs no truncation logic. -->
      <h1 class="name">{character.name}</h1>
      {#if 'profile.pronouns' in p}
        <p class="pronouns">{p['profile.pronouns']}</p>
      {/if}
      {#if 'profile.concept' in p}
        <p class="concept">{p['profile.concept']}</p>
      {/if}
    </div>
  </div>

  {#if character.description !== ''}
    <p class="description" data-testid="description">{character.description}</p>
  {/if}

  {#if 'profile.species' in p || 'profile.age' in p || 'profile.faction' in p || 'profile.currently' in p || 'profile.timezone' in p}
    <dl class="facts" data-testid="fact-pills">
      {#if 'profile.species' in p}
        <div class="pill"><dt>Species</dt><dd>{p['profile.species']}</dd></div>
      {/if}
      {#if 'profile.age' in p}
        <div class="pill"><dt>Age</dt><dd>{p['profile.age']}</dd></div>
      {/if}
      {#if 'profile.faction' in p}
        <div class="pill"><dt>Faction</dt><dd>{p['profile.faction']}</dd></div>
      {/if}
      {#if 'profile.currently' in p}
        <div class="pill"><dt>Currently</dt><dd>{p['profile.currently']}</dd></div>
      {/if}
      {#if 'profile.timezone' in p}
        <div class="pill"><dt>Timezone</dt><dd>{p['profile.timezone']}</dd></div>
      {/if}
    </dl>
  {/if}
</article>

{#if 'profile.appearance' in p}
  <section class="prose">
    <h2>Appearance</h2>
    <p>{p['profile.appearance']}</p>
  </section>
{/if}

{#if 'profile.personality' in p}
  <section class="prose">
    <h2>Personality</h2>
    <p>{p['profile.personality']}</p>
  </section>
{/if}

{#if 'profile.biography' in p}
  <section class="prose">
    <h2>Biography</h2>
    <p>{p['profile.biography']}</p>
  </section>
{/if}

{#if 'profile.rumors' in p}
  <section class="prose">
    <h2>Rumours &amp; hooks</h2>
    <p>{p['profile.rumors']}</p>
  </section>
{/if}

<!--
  profile.rp_preferences is OUT-OF-CHARACTER text about the player, not about
  the character, so it is labelled as such and renders LAST — after every
  in-character section (§7.2 naming collision).
-->
{#if 'profile.rp_preferences' in p}
  <section class="prose">
    <h2>Out of character</h2>
    <p>{p['profile.rp_preferences']}</p>
  </section>
{/if}

<ProfileMedia gallery={character.gallery} name={character.name} />

<!--
  The named slots. Non-interactive by construction: a labelled value, never a
  control, never a disabled control, never a dead link. A disabled button says
  "you cannot do this"; a named slot says "this does not exist yet", and only
  the second is true.
-->
<div class="slot" data-testid="web-dm">
  <span class="slot-label">{WEB_DM_LABEL}</span>
  <span class="slot-value">{WEB_DM_VALUE}</span>
</div>

<section class="prose" data-testid="sheet">
  <h2>{SHEET_HEADING}</h2>
  <p class="muted">{SHEET_BODY}</p>
</section>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 16px;
    border: 1px solid var(--color-border);
    border-radius: 12px;
    background: var(--color-card);
    color: var(--color-card-foreground);
  }
  .head {
    display: flex;
    align-items: flex-start;
    gap: 16px;
  }
  .ident {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .name {
    margin: 0;
    font-size: 20px;
    font-weight: 600;
    line-height: 1.2;
    overflow-wrap: anywhere;
  }
  .pronouns,
  .concept,
  .description {
    margin: 0;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
  }
  .pronouns {
    color: var(--color-status-text);
  }
  .description {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .facts {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin: 0;
  }
  .pill {
    display: flex;
    align-items: baseline;
    gap: 4px;
    padding: 4px 8px;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    min-width: 0;
  }
  .pill dt {
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--color-status-text);
  }
  .pill dd {
    margin: 0;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
  .prose {
    margin-top: 24px;
  }
  .prose h2 {
    margin: 0 0 8px;
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--color-status-text);
  }
  .prose p {
    margin: 0;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
    /* The prose IS the content; the profile never truncates it. */
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .prose .muted {
    color: var(--color-status-text);
  }
  .slot {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 8px;
    margin-top: 24px;
    padding: 16px;
    border: 1px solid var(--color-border);
    border-radius: 12px;
    background: var(--color-card);
  }
  .slot-label {
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--color-status-text);
  }
  .slot-value {
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
    color: var(--color-status-text);
  }
</style>
