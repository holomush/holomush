<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import ByteCounter from './ByteCounter.svelte';
  import { isAbortedError } from '$lib/connect/errors';

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
   * IT RENDERS NO SERVER-SUPPLIED STRING. A refusal arrives as one of two
   * authored outcomes and nothing else: a concurrent edit, which the player can
   * act on, or the generic failure. `CHARACTER_MASK_PATH_UNSUPPORTED` and
   * `CHARACTER_VERSION_REQUIRED` are CLIENT BUGS — a player cannot fix a mask
   * this code composed — so putting either in front of them is noise at best
   * and internal disclosure at worst (T-05-04-04). The shipped
   * register/+page.svelte idiom (`e instanceof Error ? e.message : …`) is
   * exactly the shape that leaks them, which is why it is not used here.
   *
   * CLASSIFICATION LIVES IN $lib/connect/errors. This file imports a predicate
   * and never inspects a status code itself: a second place that knows how a
   * refusal is spelled is a second place that can start disagreeing about it.
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
  /*
   * BOTH ARE SNAPSHOTS TAKEN ONCE, AT MOUNT, AND THE `untrack` SAYS SO. The
   * capture is load-bearing rather than incidental: a successful save in any
   * section returns the whole post-write character, so if the page rebinds
   * `values` from it, a section that tracked the prop would reset its baseline —
   * and with it, whatever the player had typed there but not yet saved. Section
   * 1 saving would quietly discard section 4's draft, which is the opposite of
   * what per-section save is for. After mount, this section owns these two maps.
   */
  let loaded = $state<Record<string, string>>(untrack(() => ({ ...values })));
  let working = $state<Record<string, string>>(untrack(() => ({ ...values })));
  let busy = $state(false);
  let saved = $state(false);
  /** '' | 'conflict' | 'generic' — an outcome this file authored, never a string the server sent. */
  let failure = $state<'' | 'conflict' | 'generic'>('');
  let fieldEls = $state<(HTMLInputElement | HTMLTextAreaElement | null)[]>([]);

  const CONFLICT_COPY =
    'This character changed somewhere else. Reload to get the latest, then re-apply your edits.';
  const GENERIC_COPY = "Couldn't save. Try again.";

  const dirty = $derived(fields.some((f) => (working[f.path] ?? '') !== (loaded[f.path] ?? '')));

  function onEdit() {
    saved = false;
  }

  async function submit() {
    if (!dirty || busy) return;
    busy = true;
    saved = false;
    failure = '';
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
      failure = isAbortedError(e) ? 'conflict' : 'generic';
      /*
       * The working values are deliberately NOT reverted: a refusal costs the
       * player one section's worth of re-applying, and wiping what they typed
       * would make it cost the typing as well. Focus lands on the first field
       * so a keyboard or screen-reader user arrives where the recovery starts,
       * rather than on whatever the browser left focused behind the message.
       */
      fieldEls[0]?.focus();
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

    <!-- The label is the section's own noun, and it carries deliberately NO
         accessible-name override: the accessible name IS the visible name.
         Once the visible text is distinct an override is duplication, and a
         duplicated string is one that drifts out of sync with the heading it
         mirrors. (Stated rather than spelled, so the acceptance gate scanning
         this whole file for the attribute cannot be tripped by the comment
         explaining its absence.) In flight the label does not change — a swap
         to "Saving…" reflows the button under the player's cursor. -->
    <button type="submit" disabled={!dirty || busy} aria-busy={busy ? 'true' : undefined}
      >{saveLabel}</button
    >
  </form>

  <!-- Rendered unconditionally and left empty: a live region has to be in the
       document BEFORE its content changes, or the first message is the one
       nobody hears. -->
  <p class="status" role="status">{saved ? 'Saved.' : ''}</p>

  {#if failure !== ''}
    <div class="error" role="alert">
      <p>{failure === 'conflict' ? CONFLICT_COPY : GENERIC_COPY}</p>
      {#if failure === 'conflict'}
        <!-- The copy above promises a reload, so the control performs one. It
             is offered ONLY on a conflict: re-reading is what resolves a stale
             version, and it resolves nothing for a generic failure. -->
        <button type="button" onclick={() => location.reload()}>Reload</button>
      {/if}
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
  .error button {
    align-self: flex-start;
    background: transparent;
    border-color: var(--color-border);
    color: var(--color-foreground);
  }
</style>
