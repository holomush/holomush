<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import CreateCharacterForm from '$lib/components/characters/CreateCharacterForm.svelte';
  import { submitCreateCharacter } from '$lib/characters/createFlow';

  /**
   * The creation route (D-87).
   *
   * A THIN SHELL AND NOTHING ELSE. It owns no field value, no error copy and no
   * classification: the form owns the first, and the flow owns the rest. The
   * page's whole job is the heading, the column and the wiring between them —
   * so there is exactly one place a create's outcome is decided, and it is not
   * a route file.
   *
   * It sits under `(authed)` because creation is an owner operation. The static
   * segment `new` outranks the sibling `[id]` route, so no route-matching
   * configuration is needed to keep `/characters/new` off the profile page.
   *
   * The submit reaches a typed RPC through the flow layer. A form submit is a
   * machine-initiated structural write, and string-building it into the human
   * text-command path would route it through a parser meant for verbs a player
   * types (.claude/rules/gateway-boundary.md).
   *
   * The column matches the roster's — same max width, same heading treatment,
   * one breakpoint at 768px — so the two owner surfaces read as one family.
   * A media query rather than a container query: this phase ships no shell, so
   * the content box and the viewport are the same thing.
   */
</script>

<div class="page">
  <div class="column">
    <h1>Create a character</h1>
    <CreateCharacterForm submit={submitCreateCharacter} />
  </div>
</div>

<style>
  .page {
    display: flex;
    justify-content: center;
    align-items: flex-start;
    min-height: calc(100vh - 36px);
    padding: 16px;
  }
  .column {
    width: 100%;
    max-width: 720px;
    display: flex;
    flex-direction: column;
    gap: 24px;
  }
  h1 {
    margin: 0;
    font-size: 20px;
    font-weight: 600;
    line-height: 1.2;
    color: var(--color-primary);
  }
  @media (min-width: 768px) {
    .page {
      padding: 48px;
    }
  }
</style>
