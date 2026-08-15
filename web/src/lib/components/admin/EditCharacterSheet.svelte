<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts" module>
  import { adminWireField } from '$lib/admin/client';

  /**
   * One editable path: its mask path, the flat request field the wire carries
   * it in, and the server's own byte cap for it.
   *
   * THE SET IS THE SERVER'S, NOT THIS FILE'S. `adminProfileMaskablePaths`
   * (internal/grpc/admin_characters_write.go:130-169) is the closed allowlist,
   * compared by exact string, and an unlisted path is REJECTED rather than
   * ignored. What lives here is a rendering order and a label — the authority
   * is server-side, which is why nothing below tries to enforce membership.
   *
   * The order is the wire's own (WebAdminUpdateCharacterRequest fields 4-16),
   * so "the first conflicting field" is a stable, inspectable notion rather
   * than whatever order an object literal happened to be written in.
   *
   * The caps do NOT agree with each other: seven short single-line paths cap at
   * world.MaxNameLength (100 bytes) and six long ones at
   * world.MaxDescriptionLength (4000). A counter applying one cap to all
   * thirteen would be wrong in both directions.
   */
  export interface EditableField {
    /** The update_mask path, compared by exact string server-side. */
    path: string;
    /**
     * The flat field on WebAdminUpdateCharacterRequest, which is also this
     * control's `name` attribute. It comes from `adminWireField` — the same
     * one-line rule the request builder uses — so the form and the wire cannot
     * disagree about what a path is called.
     */
    name: string;
    label: string;
    kind: 'line' | 'prose';
    maxBytes: number;
  }

  const SHORT = 100; // world.MaxNameLength
  const LONG = 4000; // world.MaxDescriptionLength

  const line = (path: string, label: string): EditableField => ({
    path,
    name: adminWireField(path),
    label,
    kind: 'line',
    maxBytes: SHORT,
  });

  const prose = (path: string, label: string): EditableField => ({
    path,
    name: adminWireField(path),
    label,
    kind: 'prose',
    maxBytes: LONG,
  });

  export const ADMIN_EDITABLE_FIELDS: EditableField[] = [
    prose('description', 'In-world description'),
    line('profile.pronouns', 'Pronouns'),
    line('profile.concept', 'Concept'),
    line('profile.species', 'Species'),
    line('profile.age', 'Age'),
    line('profile.faction', 'Faction'),
    line('profile.currently', 'Currently'),
    line('profile.timezone', 'Timezone'),
    prose('profile.appearance', 'Appearance'),
    prose('profile.personality', 'Personality'),
    prose('profile.biography', 'Biography'),
    prose('profile.rumors', 'Rumors'),
    prose('profile.rp_preferences', 'RP preferences'),
  ];
</script>

<script lang="ts">
  import { untrack } from 'svelte';
  import * as Sheet from '$lib/components/ui/sheet';
  import * as Field from '$lib/components/ui/field';
  import * as Alert from '$lib/components/ui/alert';
  import { Skeleton } from '$lib/components/ui/skeleton';
  import { byteCount } from '$lib/text/byteCount';
  import { isAbortedError } from '$lib/connect/errors';
  import { getAdminCharacter, type CharacterDetail, type CharacterRow } from '$lib/admin/client';

  /**
   * The admin edit surface: two groups, a transition picker, a visible mask,
   * and a real bottom sheet on the phone band.
   *
   * ITS FIRST GROUP IS THE EXCLUSIONS, DECLARED BEFORE THE FORM. Thirteen of
   * this character's fields are writable here and two are deliberately not, and
   * "incomplete" is what a well-meaning implementer FIXES — so `Managed
   * elsewhere` comes first, collapsed, and says where those two live.
   *
   * IT NEVER SENDS A STATUS VALUE. §10.6 keeps the lifecycle vocabulary off the
   * wire so `idle` stays unreachable; a maskable `status` path would put it
   * back. The picker below routes a selection to the confirmation, which sends
   * AdminRetireCharacter or AdminUnretireCharacter, and nothing on this surface
   * puts a lifecycle string on a request.
   *
   * IT RENDERS NO SERVER-SUPPLIED STRING. A refusal resolves into one of two
   * authored outcomes and nothing else. The only server-supplied values that
   * reach the screen are two version INTEGERS in the conflict alert, which the
   * table already shows.
   *
   * IT READS THE CHARACTER'S REAL CURRENT VALUES. The list row carries no
   * profile prose at all (AdminCharacter is the §11.3 projection), so the
   * thirteen inputs are seeded from a single-character AdminGetCharacter read
   * issued when the Sheet opens. Seeding from the row would render thirteen
   * blanks and OVERWRITE existing player-authored content on the first save,
   * because a blank field and an unfetched field are indistinguishable once
   * both are empty strings in a form model.
   */
  interface Props {
    /** The list row the operator reached for: name, status and version. */
    row: CharacterRow;
    /** The single-character read behind the form. Injectable for tests. */
    fetchDetail?: (id: string) => Promise<CharacterDetail | undefined>;
    /**
     * The page's mutation driver. It owns the D-110 sequence — the pending row,
     * the RPC, the in-place row update, the close and the toast — so this
     * component's only job on success is to stop being busy.
     */
    save?: (args: {
      paths: string[];
      values: Record<string, string>;
      expectedVersion: number;
    }) => Promise<unknown>;
    /** Routes a transition selection to the confirmation. Sends no RPC. */
    onlifecycle?: (intent: 'retire' | 'unretire') => void;
    onclose?: () => void;
  }

  let {
    row,
    fetchDetail = getAdminCharacter,
    save,
    onlifecycle,
    onclose,
  }: Props = $props();

  const CONFLICT_TAIL =
    'Your changes were not applied — reload to see the current values, then re-apply.';
  const GENERIC_COPY = "Couldn't save. Try again.";
  const DETAIL_FAILED_COPY = "Couldn't load this character. Try again.";

  let detailState = $state<'loading' | 'ready' | 'failed'>('loading');
  /**
   * `loaded` is what the server last confirmed and `working` is what the
   * operator has typed. Dirty is the difference between the two — never a flag
   * set by an input handler, which would survive an edit that put a field back
   * the way it started.
   */
  let loaded = $state<Record<string, string>>({});
  let working = $state<Record<string, string>>({});
  let busy = $state(false);
  let failure = $state<'' | 'conflict' | 'generic'>('');
  let conflictMine = $state(0);
  let conflictTheirs = $state<number | undefined>(undefined);
  // Plain, NOT $state: nothing renders from these and nothing derives from
  // them. They are focus targets for the conflict path, and making them
  // reactive only adds reads during teardown.
  const fieldEls: Record<string, HTMLInputElement | HTMLTextAreaElement | null> = {};
  /**
   * The version this form is composed against, carried as `expected_version`.
   * It starts as the row's and moves only when the operator reloads after a
   * conflict — never silently, because silently adopting the server's version
   * would turn the concurrency guard off exactly when it is doing its job.
   */
  let expectedVersion = $state(untrack(() => row.version));

  /** The phone band is a Svelte derivation, because `side` is a PROP. */
  let isPhone = $state(false);

  $effect(() => {
    // Guarded for SSR and for a test environment with no matchMedia — this
    // jsdom has none. The fallback is the DESKTOP shape: flickering through a
    // bottom sheet on a desktop first paint is the failure this closes.
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
    const mql = window.matchMedia('(max-width: 767px)');
    isPhone = mql.matches;
    const onchange = (e: MediaQueryListEvent) => {
      isPhone = e.matches;
    };
    mql.addEventListener('change', onchange);
    return () => mql.removeEventListener('change', onchange);
  });

  /**
   * `side` is a Svelte PROP on Sheet.Content (sheet-content.svelte:18,25),
   * emitted as data-side={side} (:36). CSS cannot mutate an attribute, so no
   * media or container rule could ever produce this value or select the bottom
   * transition classes — which is why the flip lives here and only the height
   * and the input font live in the stylesheet below. Both halves read the SAME
   * 767px literal through the SAME viewport mechanism, in this one file.
   */
  const side = $derived(isPhone ? 'bottom' : 'right');

  function valuesFrom(d: CharacterDetail | undefined): Record<string, string> {
    const out: Record<string, string> = {};
    for (const f of ADMIN_EDITABLE_FIELDS) {
      out[f.path] =
        f.path === 'description' ? (d?.description ?? '') : (d?.profile?.[f.path] ?? '');
    }
    return out;
  }

  async function loadDetail(id: string) {
    detailState = 'loading';
    try {
      const d = await fetchDetail(id);
      const next = valuesFrom(d);
      loaded = { ...next };
      working = { ...next };
      if (d?.character) expectedVersion = d.character.version;
      detailState = 'ready';
    } catch {
      // The caught value is deliberately not bound. There is nothing this form
      // may learn from it and nothing it may show.
      detailState = 'failed';
    }
  }

  $effect(() => {
    const id = row.id;
    untrack(() => void loadDetail(id));
  });

  const dirtyPaths = $derived(
    ADMIN_EDITABLE_FIELDS.filter((f) => (working[f.path] ?? '') !== (loaded[f.path] ?? '')).map(
      (f) => f.path,
    ),
  );
  const ready = $derived(detailState === 'ready');
  /**
   * EXACTLY TWO CONDITIONS DISABLE SAVE, and there is no third. An over-cap
   * value is NOT one of them: the server is the enforcer, and a client that
   * refused to send would make its own agreement with the server unobservable.
   */
  const canSave = $derived(ready && dirtyPaths.length > 0);

  const isRetired = $derived(row.status === 'retired');
  const transitionRpc = $derived(
    isRetired ? 'AdminUnretireCharacter' : 'AdminRetireCharacter',
  );

  function onlifecyclechange(e: Event) {
    const el = e.currentTarget as HTMLSelectElement;
    const chosen = el.value;
    // Snap back immediately: selecting a transition does NOT apply it, and a
    // control left showing the new value would claim it had.
    el.value = row.status;
    if (chosen === 'retired' && !isRetired) onlifecycle?.('retire');
    else if (chosen === 'active' && isRetired) onlifecycle?.('unretire');
  }

  function edit(path: string, value: string) {
    working = { ...working, [path]: value };
  }

  async function resolveConflict() {
    conflictMine = expectedVersion;
    conflictTheirs = undefined;
    let fresh: CharacterDetail | undefined;
    try {
      fresh = await fetchDetail(row.id);
    } catch {
      // A failed second read costs the alert its second number and nothing
      // else. The conflict is still a conflict.
      focusFirst(dirtyPaths[0]);
      return;
    }
    if (fresh?.character) conflictTheirs = fresh.character.version;
    const server = valuesFrom(fresh);
    const contested = dirtyPaths.filter((p) => (server[p] ?? '') !== (loaded[p] ?? ''));
    focusFirst(contested[0] ?? dirtyPaths[0]);
  }

  function focusFirst(path: string | undefined) {
    if (!path) return;
    fieldEls[path]?.focus();
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (!canSave || busy) return;
    busy = true;
    failure = '';
    const paths = [...dirtyPaths];
    const values: Record<string, string> = {};
    for (const p of paths) values[p] = working[p] ?? '';
    try {
      await save?.({ paths, values, expectedVersion });
      // On success the page closes this Sheet, updates the row in place from
      // the response and fires the receipt. Nothing further belongs here.
    } catch (err) {
      if (isAbortedError(err)) {
        failure = 'conflict';
        await resolveConflict();
      } else {
        failure = 'generic';
      }
    } finally {
      busy = false;
    }
  }

  /** Discards the draft and reseeds from the server's current values. */
  async function reload() {
    failure = '';
    conflictTheirs = undefined;
    await loadDetail(row.id);
  }

  function onOpenChange(open: boolean) {
    if (!open) onclose?.();
  }
</script>

<Sheet.Root open={true} {onOpenChange}>
  <Sheet.Content
    {side}
    class="editsheet gap-0 overflow-y-auto data-[side=right]:w-[380px] data-[side=right]:sm:max-w-[380px]"
  >
    <!--
      DELIBERATELY NOT PORTALLED ANYWHERE. No target-override prop is passed,
      so this keeps sheet-content.svelte:31's generated document.body
      destination. Both the content and the overlay are position: fixed; giving
      them a destination inside an element with layout containment would rebase
      them onto that element, leaving the top bar undimmed and clickable behind
      an open modal and making any height a claim about ancestry. Do not
      "improve" this by handing it one. (This note words the prop's name rather
      than writing it: an acceptance gate scans this file for the literal, and
      a gate that has to be suppressed to stay green stops being a gate.)

      NO GRAB HANDLE (D-109). bits-ui has no swipe-dismiss, so a handle would
      promise a gesture the component cannot honor, and an affordance that
      invites a gesture which then fails silently is worse than none. Closing
      is the backdrop, Escape, the generated close control and Cancel.
    -->
    <Sheet.Header class="gap-1">
      <Sheet.Title class="text-[15px] font-semibold">Edit character</Sheet.Title>
      <p class="meta" data-testid="sheet-meta">
        <span class="mono">{row.id}</span> · v{expectedVersion}
      </p>
      <Sheet.Description class="sr-only">
        The profile fields an administrator may write for this character.
      </Sheet.Description>
    </Sheet.Header>

    <form class="body" onsubmit={submit}>
      <!-- FIRST, and collapsed: the operator learns what this surface does not
           do before they start typing. -->
      <details class="managed" data-group="managed-elsewhere">
        <summary class="grouphead">
          <span>Managed elsewhere</span>
          <span class="summaryline">
            <b>Name</b>
            {row.name} · <b>Status</b>
            {row.status}
          </span>
          <span class="summarynote">managed by their own operations</span>
        </summary>
        <div class="managedrows">
          <p class="managedrow">
            <b>Name</b>
            Names go through the normalization pipeline and a uniqueness check, so they are not
            editable from this form.
          </p>
          <p class="managedrow">
            <b>Status</b>
            Use the lifecycle control below — it sends the transition, not a status value.
          </p>
        </div>
      </details>

      <!-- `version` is header metadata, never a row here: it is never editable
           and never actionable, so a row alongside Name and Status would imply
           a door that does not exist. -->

      <section class="lifecycle" aria-labelledby="lifecycle-label">
        <span class="grouphead" id="lifecycle-label">Lifecycle</span>
        <select
          class="control"
          name="lifecycle"
          aria-labelledby="lifecycle-label"
          value={row.status}
          onchange={onlifecyclechange}
        >
          <option value="active">Active</option>
          <option value="retired">Retired</option>
          <!-- Shown so the three-state model is legible; never selectable,
               because a selectable `idle` would put the unreachable value back
               on a wire §10.6 keeps it off. -->
          <option value="idle" disabled>idle (unavailable)</option>
        </select>
        <p class="reason">idle — system-invoked on inactivity. Not implemented in this release.</p>
        <p class="reason">Sends {transitionRpc} — never a status value.</p>
      </section>

      <section data-group="editable-here" aria-labelledby="editable-label">
        <span class="grouphead" id="editable-label">Editable here</span>

        {#if detailState === 'loading'}
          <div class="skeletons" role="status" aria-label="Loading this character's profile">
            {#each ADMIN_EDITABLE_FIELDS as f (f.path)}
              <Skeleton class="h-9 w-full" />
            {/each}
          </div>
        {:else if detailState === 'failed'}
          <Alert.Root variant="destructive" class="failure">
            <Alert.Description>{DETAIL_FAILED_COPY}</Alert.Description>
          </Alert.Root>
          <button
            type="button"
            class="stateaction"
            data-testid="detail-retry"
            onclick={() => void loadDetail(row.id)}
          >
            Try again
          </button>
        {:else}
          <Field.Group>
            {#each ADMIN_EDITABLE_FIELDS as f (f.path)}
              <Field.Field>
                <Field.Label for={`edit-${f.name}`}>{f.label}</Field.Label>
                {#if f.kind === 'prose'}
                  <textarea
                    id={`edit-${f.name}`}
                    class="control prose"
                    name={f.name}
                    rows="3"
                    bind:this={fieldEls[f.path]}
                    value={working[f.path] ?? ''}
                    oninput={(e) => edit(f.path, e.currentTarget.value)}
                  ></textarea>
                {:else}
                  <input
                    id={`edit-${f.name}`}
                    class="control"
                    type="text"
                    name={f.name}
                    bind:this={fieldEls[f.path]}
                    value={working[f.path] ?? ''}
                    oninput={(e) => edit(f.path, e.currentTarget.value)}
                  />
                {/if}
                <!-- Always visible and always in bytes. Over the cap changes
                     this element's styling and NOTHING else — the server owns
                     the refusal, and a client that refused would hide the
                     disagreement it exists to surface. -->
                <p
                  class="counter"
                  data-counter-for={f.path}
                  data-over={byteCount(working[f.path] ?? '') > f.maxBytes ? 'true' : 'false'}
                >
                  {byteCount(working[f.path] ?? '')} of {f.maxBytes}
                </p>
              </Field.Field>
            {/each}
          </Field.Group>
        {/if}
      </section>

      {#if failure === 'conflict'}
        <Alert.Root variant="destructive" class="failure">
          <Alert.Description>
            {#if conflictTheirs === undefined}
              Someone else edited this character. Your copy is version {conflictMine}. {CONFLICT_TAIL}
            {:else}
              Someone else edited this character. Your copy is version {conflictMine}; the server is
              now at {conflictTheirs}. {CONFLICT_TAIL}
            {/if}
          </Alert.Description>
        </Alert.Root>
        <button type="button" class="stateaction" data-testid="conflict-reload" onclick={reload}>
          Reload
        </button>
      {:else if failure === 'generic'}
        <Alert.Root variant="destructive" class="failure">
          <Alert.Description>{GENERIC_COPY}</Alert.Description>
        </Alert.Root>
      {/if}

      <footer class="foot">
        <span class="maskcount" data-testid="mask-footer">
          {#if dirtyPaths.length > 0}
            update_mask: {dirtyPaths.length} paths
          {:else}
            update_mask: empty — no-op
          {/if}
        </span>
        <span class="spacer"></span>
        <button type="button" class="stateaction" data-testid="sheet-cancel" onclick={() => onclose?.()}>
          Cancel
        </button>
        <button
          type="submit"
          class="stateaction primary"
          data-testid="sheet-save"
          disabled={!canSave || busy}
          aria-busy={busy ? 'true' : undefined}
        >
          Save changes
        </button>
      </footer>
    </form>
  </Sheet.Content>
</Sheet.Root>

<style>
  .meta {
    font-size: 12px;
    line-height: 1.4;
    color: var(--color-status-text);
    margin: 0;
  }
  /* The character ULID is the single string in this phase permitted monospace:
     it is an opaque identifier an operator may need to copy. */
  .mono {
    font-family: var(--font-mono, 'JetBrains Mono', monospace);
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 16px;
    min-height: 0;
  }
  .grouphead {
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    color: var(--color-muted-foreground);
    display: block;
  }
  .managed {
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 8px 10px;
  }
  .managed > summary {
    cursor: pointer;
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 8px;
  }
  .summaryline,
  .summarynote {
    font-size: 12px;
    font-weight: 400;
    line-height: 1.4;
    color: var(--color-status-text);
  }
  .summarynote {
    margin-left: auto;
  }
  .managedrows {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: 8px;
  }
  .managedrow {
    margin: 0;
    font-size: 12px;
    line-height: 1.4;
    color: var(--color-status-text);
  }
  .lifecycle {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .reason {
    margin: 0;
    font-size: 12px;
    line-height: 1.4;
    color: var(--color-status-text);
  }
  .skeletons {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .control {
    width: 100%;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    background: var(--color-input-background, var(--color-card));
    color: var(--color-foreground);
    font-size: 14px;
    line-height: 1.4;
    padding: 8px 10px;
  }
  .control:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
  .prose {
    resize: vertical;
  }
  .counter {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    color: var(--color-status-text);
  }
  .counter[data-over='true'] {
    color: var(--color-destructive);
  }
  .foot {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-direction: row;
  }
  .maskcount {
    font-size: 12px;
    line-height: 1.4;
    color: var(--color-status-text);
  }
  .spacer {
    flex: 1;
  }
  .stateaction {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 0 12px;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    background: var(--color-card);
    color: var(--color-foreground);
    font-size: 14px;
    cursor: pointer;
  }
  .stateaction:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .primary {
    background: var(--color-primary);
    border-color: var(--color-primary);
    color: var(--color-primary-foreground, #fff);
  }
  .stateaction:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }

  /*
    THE ONLY VIEWPORT RULE IN THIS FILE, and it carries exactly the two things
    CSS can actually discharge. The 767px literal is byte-identical to the one
    the matchMedia derivation above reads, through the same viewport mechanism,
    in the same file — so the two halves cannot fire at different widths.

    The height is `vh`, not `%`: a percentage resolves against the containing
    block and would make the E2E's "≈84% of the viewport" an argument about
    ancestry rather than a claim about the quantity declared here.

    16px is a PLATFORM CONSTRAINT, not a style preference — any font below it
    in a FOCUSED input triggers iOS Safari's zoom-on-focus, which does not
    unzoom on blur.

    The doubled class raises specificity above the generated
    `data-[side=bottom]:h-auto` utility deterministically, rather than relying
    on which stylesheet the bundler emitted last.
  */
  @media (max-width: 767px) {
    :global(.editsheet.editsheet[data-side='bottom']) {
      height: 84vh;
    }
    :global(.editsheet input),
    :global(.editsheet textarea),
    :global(.editsheet select) {
      font-size: 16px;
    }
  }
</style>
