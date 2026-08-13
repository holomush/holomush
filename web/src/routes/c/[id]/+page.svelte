<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { page } from '$app/stores';
  import PublicProfile from '$lib/components/characters/PublicProfile.svelte';
  import { getCharacterProfile } from '$lib/characters/client';
  import { isNotFoundError } from '$lib/connect/errors';
  import type { PublicCharacter } from '$lib/connect/holomush/characteraccess/v1/characteraccess_pb';

  /*
    The first route the app serves outside `(authed)` — a sibling of `login` and
    `register`, deliberately so, because `(authed)/+layout.ts`'s redirect leg is
    exactly what must never be reachable from a shared profile link (D-84).

    THERE IS NO `+page.ts`. The root layout already declares `ssr = false` and
    `adapter-static` runs with `fallback: 'index.html'`, so every path already
    answers HTTP 200 with the SPA shell: route-level indistinguishability
    (§8.7) is structural rather than something this route implements. A
    `load()` here would only add a second place the not-found decision could be
    made.

    IT SENDS NO SESSION HANDLING OF ANY KIND. The gateway forwards an empty
    player_session_token when the header is absent and the facade degrades that
    to the anonymous rung, so the identical call serves a logged-out visitor and
    a signed-in one. Nothing here reads, checks or refreshes a session, and the
    page carries no sign-in invitation of its own: the root layout's TopBar
    already renders Login / Register unconditionally for every anonymous viewer
    on every page (D-85). A profile-LOCAL invitation would vary with the page it
    sat on, which is what makes it an oracle for which profiles are populated.
  */

  const id = $derived($page.params.id ?? '');

  let loading = $state(true);
  let character = $state<PublicCharacter | undefined>(undefined);
  /*
    EXACTLY ONE FAILURE FLAG, set by exactly one predicate. §9.6 answers "no
    such character" and "below this viewer's reachability floor" with one code
    and one message literal on purpose; a second branch here would rebuild the
    distinction the single code exists to collapse. The rendered markup is the
    same whichever cause produced the status, because this component never
    learns which one did.
  */
  let missing = $state(false);
  let failed = $state(false);

  /*
    THE FETCH FOLLOWS THE PARAM, NOT THE MOUNT. SvelteKit reuses one component
    instance across a client-side navigation between two params of the SAME
    route, so a fetch anchored to `onMount` runs once and never again: `/c/A` →
    `/c/B` would leave A's profile on screen while the URL and `id` both say B.
    Reading `id` inside the effect is what establishes the dependency, so the
    load re-runs for every param the route is asked for — including the first.

    `inFlight` is the LAST-REQUEST TOKEN. Two loads can overlap, and network
    order is not param order: without it, a slow response for A landing after
    B's fast one would overwrite B's state with A's profile — the exact
    wrong-character render the effect exists to prevent. Every write after the
    await is gated on the token still naming this request.
  */
  let inFlight = $state('');

  $effect(() => {
    void load(id);
  });

  async function load(target: string) {
    inFlight = target;
    loading = true;
    missing = false;
    failed = false;
    character = undefined;
    try {
      const loaded = await getCharacterProfile(target);
      if (inFlight !== target) return;
      character = loaded;
      if (loaded === undefined) {
        missing = true;
      }
    } catch (e) {
      if (inFlight !== target) return;
      if (isNotFoundError(e)) {
        missing = true;
      } else {
        failed = true;
      }
    } finally {
      if (inFlight === target) loading = false;
    }
  }
</script>

<div class="page">
  {#if loading}
    <!-- One centered line. No skeleton: a field-shaped placeholder
         pre-announces a response shape the response may not have, on the one
         surface whose whole constraint is that it must not signal how much it
         is holding back. -->
    <p class="centered" role="status">Loading…</p>
  {:else if missing}
    <!-- Inline, not an error boundary (D-95); Phase 6 owns the single root
         boundary and this route adopts it when it lands. It echoes neither the
         requested identifier — echoing the string confirms it was routed — nor
         any reason, because naming one is precisely the distinction §9.6
         collapses into a single code. -->
    <section class="notice">
      <h1>Not found</h1>
      <p>We couldn't find that page.</p>
      <a href="/">Home</a>
    </section>
  {:else if failed}
    <section class="notice" role="alert">
      <p>Something went wrong. Try again.</p>
      <button type="button" onclick={() => void load(id)}>Try again</button>
    </section>
  {:else if character !== undefined}
    <PublicProfile {character} />
  {/if}
</div>

<style>
  /* The page body is UNPADDED at the block level: the card and each section
     carry their own margin, so sections grow BELOW the card rather than sharing
     a document flow with it. That layering is the structural mechanism by which
     a sparse profile reads as complete rather than truncated. */
  .page {
    width: 100%;
    max-width: 720px;
    margin: 0 auto;
    padding: 32px 16px;
  }
  @media (min-width: 768px) {
    .page {
      padding: 32px 48px;
    }
  }
  .centered {
    margin: 0;
    text-align: center;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
    color: var(--color-status-text);
  }
  .notice {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
    padding: 16px;
    border: 1px solid var(--color-border);
    border-radius: 12px;
    background: var(--color-card);
  }
  .notice h1 {
    margin: 0;
    font-size: 20px;
    font-weight: 600;
    line-height: 1.2;
  }
  .notice p {
    margin: 0;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
    color: var(--color-status-text);
  }
  .notice a,
  .notice button {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 8px 12px;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    background: transparent;
    color: var(--color-primary);
    font-size: 14px;
    font-weight: 600;
    line-height: 1.5;
    text-decoration: none;
    cursor: pointer;
  }
  .notice a:focus-visible,
  .notice button:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
</style>
