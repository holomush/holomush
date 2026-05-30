<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { onDestroy } from 'svelte';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { uiPrefs } from '$lib/stores/uiPrefsStore';
  import {
    pushCommand,
    navigatePrev,
    navigateNext,
    resetNav,
    seedCommands,
  } from '$lib/stores/commandHistoryStore';
  import ModeChip from './ModeChip.svelte';

  interface Props {
    sessionId: string;
    onSend: (command: string) => void;
    injectText?: string;
    onInjectConsumed?: () => void;
  }

  let { sessionId, onSend, injectText, onInjectConsumed }: Props = $props();

  const DRAFT_KEY_PREFIX = 'holomush-draft:';
  const DRAFT_DEBOUNCE_MS = 500;
  const LINE_HEIGHT_PX = 20;

  let text = $state('');
  let textarea: HTMLTextAreaElement;
  let draftTimer: ReturnType<typeof setTimeout> | undefined;

  const client = createClient(WebService, transport);

  // Derived: mode chip from leading characters
  let modeChip = $derived.by<'say' | 'pose' | 'ooc' | null>(() => {
    const v = text.trimStart();
    if (v.startsWith(':') || v.startsWith('pose ')) return 'pose';
    if (v.startsWith('"') || v.startsWith('say ')) return 'say';
    if (v.startsWith('ooc ')) return 'ooc';
    return null;
  });

  // Derived: line count and near-max flag for composer nudge
  let lineCount = $derived(text === '' ? 1 : text.split('\n').length);
  let nearMax = $derived(lineCount >= 6);

  // Restore draft and load command history when session changes
  $effect(() => {
    clearTimeout(draftTimer);

    if (!sessionId) {
      seedCommands([]);
      text = '';
      return;
    }

    const saved = localStorage.getItem(DRAFT_KEY_PREFIX + sessionId);
    text = saved ?? '';
    requestAnimationFrame(() => {
      autoGrow();
      if (textarea && !$uiPrefs.composerOpen) textarea.focus();
    });

    const captured = sessionId;
    client.getCommandHistory({ sessionId }).then((resp) => {
      if (captured !== sessionId) return;
      seedCommands(resp.commands ?? []);
    }).catch((e) => {
      if (captured !== sessionId) return;  // stale session — skip log
      console.warn('[history] load failed', e);
    });
  });

  // Inject-from-parent pathway (recent card, composer close)
  $effect(() => {
    if (injectText !== undefined) {
      text = injectText;
      onInjectConsumed?.();
      requestAnimationFrame(() => {
        autoGrow();
        if (textarea && !$uiPrefs.composerOpen) textarea.focus();
      });
    }
  });

  // Debounced save of draft text to localStorage
  $effect(() => {
    const current = text;
    const sid = sessionId;
    if (!sid) {
      clearTimeout(draftTimer);
      return;
    }
    clearTimeout(draftTimer);
    if (current) {
      draftTimer = setTimeout(() => {
        try {
          localStorage.setItem(DRAFT_KEY_PREFIX + sid, current);
        } catch (e) { console.warn('[draft] persist failed', e); }
      }, DRAFT_DEBOUNCE_MS);
    } else {
      try {
        localStorage.removeItem(DRAFT_KEY_PREFIX + sid);
      } catch { /* best effort */ }
    }
  });

  onDestroy(() => clearTimeout(draftTimer));

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      submit();
    } else if (e.key === 'Escape') {
      text = '';
      resetNav();
    } else if (e.key === 'ArrowUp' && !e.shiftKey) {
      const prev = navigatePrev();
      if (prev !== null) {
        text = prev;
        requestAnimationFrame(autoGrow);
      }
      e.preventDefault();
    } else if (e.key === 'ArrowDown' && !e.shiftKey) {
      const next = navigateNext();
      if (next !== null) {
        text = next;
        requestAnimationFrame(autoGrow);
      }
      e.preventDefault();
    }
  }

  function submit() {
    const cmd = text.trim();
    if (!cmd) return;
    pushCommand(cmd);
    text = '';
    clearTimeout(draftTimer);
    if (sessionId) {
      try { localStorage.removeItem(DRAFT_KEY_PREFIX + sessionId); }
      catch { /* best effort */ }
    }
    onSend(cmd);
    requestAnimationFrame(() => {
      if (textarea) textarea.style.height = 'auto';
    });
  }

  function autoGrow() {
    // Gate: don't read scrollHeight on a disabled textarea (composer open)
    if ($uiPrefs.composerOpen) return;
    if (!textarea) return;
    textarea.style.height = 'auto';
    const maxLines = parseInt(
      getComputedStyle(textarea).getPropertyValue('--cmd-max-lines') || '8',
      10,
    );
    const maxHeight = (Number.isFinite(maxLines) ? maxLines : 8) * LINE_HEIGHT_PX;
    textarea.style.height = Math.min(textarea.scrollHeight, maxHeight) + 'px';
  }
</script>

<div class="cmd-wrap" class:is-suspended={$uiPrefs.composerOpen} class:is-multiline={lineCount > 1}>
  <span class="cmd-prompt">&gt;</span>
  {#if modeChip}<ModeChip mode={modeChip} />{/if}
  <textarea
    bind:this={textarea}
    bind:value={text}
    onkeydown={handleKeydown}
    oninput={autoGrow}
    rows="1"
    placeholder="Enter command..."
    spellcheck="false"
    autocomplete="off"
    disabled={$uiPrefs.composerOpen}
    aria-disabled={$uiPrefs.composerOpen}
  ></textarea>
  {#if $uiPrefs.composerOpen}
    <div class="suspended-overlay" aria-live="polite">Composer open — input paused</div>
  {/if}
</div>

<div class="cmd-hints">
  <span><kbd>↑↓</kbd> history</span>
  <span><kbd>⇧⏎</kbd> newline</span>
  <span><kbd>Esc</kbd> clear</span>
  <span><kbd>⌘K</kbd> palette</span>
  <span><kbd>⌘B</kbd> rail</span>
  <span><kbd>⌘.</kbd> sidebar</span>
  <span><kbd>⌘⇧E</kbd> composer</span>
  <span class="line-count">{lineCount} line{lineCount === 1 ? '' : 's'}</span>
  {#if nearMax && !$uiPrefs.composerOpen}
    <span class="composer-nudge">Press ⌘⇧E for a bigger editor</span>
  {/if}
</div>

<style>
  .cmd-wrap {
    position: relative;
    display: flex;
    align-items: flex-start;
    gap: 6px;
    padding: 8px 12px;
    background: var(--color-input-background);
    border-top: 1px solid var(--color-border);
  }
  .cmd-prompt { color: var(--color-input-prompt); line-height: 20px; flex-shrink: 0; }
  textarea {
    flex: 1;
    background: transparent;
    border: none; outline: none;
    color: var(--color-input-text);
    caret-color: var(--color-cursor);
    font-family: inherit; font-size: inherit;
    resize: none; line-height: 20px;
    overflow-y: auto;
  }
  textarea:disabled { opacity: 0.5; }
  .suspended-overlay {
    position: absolute;
    inset: 0;
    display: flex; align-items: center; justify-content: center;
    color: var(--color-status-text);
    background: color-mix(in srgb, var(--color-background) 50%, transparent);
    font-size: 12px;
    pointer-events: none;
  }
  .cmd-hints {
    padding: 3px 12px;
    font-size: 14px;
    color: var(--color-status-text);
    background: var(--color-background);
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    align-items: center;
  }
  .cmd-hints kbd {
    font-family: inherit; padding: 1px 5px;
    border: 1px solid var(--color-border); border-radius: 3px;
    font-size: 13px;
  }
  .line-count { color: var(--color-muted-foreground); }
  .composer-nudge { color: var(--color-primary); }
</style>
