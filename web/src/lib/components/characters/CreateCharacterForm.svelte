<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import ByteCounter from './ByteCounter.svelte';

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
  const GENERIC_COPY = "Couldn't create the character. Try again.";

  const MAX_NAME_LENGTH = 32;
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

  const nameLength = $derived(name.length);

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
      error = e instanceof Error ? e.message : GENERIC_COPY;
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
    <p class="rule">{NAME_RULE}</p>
    <p class="counter" data-testid="name-counter">{nameLength} / {MAX_NAME_LENGTH}</p>
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
