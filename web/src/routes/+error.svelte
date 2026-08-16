<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors

  The SINGLE error boundary for the whole route tree. SvelteKit resolves the
  nearest one; a second boundary anywhere under web/src/routes/ would let one
  kind of miss render a different component from another, and which component
  came back is readable from outside. The count is pinned by
  test/meta/web_error_boundary_census_test.go (INV-PRIVACY-14).

  This component takes NO prop describing why it is rendering, and reads
  nothing about the request. It often could know — a denied RPC, a route that
  did not match — and having nothing is what stops a later change surfacing it.
  See the plan SUMMARY for the enumeration of what is deliberately unread.
-->
<script lang="ts">
  import CompassIcon from '@lucide/svelte/icons/compass';
  import { authState } from '$lib/stores/authStore';
  import { visibleSections } from '$lib/nav/sections';

  // The auth store starts unresolved and every resolution path — registered
  // player AND guest — goes through setPlayerProfile, which sets
  // isPlayerAuthenticated. So that field is the resolution bit; isGuest alone
  // cannot serve, because it is false both before resolution and for a resolved
  // registered player.
  //
  // Unresolved therefore falls back to GUEST, the smallest destination set.
  // The direction is deliberate: rendering fewer destinations for a frame
  // costs nothing, while rendering the registered-player set to a viewer whose
  // session has not been restored would correlate the list with a permission.
  const resolved = $derived($authState.isPlayerAuthenticated);
  const isGuest = $derived(resolved ? $authState.isGuest : true);

  // Pure and synchronous over a compile-time const, so this never awaits and
  // can never be unavailable. `/admin` is not a member and is never added: the
  // rail draws its own Admin entry from the session's roles.
  const sections = $derived(visibleSections({ isGuest }));
</script>

<div class="notfound">
  <CompassIcon class="notfound-glyph" aria-hidden="true" />
  <h1 class="notfound-heading">Not found</h1>
  <p class="notfound-body">We couldn't find that page.</p>
  <nav class="notfound-destinations" aria-label="Where you can go">
    <a class="destination" href="/">Home</a>
    {#each sections as section (section.id)}
      <a class="destination" href={section.href}>{section.label}</a>
    {/each}
  </nav>
</div>

<style>
  .notfound {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    min-height: 60vh;
    padding: 32px 24px;
    text-align: center;
    color: var(--color-foreground);
  }

  :global(.notfound-glyph) {
    width: 32px;
    height: 32px;
    color: var(--color-muted-foreground);
  }

  .notfound-heading {
    margin: 0;
    font-size: 20px;
    font-weight: 600;
    line-height: 1.2;
    /* Long headings wrap; nothing here truncates. */
    overflow-wrap: anywhere;
  }

  .notfound-body {
    margin: 0;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
    color: var(--color-muted-foreground);
    overflow-wrap: anywhere;
  }

  .notfound-destinations {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: center;
    gap: 8px 16px;
    margin-top: 8px;
  }

  .destination {
    font-size: 14px;
    color: var(--color-primary);
    text-decoration: none;
    overflow-wrap: anywhere;
  }

  .destination:hover,
  .destination:focus-visible {
    text-decoration: underline;
  }
</style>
