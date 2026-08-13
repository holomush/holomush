<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { page } from '$app/stores';
  import ProfileSection from '$lib/components/characters/ProfileSection.svelte';
  import {
    getMyCharacter,
    updateCharacterDescription,
    updateCharacterProfile,
  } from '$lib/characters/client';
  import type { OwnCharacter } from '$lib/connect/holomush/characteraccess/v1/characteraccess_pb';

  /*
    The owner's authoring surface: five sections, five Saves, five error regions.

    WHY FIVE AND NOT ONE. UpdateCharacterProfile writes twelve entity_properties
    rows and UpdateCharacterDescription writes the characters.description column.
    They are separate RPCs, they guard the SAME characters.version, and NO
    transaction spans them. One Save over both would therefore be a two-call
    chain whose second call can be refused for a concurrent edit AFTER the first
    has already committed — a half-applied form with a stale version and nothing
    to roll back. Splitting the surface along the seam the system already has
    makes that state stop existing, and makes a conflict cost one section of
    typing rather than twelve (D-93).

    ONE LOAD, NO VIEW/EDIT SPLIT. GetMyCharacter returns the id, the name, the
    description, the whole profile map, the media, the status and the version, so
    one call feeds the page. A separate read view would render the identical
    dataset from a second load — §8.12 ships no per-field audience controls in
    v0.13, so there is nothing an owner could see in one mode and not the other —
    and would give `version` a second place to go stale (D-92).

    (The comments in this file state the affordances this surface must not grow
    rather than naming them, so the acceptance gates that scan the whole file for
    that vocabulary cannot be tripped by the prose explaining their absence. A
    gate that has to be suppressed to stay green stops being a gate — the same
    reason PublicProfile.svelte words its own header that way.)
  */

  const id = $derived($page.params.id ?? '');

  let loading = $state(true);
  let failed = $state(false);
  let character = $state<OwnCharacter | undefined>(undefined);

  /*
    THE ONE VERSION CELL. Every section sends this as expected_version and every
    successful response replaces it from the returned OwnCharacter. It is never
    re-read from the server just before a save to get a fresher number: that
    would satisfy the guard with a value the player's screen never showed, which
    turns every stale client into a silent last-write-wins — precisely what the
    facade's own doc block says the guard exists to prevent.
  */
  let version = $state(0);

  const profile = $derived(character?.profile ?? {});

  // 05-UI-SPEC's authored sentence, rendered once, above everything. It is a
  // standing fact rather than an alert: muted body text, no icon, no border, no
  // callout, and never the cursor colour.
  const NOT_RETROACTIVE =
    'Privacy here is not retroactive. Editing or clearing a field changes what this profile shows from now on — it cannot recall what visitors have already read.';

  /*
    The five sections' field descriptors.

    The caps are the shipped world constants, transcribed from the facade's
    allowlist (internal/grpc/characteraccess_write.go:143-178): the seven short
    single-line fields cap at world.MaxNameLength = 100 BYTES, and the five long
    multi-paragraph fields plus the description at world.MaxDescriptionLength =
    4000 BYTES. Both are byte counts, which is why ByteCounter measures bytes.

    The union of sections 1, 3, 4 and 5 is EXACTLY the twelve paths in
    updateCharacterProfileMaskablePaths, each appearing once. No path is in two
    sections and none is unassigned — a path in two sections would let one
    section's Save overwrite another's field, and an unassigned one would be
    uneditable.
  */
  const SHORT = 100;
  const LONG = 4000;

  const IDENTITY_FIELDS = [
    { path: 'profile.pronouns', label: 'Pronouns', name: 'pronouns', kind: 'line' as const, maxBytes: SHORT },
    { path: 'profile.concept', label: 'Concept', name: 'concept', kind: 'line' as const, maxBytes: SHORT },
    { path: 'profile.species', label: 'Species', name: 'species', kind: 'line' as const, maxBytes: SHORT },
    { path: 'profile.age', label: 'Age', name: 'age', kind: 'line' as const, maxBytes: SHORT },
    { path: 'profile.faction', label: 'Faction', name: 'faction', kind: 'line' as const, maxBytes: SHORT },
  ];

  /*
    Section 2 carries NO mask path. Its `path` is a local key only: the
    description is a column, not a set of rows, and UpdateCharacterDescription
    takes no update_mask at all. That is exactly why it is its own section rather
    than a thirteenth path on the profile call.
  */
  const DESCRIPTION_FIELDS = [
    {
      path: 'description',
      label: 'What others see when they look at your character',
      name: 'description',
      kind: 'prose' as const,
      maxBytes: LONG,
    },
  ];

  const APPEARANCE_FIELDS = [
    { path: 'profile.appearance', label: 'Appearance', name: 'appearance', kind: 'prose' as const, maxBytes: LONG },
    { path: 'profile.personality', label: 'Personality', name: 'personality', kind: 'prose' as const, maxBytes: LONG },
    { path: 'profile.biography', label: 'Biography', name: 'biography', kind: 'prose' as const, maxBytes: LONG },
  ];

  const HOOKS_FIELDS = [
    { path: 'profile.rumors', label: 'Rumours & hooks', name: 'rumors', kind: 'prose' as const, maxBytes: LONG },
    { path: 'profile.currently', label: 'Currently', name: 'currently', kind: 'line' as const, maxBytes: SHORT },
  ];

  /*
    profile.rp_preferences is OUT-OF-CHARACTER text about the player, and it
    travels ONLY as that mask path, which the facade maps to an entity_properties
    row name. It has no route to the JSONB settings column on the characters
    table, which shares a word with it and nothing else (§7.2 naming collision).
    Nothing on this surface addresses that column: the only write it can reach is
    the one the mask path names.
  */
  const OOC_FIELDS = [
    { path: 'profile.rp_preferences', label: 'RP preferences', name: 'rp_preferences', kind: 'prose' as const, maxBytes: LONG },
    { path: 'profile.timezone', label: 'Time zone', name: 'timezone', kind: 'line' as const, maxBytes: SHORT },
  ];

  type Field = (typeof IDENTITY_FIELDS)[number] | (typeof APPEARANCE_FIELDS)[number];

  /** The section's loaded values, read straight off the response's profile map. */
  function loadedValues(fields: Field[]): Record<string, string> {
    const out: Record<string, string> = {};
    for (const f of fields) out[f.path] = profile[f.path] ?? '';
    return out;
  }

  /*
    THE LOAD FOLLOWS THE PARAM, NOT THE MOUNT. SvelteKit reuses one component
    instance across a client-side navigation between two params of the SAME
    route, so a load anchored to `onMount` runs once and never again. On THIS
    surface the consequence is worse than a stale render: `version` and every
    section's loaded snapshot would still belong to the previous character
    while each Save sends `characterId: id` for the new one — a write guarded
    by a version cell that was never read from the row it targets. Reading `id`
    inside the effect is what makes the load re-run for every param, first one
    included.

    `inFlight` is the LAST-REQUEST TOKEN, for the same reason: network order is
    not param order, and a slow response for the previous character landing
    after the new one's would seat that stale version.
  */
  let inFlight = $state('');

  $effect(() => {
    void load(id);
  });

  async function load(target: string) {
    inFlight = target;
    loading = true;
    failed = false;
    character = undefined;
    try {
      const c = await getMyCharacter(target);
      if (inFlight !== target) return;
      if (c === undefined) {
        failed = true;
        return;
      }
      character = c;
      version = c.version;
    } catch {
      if (inFlight !== target) return;
      failed = true;
    } finally {
      if (inFlight === target) loading = false;
    }
  }

  /*
    The four profile sections share one callback because they differ only in
    their mask. `paths` is the section's own set and nothing else — never the
    full twelve, and never the bare path `profile`, which the facade does NOT
    expand into a subtree (§9.5 rule 2).

    The unlisted fields go out as the empty string and are ignored, because the
    MASK selects what is written; the values are only consulted for the paths the
    mask names.

    IT DOES NOT CATCH. The refusal is what ProfileSection classifies into the
    concurrent-edit copy or the generic copy, and swallowing it here would leave
    the section reporting a save that did not happen.
  */
  async function saveProfile(paths: string[], values: Record<string, string>) {
    const v = (p: string) => values[p];
    const updated = await updateCharacterProfile({
      characterId: id,
      expectedVersion: version,
      paths,
      pronouns: v('profile.pronouns'),
      concept: v('profile.concept'),
      species: v('profile.species'),
      age: v('profile.age'),
      faction: v('profile.faction'),
      appearance: v('profile.appearance'),
      personality: v('profile.personality'),
      biography: v('profile.biography'),
      rumors: v('profile.rumors'),
      currently: v('profile.currently'),
      rpPreferences: v('profile.rp_preferences'),
      timezone: v('profile.timezone'),
    });
    return adopt(updated);
  }

  async function saveDescription(_paths: string[], values: Record<string, string>) {
    const updated = await updateCharacterDescription({
      characterId: id,
      expectedVersion: version,
      description: values['description'] ?? '',
    });
    return adopt(updated);
  }

  /**
   * Takes the fresh version off the post-write character. The next Save of ANY
   * section carries it, which is what keeps five independent forms honest about
   * one row's concurrency token.
   */
  function adopt(updated: OwnCharacter | undefined): OwnCharacter | undefined {
    if (updated !== undefined) {
      character = updated;
      version = updated.version;
    }
    return updated;
  }
</script>

<div class="page">
  {#if loading}
    <p class="centered" role="status">Loading…</p>
  {:else if failed}
    <section class="notice" role="alert">
      <p>Couldn't load this character. Try again.</p>
      <button type="button" onclick={() => void load(id)}>Try again</button>
    </section>
  {:else if character !== undefined}
    <header class="head">
      <h1>{character.name}</h1>
      <!-- Keyed on the character ID, never the name (§9.2): the name is
           mutable-in-principle and is not the URL key the public route reads. -->
      <a class="public-link" href={`/c/${character.id}`}>View public profile →</a>
    </header>

    <p class="standing">{NOT_RETROACTIVE}</p>

    <ProfileSection
      heading="Identity"
      saveLabel="Save identity"
      fields={IDENTITY_FIELDS}
      values={loadedValues(IDENTITY_FIELDS)}
      save={saveProfile}
    />

    <ProfileSection
      heading="In-world description"
      saveLabel="Save in-world description"
      fields={DESCRIPTION_FIELDS}
      values={{ description: character.description }}
      save={saveDescription}
    />

    <ProfileSection
      heading="Appearance & history"
      saveLabel="Save appearance & history"
      fields={APPEARANCE_FIELDS}
      values={loadedValues(APPEARANCE_FIELDS)}
      save={saveProfile}
    />

    <ProfileSection
      heading="Hooks & current"
      saveLabel="Save hooks & current"
      fields={HOOKS_FIELDS}
      values={loadedValues(HOOKS_FIELDS)}
      save={saveProfile}
    />

    <ProfileSection
      heading="Out of character"
      saveLabel="Save out of character"
      fields={OOC_FIELDS}
      values={loadedValues(OOC_FIELDS)}
      save={saveProfile}
    />
  {/if}
</div>

<style>
  .page {
    display: flex;
    flex-direction: column;
    /* xl between authoring sections. */
    gap: 24px;
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
  .head {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .head h1 {
    margin: 0;
    font-size: 20px;
    font-weight: 600;
    line-height: 1.2;
  }
  .public-link {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    color: var(--color-primary);
    font-size: 14px;
    font-weight: 600;
    line-height: 1.5;
    text-decoration: none;
  }
  .public-link:hover {
    text-decoration: underline;
  }
  .public-link:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
  /* Muted body text, and nothing else: no border, no background, no icon, no
     accent. It states a property of the system, so dressing it as a warning
     would tell the player to act on something there is no action for. */
  .standing {
    margin: 0;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
    color: var(--color-status-text);
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
  .notice p {
    margin: 0;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
    color: var(--color-status-text);
  }
  .notice button {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 8px 12px;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    background: transparent;
    /* Not accent. UI-SPEC:134 closes the accent reserved-for list and a retry
       control is not on it; the roster's equivalent retry is already
       `--color-foreground`. Deliberately NOT the same as `.public-link` above,
       which IS on the list (item 5). */
    color: var(--color-foreground);
    font-size: 14px;
    font-weight: 600;
    line-height: 1.5;
    cursor: pointer;
  }
  .notice button:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
</style>
