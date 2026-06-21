<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { X } from '@lucide/svelte';
  import {
    uiPrefs,
    closeComposer,
    setComposerPos,
    setComposerSize,
  } from '$lib/stores/uiPrefsStore';

  interface Props {
    draft: string;
    ondraftChange: (text: string) => void;
    onsubmit: (text: string) => void;
  }
  let { draft, ondraftChange, onsubmit }: Props = $props();

  let textareaEl: HTMLTextAreaElement | undefined = $state(undefined);
  let panelEl: HTMLElement | undefined = $state(undefined);

  let localText = $state('');
  // Sync external draft changes (the parent seeds the draft when the composer
  // opens) into localText WITHOUT re-firing on the user's own keystrokes. The
  // textarea two-way-binds localText via bind:value; if this effect also tracked
  // localText, every keystroke would re-run it, observe the not-yet-propagated
  // draft, and clobber localText back to the stale draft BEFORE onTextInput
  // could push the new text upward — so the composer would never accept input
  // (holomush-6k7d). untrack() makes draft the sole dependency.
  $effect(() => {
    const next = draft;
    untrack(() => {
      if (next !== localText) localText = next;
    });
  });

  let pos = $state({ x: 0, y: 0 });
  let size = $state({ w: 640, h: 340 });

  $effect(() => {
    if (!$uiPrefs.composerOpen) return;
    const stored = $uiPrefs.composerPos;
    const storedSize = $uiPrefs.composerSize;
    if (stored.x < 0 || stored.y < 0) {
      pos = {
        x: Math.max(0, (window.innerWidth - storedSize.w) / 2),
        y: Math.max(0, (window.innerHeight - storedSize.h) / 2),
      };
    } else {
      pos = { ...stored };
    }
    size = { ...storedSize };
    requestAnimationFrame(() => textareaEl?.focus());
  });

  // Composer-scoped window Esc listener — gated on composerOpen so
  // the global layout handler never sees Esc while composer is open,
  // even if focus has escaped the panel.
  $effect(() => {
    if (!$uiPrefs.composerOpen) return;
    function onEsc(e: KeyboardEvent) {
      if (e.key !== 'Escape') return;
      if (e.isComposing) return;
      e.preventDefault();
      e.stopPropagation();
      closeComposer();
    }
    window.addEventListener('keydown', onEsc, true);
    return () => window.removeEventListener('keydown', onEsc, true);
  });

  function onTextInput() {
    ondraftChange(localText);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.isComposing) return;
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      const cmd = localText.trim();
      if (!cmd) return;
      onsubmit(cmd);
      ondraftChange('');
      closeComposer();
    }
  }

  let dragStart: { x: number; y: number; origX: number; origY: number } | null = null;
  function onHeaderPointerDown(e: PointerEvent) {
    if ((e.target as HTMLElement).closest('.cclose')) return;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    dragStart = { x: e.clientX, y: e.clientY, origX: pos.x, origY: pos.y };
  }
  function onHeaderPointerMove(e: PointerEvent) {
    if (!dragStart) return;
    const nx = dragStart.origX + (e.clientX - dragStart.x);
    const ny = dragStart.origY + (e.clientY - dragStart.y);
    pos = {
      x: Math.max(-size.w + 80, Math.min(window.innerWidth - 80, nx)),
      y: Math.max(0, Math.min(window.innerHeight - 40, ny)),
    };
  }
  function onHeaderPointerUp() {
    if (!dragStart) return;
    dragStart = null;
    setComposerPos({ ...pos });
  }

  let resizeStart: { x: number; y: number; origW: number; origH: number } | null = null;
  function onHandlePointerDown(e: PointerEvent) {
    e.stopPropagation();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    resizeStart = { x: e.clientX, y: e.clientY, origW: size.w, origH: size.h };
  }
  function onHandlePointerMove(e: PointerEvent) {
    if (!resizeStart) return;
    const nw = resizeStart.origW + (e.clientX - resizeStart.x);
    const nh = resizeStart.origH + (e.clientY - resizeStart.y);
    size = {
      w: Math.max(360, Math.min(window.innerWidth - 40, nw)),
      h: Math.max(200, Math.min(window.innerHeight - 40, nh)),
    };
  }
  function onHandlePointerUp() {
    if (!resizeStart) return;
    resizeStart = null;
    setComposerSize({ ...size });
  }

  let charCount = $derived(localText.length);
  let lineCount = $derived(localText === '' ? 1 : localText.split('\n').length);
</script>

{#if $uiPrefs.composerOpen}
  <!-- svelte-ignore a11y_no_redundant_roles -->
  <section
    bind:this={panelEl}
    class="composer"
    role="region"
    aria-label="Command composer"
    style="left: {pos.x}px; top: {pos.y}px; width: {size.w}px; height: {size.h}px"
  >
    <header
      class="chead"
      role="toolbar"
      aria-label="Composer header"
      tabindex="-1"
      onpointerdown={onHeaderPointerDown}
      onpointermove={onHeaderPointerMove}
      onpointerup={onHeaderPointerUp}
      oncontextmenu={(e) => e.preventDefault()}
    >
      <span class="ctitle">Composer</span>
      <span class="cmeta"
        >{charCount} char{charCount === 1 ? '' : 's'} · {lineCount} line{lineCount === 1
          ? ''
          : 's'}</span
      >
      <button class="cclose" onclick={closeComposer} aria-label="Close composer">
        <X size={14} />
      </button>
    </header>
    <textarea
      bind:this={textareaEl}
      bind:value={localText}
      oninput={onTextInput}
      onkeydown={onKeydown}
      class="ctextarea"
      spellcheck="false"
      autocomplete="off"
    ></textarea>
    <footer class="cfoot">
      <kbd>⌘⏎</kbd> send · <kbd>Esc</kbd> close · <kbd>⇧⏎</kbd> newline
    </footer>
    <button
      class="resize-handle"
      aria-label="Resize composer"
      onpointerdown={onHandlePointerDown}
      onpointermove={onHandlePointerMove}
      onpointerup={onHandlePointerUp}
    ></button>
  </section>
{/if}

<style>
  .composer {
    position: fixed;
    z-index: 100;
    display: flex;
    flex-direction: column;
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
    overflow: hidden;
  }
  @media (prefers-reduced-motion: no-preference) {
    .composer {
      animation: composer-slide-up 180ms ease-out;
    }
  }
  @keyframes composer-slide-up {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  .chead {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    background: var(--color-muted);
    border-bottom: 1px solid var(--color-border);
    cursor: move;
    user-select: none;
    font-size: 12px;
  }
  .ctitle {
    font-weight: 600;
    color: var(--color-input-text);
  }
  .cmeta {
    flex: 1;
    color: var(--color-status-text);
    font-size: 11px;
  }
  .cclose {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--color-status-text);
    padding: 2px;
    border-radius: 4px;
  }
  .cclose:hover {
    color: var(--color-input-text);
    background: color-mix(in srgb, var(--color-primary) 10%, transparent);
  }
  .ctextarea {
    flex: 1;
    padding: 10px 12px;
    background: var(--color-input-background);
    border: none;
    outline: none;
    color: var(--color-input-text);
    font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', monospace;
    font-size: 14px;
    line-height: 1.5;
    resize: none;
  }
  .cfoot {
    padding: 4px 10px;
    background: var(--color-muted);
    border-top: 1px solid var(--color-border);
    font-size: 10px;
    color: var(--color-status-text);
  }
  .cfoot kbd {
    font-family: inherit;
    padding: 0 3px;
    border: 1px solid var(--color-border);
    border-radius: 3px;
    font-size: 9px;
  }
  .resize-handle {
    position: absolute;
    right: 0;
    bottom: 0;
    width: 14px;
    height: 14px;
    background: none;
    border: none;
    cursor: nwse-resize;
  }
  .resize-handle::before {
    content: '';
    position: absolute;
    right: 3px;
    bottom: 3px;
    width: 8px;
    height: 8px;
    border-right: 2px solid var(--color-border);
    border-bottom: 2px solid var(--color-border);
  }
</style>
