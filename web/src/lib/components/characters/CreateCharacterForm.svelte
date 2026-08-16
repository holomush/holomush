<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { ConnectError } from '@connectrpc/connect';
  import ByteCounter from './ByteCounter.svelte';
  import { isAlreadyExistsError, isInvalidArgumentError } from '$lib/connect/errors';

  /**
   * The six-field creation card: one section, one Save, submit-and-report.
   *
   * IT PERFORMS EXACTLY ONE LOCAL CHECK — that a name was entered. Everything
   * else about whether a name is admissible belongs to the server, and this
   * file mirrors none of it. Reimplementing any part of the name pipeline here
   * would duplicate a security-adjacent transform in a second language and
   * create two sources of truth for the value the unique index depends on
   * (D-88). There is likewise no lookup of whether a name is free, debounced or
   * otherwise: the answer would be computed at a different moment than the
   * insert that acts on it, so it could only ever be a guess dressed as a fact.
   *
   * A REJECTION CLEARS NOTHING. All six values survive and focus returns to the
   * name field. This is a precondition of the submit-and-report design rather
   * than a courtesy: the design accepts that a collision is learned only after
   * submitting, and that trade holds only while the round trip is cheap.
   *
   * ONLY ONE CODE'S MESSAGE REACHES THE PLAYER. `InvalidArgument` carries
   * strings the name pipeline authored FOR players — including the two separate
   * wordings for a submission with no visible characters, where "please enter a
   * name" would tell someone to do the thing they believe they just did. That
   * is a deliberate, scoped exception to never rendering a server string. Every
   * other code renders authored copy with no message and no code.
   *
   * TWO CAPS, TWO UNITS, NOT INTERCHANGEABLE. The name is bounded in code
   * points at 32 (internal/charname/syntax); the five short values are bounded
   * in BYTES at 100, which is what ByteCounter measures. They agree on ASCII
   * and disagree on everything else, so conflating them is invisible in testing
   * and wrong for every player who writes outside it.
   */

  let {
    submit,
  }: {
    submit: (fields: {
      name: string;
      pronouns: string;
      concept: string;
      species: string;
      age: string;
      faction: string;
    }) => Promise<unknown>;
  } = $props();

  const NAME_RULE =
    'Letters and single spaces, 2–32 characters. Full-width characters, invisible marks and extra spaces are folded, so the name you get may differ slightly from what you type.';
  const NAME_REQUIRED = 'Enter a character name.';
  const TAKEN_COPY = 'That name is taken. Try another.';
  const INVALID_FALLBACK = "That name can't be used here.";
  const GENERIC_COPY = "Couldn't create the character. Try again.";

  /** Code points, per internal/charname/syntax MaxNameLength — NOT bytes. */
  const MAX_NAME_RUNES = 32;
  /** Bytes, per the facade's short-field cap — NOT code points. */
  const SHORT_CAP_BYTES = 100;

  let name = $state('');
  let pronouns = $state('');
  let concept = $state('');
  let species = $state('');
  let age = $state('');
  let faction = $state('');

  let busy = $state(false);
  let error = $state('');
  let nameEl = $state<HTMLInputElement | null>(null);

  // Spread-then-count yields CODE POINTS. The string's own length is UTF-16
  // code units, which agrees with this for ASCII and doubles for anything on an
  // astral plane — a counter that is right in testing and wrong in use.
  const nameRunes = $derived([...name].length);

  // Same DISPLAY rule ByteCounter applies to the five sibling fields
  // (`shown = value >= max * 0.8`, UI-SPEC:491): numeric chrome appears only
  // within 20% of the cap. Without this the name was the one field of six
  // carrying a counter from first paint, which contradicts UI-SPEC:423
  // ("Authoring form, all fields blank | No copy"). The unit is RUNES here and
  // bytes there, so the gate is restated rather than shared.
  const nameCounterShown = $derived(nameRunes >= MAX_NAME_RUNES * 0.8);

  /**
   * Turns a refusal into player-facing copy.
   *
   * Classification is by CODE, through the shared predicates — never by
   * matching a message string, which would couple this file to wording the
   * server is free to reword.
   *
   * The `InvalidArgument` arm reads `rawMessage`, not `message`: ConnectError's
   * `message` is the same string with a `[code]` prefix bolted on, so rendering
   * it would put wire vocabulary in front of a player in the one place the
   * design promised to pass the server's own words through untouched. And the
   * words really are untouched — nothing is appended. The pipeline's confusable
   * refusal names no colliding character on purpose, and a helpful addition
   * here would rebuild the enumeration oracle §6.1.2 closes.
   */
  function classify(e: unknown): string {
    if (isAlreadyExistsError(e)) return TAKEN_COPY;
    if (isInvalidArgumentError(e)) {
      const authored = e instanceof ConnectError ? e.rawMessage.trim() : '';
      return authored === '' ? INVALID_FALLBACK : authored;
    }
    return GENERIC_COPY;
  }

  async function handleSubmit() {
    if (name.trim() === '') {
      error = NAME_REQUIRED;
      nameEl?.focus();
      return;
    }
    error = '';
    busy = true;
    try {
      await submit({ name, pronouns, concept, species, age, faction });
    } catch (e) {
      error = classify(e);
      // Nothing above resets a field. The player re-reads one line and presses
      // the button again; they do not retype six of them. Focus lands on the
      // name because that is where every refusal this form reports originates.
      nameEl?.focus();
    } finally {
      busy = false;
    }
  }
</script>

<form
  class="card"
  onsubmit={(e) => {
    e.preventDefault();
    void handleSubmit();
  }}
>
  <div class="field">
    <label for="field-name">Name</label>
    <input id="field-name" name="characterName" type="text" bind:value={name} bind:this={nameEl} />
    <!-- Always shown, with no interaction gate. It states the rule and the
         CLASS of rewrite; it never predicts the result for the entered string,
         because that would require the client-side transform D-88 forbids. -->
    <p class="rule">{NAME_RULE}</p>
    {#if nameCounterShown}
      <p class="counter" data-testid="name-counter" aria-live="polite">
        {nameRunes} / {MAX_NAME_RUNES}
      </p>
    {/if}
  </div>

  <div class="field">
    <label for="field-pronouns">Pronouns</label>
    <input id="field-pronouns" name="pronouns" type="text" bind:value={pronouns} />
    <ByteCounter value={pronouns} maxBytes={SHORT_CAP_BYTES} />
  </div>

  <div class="field">
    <label for="field-concept">Concept</label>
    <input id="field-concept" name="concept" type="text" bind:value={concept} />
    <ByteCounter value={concept} maxBytes={SHORT_CAP_BYTES} />
  </div>

  <div class="field">
    <label for="field-species">Species</label>
    <input id="field-species" name="species" type="text" bind:value={species} />
    <ByteCounter value={species} maxBytes={SHORT_CAP_BYTES} />
  </div>

  <div class="field">
    <label for="field-age">Age</label>
    <input id="field-age" name="age" type="text" bind:value={age} />
    <ByteCounter value={age} maxBytes={SHORT_CAP_BYTES} />
  </div>

  <div class="field">
    <label for="field-faction">Faction</label>
    <input id="field-faction" name="faction" type="text" bind:value={faction} />
    <ByteCounter value={faction} maxBytes={SHORT_CAP_BYTES} />
  </div>

  <button type="submit" disabled={busy} aria-busy={busy ? 'true' : undefined}>Create character</button>

  {#if error !== ''}
    <div class="error" role="alert"><p>{error}</p></div>
  {/if}
</form>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 16px;
    border: 1px solid var(--color-border);
    border-radius: 12px;
    background: var(--color-card);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  label {
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    color: var(--color-status-text);
  }
  input {
    width: 100%;
    padding: 8px 12px;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    background: var(--color-input-background, var(--color-background));
    color: var(--color-foreground);
    font: inherit;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
  }
  input:focus-visible,
  button:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
  .rule {
    margin: 0;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
    color: var(--color-status-text);
  }
  .counter {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    color: var(--color-status-text);
  }
  button {
    align-self: flex-start;
    min-height: 44px;
    padding: 8px 16px;
    border: 1px solid transparent;
    border-radius: 8px;
    background: var(--color-primary);
    color: var(--color-primary-foreground);
    font-size: 14px;
    font-weight: 600;
    line-height: 1.5;
    cursor: pointer;
  }
  button:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .error {
    padding: 12px;
    border: 1px solid var(--color-destructive);
    border-radius: 8px;
  }
  .error p {
    margin: 0;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
    color: var(--color-destructive);
  }
</style>
