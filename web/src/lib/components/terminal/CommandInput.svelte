<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { onDestroy } from 'svelte';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';

  interface Props {
    sessionId: string;
    onSend: (command: string) => void;
  }

  let { sessionId, onSend }: Props = $props();

  const DRAFT_KEY_PREFIX = 'holomush-draft:';
  const DRAFT_DEBOUNCE_MS = 500;

  let text = $state('');
  let textarea: HTMLTextAreaElement;
  let history: string[] = $state([]);
  let historyIndex = $state(-1);
  let draftTimer: ReturnType<typeof setTimeout> | undefined;

  const client = createClient(WebService, transport);

  // Restore draft and load command history when session changes
  $effect(() => {
    // Cancel any pending save from the previous session
    clearTimeout(draftTimer);

    if (!sessionId) {
      history = [];
      historyIndex = -1;
      text = '';
      return;
    }

    // Restore saved draft (or clear if none exists)
    const saved = localStorage.getItem(DRAFT_KEY_PREFIX + sessionId);
    text = saved ?? '';
    requestAnimationFrame(autoGrow);

    const captured = sessionId;
    client.getCommandHistory({ sessionId }).then((resp) => {
      if (captured !== sessionId) return;
      history = resp.commands ?? [];
    }).catch(() => { /* best-effort */ });
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
        localStorage.setItem(DRAFT_KEY_PREFIX + sid, current);
      }, DRAFT_DEBOUNCE_MS);
    } else {
      localStorage.removeItem(DRAFT_KEY_PREFIX + sid);
    }
  });

  onDestroy(() => clearTimeout(draftTimer));

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      submit();
    } else if (e.key === 'Escape') {
      text = '';
      historyIndex = -1;
    } else if (e.key === 'ArrowUp' && !e.shiftKey) {
      if (historyIndex < history.length - 1) {
        historyIndex++;
        text = history[history.length - 1 - historyIndex];
        requestAnimationFrame(autoGrow);
      }
      e.preventDefault();
    } else if (e.key === 'ArrowDown' && !e.shiftKey) {
      if (historyIndex > 0) {
        historyIndex--;
        text = history[history.length - 1 - historyIndex];
        requestAnimationFrame(autoGrow);
      } else if (historyIndex === 0) {
        historyIndex = -1;
        text = '';
        requestAnimationFrame(autoGrow);
      }
      e.preventDefault();
    }
  }

  function submit() {
    const cmd = text.trim();
    if (!cmd) return;
    history = [...history, cmd];
    historyIndex = -1;
    text = '';
    clearTimeout(draftTimer);
    if (sessionId) localStorage.removeItem(DRAFT_KEY_PREFIX + sessionId);
    onSend(cmd);
    requestAnimationFrame(() => {
      if (textarea) textarea.style.height = 'auto';
    });
  }

  function autoGrow() {
    if (!textarea) return;
    textarea.style.height = 'auto';
    const maxHeight = 6 * 20;
    textarea.style.height = Math.min(textarea.scrollHeight, maxHeight) + 'px';
  }
</script>

<div class="command-input">
  <span class="prompt">&gt;</span>
  <textarea
    bind:this={textarea}
    bind:value={text}
    onkeydown={handleKeydown}
    oninput={autoGrow}
    rows="1"
    placeholder="Enter command..."
    spellcheck="false"
    autocomplete="off"
  ></textarea>
</div>

<div class="hints">
  <span>Up/Down history | Shift+Enter newline | Esc clear</span>
</div>

<style>
  .command-input {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    padding: 8px 12px;
    background: var(--color-input-background);
    border-top: 1px solid var(--color-border);
  }
  .prompt { color: var(--color-input-prompt); line-height: 20px; flex-shrink: 0; }
  textarea {
    flex: 1;
    background: transparent;
    border: none;
    outline: none;
    color: var(--color-input-text);
    font-family: inherit;
    font-size: inherit;
    resize: none;
    line-height: 20px;
    overflow-y: auto;
  }
  .hints {
    padding: 3px 12px;
    font-size: 9px;
    color: var(--color-status-text);
    background: var(--color-background);
  }
</style>
