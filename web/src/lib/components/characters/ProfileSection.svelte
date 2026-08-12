<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import ByteCounter from './ByteCounter.svelte';

  /**
   * One authoring section: its own fields, its own Save, its own status region
   * and its own error region.
   *
   * IT IS THE UNIT OF FAILURE, which is the entire reason it exists. The two
   * write RPCs guard the SAME characters.version with NO transaction spanning
   * them, so a whole-form save would be a two-call chain whose second call can
   * lose a concurrent edit after the first has already committed — a partial
   * save with a stale form and nothing to roll back. Scoping save, status and
   * error to one section makes the interface's shape agree with the system's,
   * and a conflict then costs the player one section of typing instead of
   * twelve (D-93).
   *
   * IT KNOWS NOTHING ABOUT `version`. The page owns exactly one version cell and
   * threads it through the `save` callback; a section holding its own copy would
   * reintroduce the several-places-to-go-stale problem the single cell removes.
   *
   * PLACEHOLDER ERROR HANDLING — this revision copies the shipped
   * register/+page.svelte idiom (`e instanceof Error ? e.message : …`), which
   * renders whatever the server said straight at the player and draws no
   * distinction between a concurrent edit and anything else. That is the
   * in-repo precedent an implementer reaches for and it is what the sibling
   * test file exists to reject. Task 2's GREEN step replaces it.
   */

  type Field = {
    /**
     * The section's key for this field. For four of the five sections it is
     * also the mask path the page sends; for the in-world description — which
     * writes a column rather than a set of rows and therefore takes no mask —
     * it is only a key. This component never learns which case it is in.
     */
    path: string;
    label: string;
    name: string;
    kind: 'line' | 'prose';
    maxBytes: number;
  };

  let {
    heading,
    saveLabel,
    fields,
    values,
    save,
  }: {
    heading: string;
    saveLabel: string;
    fields: Field[];
    values: Record<string, string>;
    save: (paths: string[], values: Record<string, string>) => Promise<unknown>;
  } = $props();

  /*
   * `loaded` is this section's own copy of what the server last confirmed, and
   * `working` is what the player has typed. Dirty is the difference between the
   * two — never a flag set by an input handler, which would survive an edit that
   * put a field back the way it started.
   */
  let loaded = $state<Record<string, string>>({ ...values });
  let working = $state<Record<string, string>>({ ...values });
  let busy = $state(false);
  let saved = $state(false);
  let failureText = $state('');
  let fieldEls = $state<(HTMLInputElement | HTMLTextAreaElement | null)[]>([]);

  const dirty = $derived(fields.some((f) => (working[f.path] ?? '') !== (loaded[f.path] ?? '')));

  function onEdit() {
    saved = false;
  }

  async function submit() {
    if (!dirty || busy) return;
    busy = true;
    saved = false;
    failureText = '';
    try {
      /*
       * EVERY path this section declares, not only the dirty ones: the mask is
       * what selects what gets written, so an untouched field still travels
       * under its own path. And only this section's paths — never the full
       * twelve, because a mask carrying a path the player is not editing is a
       * write they did not ask for.
       */
      await save(
        fields.map((f) => f.path),
        { ...working },
      );
      loaded = { ...working };
      saved = true;
    } catch (e) {
      failureText = e instanceof Error ? e.message : "Couldn't save. Try again.";
    } finally {
      busy = false;
    }
  }
</script>

<section class="section">
  <h2>{heading}</h2>
  <form
    onsubmit={(e) => {
      e.preventDefault();
      void submit();
    }}
  >
    {#each fields as field, i (field.path)}
      <div class="field">
        <label for={`field-${field.name}`}>{field.label}</label>
        {#if field.kind === 'prose'}
          <textarea
            id={`field-${field.name}`}
            name={field.name}
            rows="6"
            bind:value={working[field.path]}
            bind:this={fieldEls[i]}
            oninput={onEdit}
          ></textarea>
        {:else}
          <input
            id={`field-${field.name}`}
            name={field.name}
            type="text"
            bind:value={working[field.path]}
            bind:this={fieldEls[i]}
            oninput={onEdit}
          />
        {/if}
        <ByteCounter value={working[field.path] ?? ''} maxBytes={field.maxBytes} />
      </div>
    {/each}

    <!-- The label is the section's own noun, and there is deliberately NO
         aria-label: once the visible text is distinct the override is
         duplication, and a duplicated string drifts out of sync with the
         heading it mirrors. In flight the label does not change — a swap to
         "Saving…" reflows the button under the player's cursor. -->
    <button type="submit" disabled={!dirty || busy} aria-busy={busy ? 'true' : undefined}
      >{saveLabel}</button
    >
  </form>

  <!-- Rendered unconditionally and left empty: a live region has to be in the
       document BEFORE its content changes, or the first message is the one
       nobody hears. -->
  <p class="status" role="status">{saved ? 'Saved.' : ''}</p>

  {#if failureText !== ''}
    <div class="error" role="alert">
      <p>{failureText}</p>
    </div>
  {/if}
</section>

<style>
  .section {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 16px;
    border: 1px solid var(--color-border);
    border-radius: 12px;
    background: var(--color-card);
  }
  .section h2 {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--color-status-text);
  }
  form {
    display: flex;
    flex-direction: column;
    gap: 16px;
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
  input,
  textarea {
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
  textarea {
    resize: vertical;
  }
  input:focus-visible,
  textarea:focus-visible,
  button:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
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
  .status {
    margin: 0;
    font-size: 14px;
    font-weight: 400;
    line-height: 1.5;
    color: var(--color-status-text);
  }
  .status:empty {
    display: none;
  }
  .error {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
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
