<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import CharacterPortrait from './CharacterPortrait.svelte';
  import type { ProfileImage } from '$lib/connect/holomush/characteraccess/v1/characteraccess_pb';

  /**
   * Renders the media a profile carried — the single `profile.image.primary`
   * row in the portrait slot, and the `profile.image.gallery.00`…`.09` rows
   * below the sections.
   *
   * IT RENDERS NOTHING AT ZERO ROWS. No wrapper, no heading, no slot, no
   * "coming soon" placeholder. Media is one of the values the absence contract
   * governs (05-UI-SPEC): a viewer must not be able to tell a profile with no
   * images from a profile whose images they may not see, and a placeholder slot
   * is exactly the signal that would tell them.
   *
   * NOTHING IN v0.13 REACHES IT. 01-SPEC §7.3 ships the media model with zero
   * upload behaviour and nothing mints a media identifier, so projectPublic
   * emits no image row in this release. The behaviour is pinned by
   * ProfileMedia.svelte.test.ts rather than by running the game.
   */
  let {
    primaryImage = undefined,
    gallery = [],
    name,
    size = 80,
  }: {
    primaryImage?: ProfileImage | undefined;
    gallery?: ProfileImage[];
    name: string;
    size?: number;
  } = $props();

  /**
   * Derives the fetch path from the opaque `media_id`. 01-SPEC §7.3 leaves the
   * identifier's format deliberately unfixed and no host code parses, validates
   * or generates one, so this is the client's single derivation point and the
   * one place to change when a serving endpoint lands. It is same-origin, which
   * is what the prerendered SPA's `img-src 'self' data:` policy admits.
   */
  function mediaSrc(mediaId: string): string {
    return `/media/${encodeURIComponent(mediaId)}`;
  }

  // A broken handle degrades to the SAME initial-letter plate the no-media case
  // shows, rather than to a broken-image glyph that would read as "this profile
  // has an image you cannot see".
  let primaryFailed = $state(false);
  // Revealed content warnings, keyed by the row's media id. A warning is
  // advisory, so the reveal is per-image and never sticky across rows.
  let revealed = $state<Record<string, boolean>>({});

  const showPrimary = $derived(primaryImage !== undefined && !primaryFailed);

  function reveal(mediaId: string) {
    revealed = { ...revealed, [mediaId]: true };
  }
</script>

{#if primaryImage !== undefined && primaryFailed}
  <CharacterPortrait {name} {size} />
{:else if showPrimary && primaryImage !== undefined}
  {@const primary = primaryImage}
  {@const warned = primary.contentWarning !== '' && !revealed[primary.mediaId]}
  {#if primary.contentWarning !== ''}
    <span class="warned-slot" style="--media-size: {size}px">
      <img
        class="primary"
        class:warned
        src={mediaSrc(primary.mediaId)}
        alt={primary.altText}
        style="--media-size: {size}px"
        onerror={() => (primaryFailed = true)}
      />
      <!--
        The accessible name IS the warning text (05-UI-SPEC): the whole point of
        the control is that a viewer decides from the warning alone whether to
        look. `aria-expanded` carries the show/hide semantics without displacing
        that name with a generic verb.
      -->
      <button
        type="button"
        class="reveal"
        class:revealed={!warned}
        aria-expanded={warned ? 'false' : 'true'}
        onclick={() => reveal(primary.mediaId)}>{primary.contentWarning}</button
      >
    </span>
  {:else}
    <img
      class="primary"
      src={mediaSrc(primary.mediaId)}
      alt={primary.altText}
      style="--media-size: {size}px"
      onerror={() => (primaryFailed = true)}
    />
  {/if}
{/if}

{#if gallery.length > 0}
  <ul class="gallery">
    <!--
      Keyed on the array INDEX, not on the media id: §7.3 compares media names
      as exact bytes, `…gallery.0` and `…gallery.00` are two different rows, and
      the projection already emitted them in slot order. Re-keying or sorting
      here would install a second ordering authority beside the one the server
      owns.
    -->
    {#each gallery as row, i (i)}
      {@const warned = row.contentWarning !== '' && !revealed[row.mediaId]}
      <li>
        <img class="tile" class:warned src={mediaSrc(row.mediaId)} alt={row.altText} />
        {#if row.contentWarning !== ''}
          <button
            type="button"
            class="reveal"
            aria-expanded={warned ? 'false' : 'true'}
            onclick={() => reveal(row.mediaId)}>{row.contentWarning}</button
          >
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .warned-slot {
    position: relative;
    display: inline-flex;
    width: var(--media-size);
    flex-shrink: 0;
  }
  .primary {
    width: var(--media-size);
    height: var(--media-size);
    border-radius: 12px;
    object-fit: cover;
    flex-shrink: 0;
    border: 1px solid var(--color-border);
  }
  .gallery {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
    gap: 12px;
    list-style: none;
    margin: 24px 0 0;
    padding: 0;
  }
  .gallery li {
    position: relative;
  }
  .tile {
    width: 100%;
    aspect-ratio: 1 / 1;
    object-fit: cover;
    border-radius: 8px;
    border: 1px solid var(--color-border);
  }
  .warned {
    filter: blur(16px);
  }
  .reveal {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 44px;
    padding: 8px;
    border: 0;
    border-radius: 8px;
    background: color-mix(in srgb, var(--color-card) 80%, transparent);
    color: var(--color-status-text);
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    text-align: center;
    cursor: pointer;
  }
  .reveal.revealed {
    inset: auto 0 0 0;
    background: color-mix(in srgb, var(--color-card) 90%, transparent);
  }
  .reveal:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
</style>
