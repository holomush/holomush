<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import * as AlertDialog from '$lib/components/ui/alert-dialog';
  import * as Alert from '$lib/components/ui/alert';

  /**
   * The one confirmation both lifecycle transitions route through, from both
   * entrances: the table's row action and the Sheet's transition picker. Two
   * entrances, one component, one mutation.
   *
   * IT IS AN alert-dialog, NOT A dialog. This confirmation requires a decision
   * and MUST NOT be dismissible by a stray backdrop tap: it is an audited
   * mutation by an operator on a character they do not own. The installed
   * `dialog` primitive does not carry that role, and reusing it would ship a
   * confirmation a misplaced click can silently cancel.
   *
   * ITS RETIRE BODY ASSERTS NOTHING UNTRUE. The misconception this actor is
   * likeliest to hold is that retiring is a takedown, and every clause below
   * corrects exactly that: the public profile stays visible, published history
   * is unchanged, the name stays reserved (§4.4, §4.5), and the transition is
   * undoable. There is no AdminDeleteCharacter RPC at all — §4.4 and §10.6 both
   * forbid wiring world.Service.DeleteCharacter to an admin affordance — so no
   * copy here may describe this as deleting, removing or hiding anyone.
   *
   * UN-RETIRE CONFIRMS TOO, deliberately: both directions are audited
   * mutations on someone else's character, and the picker's whole contract is
   * that selecting a transition ROUTES TO THE CONFIRMATION rather than applying
   * it.
   */
  interface Props {
    /** The character's display name, interpolated into the title. */
    name: string;
    intent: 'retire' | 'unretire';
    /**
     * Sends the RPC. It REJECTS on failure rather than resolving with an
     * outcome, so this component learns nothing about the wire beyond "it did
     * not work" — which is all it renders.
     */
    onconfirm: () => Promise<unknown>;
    oncancel?: () => void;
  }

  let { name, intent, onconfirm, oncancel }: Props = $props();

  const FAILURE_COPY = "Couldn't change this character's lifecycle. Try again.";

  const retiring = $derived(intent === 'retire');
  const title = $derived(retiring ? `Retire ${name}?` : `Return ${name} to active play?`);
  const confirmLabel = $derived(retiring ? 'Retire character' : 'Un-retire character');

  let busy = $state(false);
  let failed = $state(false);
  let cancelEl = $state<HTMLElement | null>(null);

  /*
   * Initial focus lands on Cancel, never on the destructive confirm. Set here
   * rather than left to the primitive's default so the property is a decision
   * this file makes and a test can observe, instead of a behaviour inherited
   * from a dependency that could change it.
   */
  $effect(() => {
    cancelEl?.focus();
  });

  async function confirm() {
    if (busy) return;
    busy = true;
    failed = false;
    try {
      await onconfirm();
      // On success the page closes this dialog, updates the row in place from
      // the response and fires the receipt.
    } catch {
      // The caught value is deliberately not bound: a wrapped refusal carries
      // nothing an operator may act on and nothing this surface may show.
      failed = true;
    } finally {
      busy = false;
    }
  }

  function onOpenChange(open: boolean) {
    if (!open && !busy) oncancel?.();
  }
</script>

<AlertDialog.Root open={true} {onOpenChange}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{title}</AlertDialog.Title>
      <AlertDialog.Description>
        {#if retiring}
          Retiring takes this character out of active play. It does not hide them — their public
          profile stays visible and everything they have already posed or played in is unchanged.
          The name stays reserved. You can undo this at any time.
        {:else}
          This character becomes playable again. Their public profile and history are unchanged.
        {/if}
      </AlertDialog.Description>
    </AlertDialog.Header>

    {#if failed}
      <Alert.Root variant="destructive">
        <Alert.Description>{FAILURE_COPY}</Alert.Description>
      </Alert.Root>
    {/if}

    <AlertDialog.Footer>
      <AlertDialog.Cancel bind:ref={cancelEl} disabled={busy}>Cancel</AlertDialog.Cancel>
      <!--
        A PLAIN BUTTON, not AlertDialog.Action. The primitive's Action closes
        the dialog on click, which would dismiss the confirmation before the
        mutation has answered — and a failure would then have nowhere to land
        but a toast, which is a receipt for something that FINISHED. Closing is
        the page's decision, taken when the RPC resolves.
      -->
      <button
        type="button"
        class="confirmbtn"
        class:destructive={retiring}
        data-testid="lifecycle-confirm"
        disabled={busy}
        aria-busy={busy ? 'true' : undefined}
        onclick={confirm}
      >
        {confirmLabel}
      </button>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>

<style>
  .confirmbtn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 44px;
    padding: 0 14px;
    border-radius: 6px;
    border: 1px solid var(--color-primary);
    background: var(--color-primary);
    color: var(--color-primary-foreground, #fff);
    font-size: 14px;
    font-weight: 600;
    cursor: pointer;
  }
  .confirmbtn.destructive {
    border-color: var(--color-destructive);
    background: var(--color-destructive);
  }
  .confirmbtn:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .confirmbtn:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
</style>
