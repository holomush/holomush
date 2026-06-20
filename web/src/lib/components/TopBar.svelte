<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { LogOut, ArrowLeftRight, Palette, PanelRightOpen, Command as CommandIcon, Menu } from '@lucide/svelte';
  import { page } from '$app/stores';
  import { authState, clearAuth } from '$lib/stores/authStore';
  import {
    activeTheme,
    themePreferences,
    setTheme,
    setTerminalBlackBackground,
    getAvailableThemes,
  } from '$lib/stores/themeStore';
  import { location } from '$lib/stores/sidebarStore';
  import { connectionStatus } from '$lib/stores/connectionStore';
  import { toggleSidebar } from '$lib/stores/uiPrefsStore';
  import { toggleMobileNav } from '$lib/stores/mobileNavStore';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { goto } from '$app/navigation';
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
  import defaultDark from '$lib/theme/default-dark.json';
  import defaultLight from '$lib/theme/default-light.json';
  import warmDark from '$lib/theme/warm-dark.json';
  import warmLight from '$lib/theme/warm-light.json';
  import type { Theme } from '$lib/theme/types';

  const themeData: Record<string, Theme> = {
    'default-dark': defaultDark as Theme,
    'default-light': defaultLight as Theme,
    'warm-dark': warmDark as Theme,
    'warm-light': warmLight as Theme,
  };

  const client = createClient(WebService, transport);
  const availableThemes = getAvailableThemes();
  let isAuthed = $derived($authState.isPlayerAuthenticated || !!$authState.sessionId);

  let themeId = $derived($themePreferences.themeId);
  let onTerminal = $derived($page.route.id?.includes('/terminal') ?? false);

  async function handleLogout() {
    try { await client.webLogout({}); } catch { /* best effort */ }
    clearAuth();
    goto('/');
  }

  function handleSwitchCharacter() { goto('/characters'); }
  function displayName(id: string): string {
    return id.split('-').map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(' ');
  }
</script>

<header>
  <div class="left">
    {#if isAuthed}
      <button class="icon-btn mobile-only" onclick={toggleMobileNav} title="Menu" aria-label="Open navigation">
        <Menu size={18} />
      </button>
    {/if}
    <a href="/" class="logo brand-chip">
      <span class="logo-icon">H</span>
      <span class="logo-text">HoloMUSH</span>
    </a>
    {#if onTerminal && $authState.characterName}
      <span class="vdiv" aria-hidden="true"></span>
      <div class="breadcrumb" data-testid="topbar-breadcrumb">
        <span class="char">{$authState.characterName}</span>
        {#if $location?.name}
          <span class="sep">@</span>
          <span class="loc">{$location.name}</span>
          <span class="loc-id">#{$location.id.slice(0, 8)}</span>
        {/if}
      </div>
    {/if}
  </div>
  <nav class="right">
    {#if onTerminal}
      <span class="kbd-hint" aria-hidden="true">
        <CommandIcon size={12} /><kbd>K</kbd> palette
      </span>
      <span
        class="conn-pill"
        data-testid="conn-pill"
        data-status={$connectionStatus}
      >
        <span class="conn-dot" aria-hidden="true"></span>
        {#if $connectionStatus === 'connected'}connected{:else if $connectionStatus === 'syncing'}syncing{:else}disconnected{/if}
      </span>
      <span class="vdiv" aria-hidden="true"></span>
    {/if}

    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        {#snippet child({ props })}
          <button {...props} class="icon-btn" title="Theme" aria-label="Change theme">
            <Palette size={16} />
          </button>
        {/snippet}
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end" class="w-56">
        <DropdownMenu.Label>Theme</DropdownMenu.Label>
        <DropdownMenu.Separator />
        <DropdownMenu.RadioGroup value={themeId} onValueChange={(v) => v && setTheme(v)}>
          {#each availableThemes as theme (theme.id)}
            <DropdownMenu.RadioItem value={theme.id}>
              <span class="theme-option">
                <span class="theme-swatches">
                  <span class="swatch" style="background: {themeData[theme.id]?.colors.background ?? '#000'}"></span>
                  <span class="swatch" style="background: {themeData[theme.id]?.colors.primary ?? '#888'}"></span>
                  <span class="swatch" style="background: {themeData[theme.id]?.colors.accent ?? '#888'}"></span>
                </span>
                {displayName(theme.id)}
              </span>
            </DropdownMenu.RadioItem>
          {/each}
        </DropdownMenu.RadioGroup>
        <DropdownMenu.Separator />
        <DropdownMenu.CheckboxItem
          checked={$themePreferences.terminalBlackBackground}
          onCheckedChange={(v) => setTerminalBlackBackground(v === true)}
        >
          Black terminal background
        </DropdownMenu.CheckboxItem>
      </DropdownMenu.Content>
    </DropdownMenu.Root>

    {#if onTerminal}
      <button
        class="icon-btn"
        onclick={toggleSidebar}
        title="Toggle sidebar"
        aria-label="Toggle sidebar"
      >
        <PanelRightOpen size={16} />
      </button>
    {/if}

    {#if !$authState.isPlayerAuthenticated && !$authState.sessionId}
      <a href="/login" class="nav-link">Login</a>
      <a href="/register" class="nav-link accent">Register</a>
    {:else if $authState.sessionId && $authState.characterName}
      <span class="char-name" data-testid="topbar-char-name">{$authState.characterName}</span>
      <button class="icon-btn" onclick={handleSwitchCharacter} title="Switch character" aria-label="Switch character">
        <ArrowLeftRight size={16} />
      </button>
      <button class="icon-btn" onclick={handleLogout} title="Logout" aria-label="Log out">
        <LogOut size={16} />
      </button>
    {:else if $authState.isPlayerAuthenticated}
      <span class="player-name">{$authState.playerName}</span>
      <button class="icon-btn" onclick={handleLogout} title="Logout" aria-label="Log out">
        <LogOut size={16} />
      </button>
    {/if}
  </nav>
</header>

<style>
  header {
    height: var(--topbar-h);
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 12px;
    background: var(--color-surface);
    border-bottom: 1px solid var(--color-border);
    flex-shrink: 0;
    font-size: 13px;
  }
  .left { display: flex; align-items: center; gap: 10px; }
  .logo { display: flex; align-items: center; gap: 6px; text-decoration: none; color: var(--color-input-text); }
  .logo-icon {
    width: 22px; height: 22px;
    display: flex; align-items: center; justify-content: center;
    background: var(--color-primary); color: var(--color-primary-foreground);
    border-radius: 4px; font-weight: bold; font-size: 12px; flex-shrink: 0;
  }
  .logo-text { color: var(--color-primary); font-weight: 600; letter-spacing: 0.05em; }
  .vdiv {
    width: 1px; height: 20px;
    background: var(--color-border);
  }
  .breadcrumb { display: flex; align-items: center; gap: 6px; font-size: 13px; }
  .breadcrumb .char { color: var(--mush-pose-actor); }
  .breadcrumb .sep { color: var(--color-status-text); }
  .breadcrumb .loc { color: var(--color-input-text); }
  .breadcrumb .loc-id { color: var(--color-status-text); font-size: 11px; font-family: 'JetBrains Mono', monospace; }
  .right { display: flex; align-items: center; gap: 8px; }
  .kbd-hint {
    display: none;
    align-items: center;
    gap: 4px;
    color: var(--color-status-text);
    font-size: 11px;
  }
  @media (min-width: 768px) {
    .kbd-hint { display: inline-flex; }
  }
  .kbd-hint kbd {
    font-family: inherit; font-size: 11px;
    padding: 1px 4px;
    border: 1px solid var(--color-border);
    border-radius: 3px;
  }
  .conn-pill {
    display: inline-flex; align-items: center; gap: 5px;
    padding: 2px 8px;
    border-radius: 999px;
    font-size: 11px;
    background: var(--color-muted);
    color: var(--color-muted-foreground);
  }
  .conn-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--color-status-text); }
  .conn-pill[data-status="connected"] .conn-dot { background: var(--color-status-online); }
  .conn-pill[data-status="syncing"] .conn-dot { background: var(--color-accent); animation: dot-pulse 1200ms ease-in-out infinite; }
  .conn-pill[data-status="disconnected"] .conn-dot { background: var(--color-status-offline); }
  .nav-link {
    color: var(--color-input-text); text-decoration: none;
    padding: 2px 8px; border-radius: 4px; border: 1px solid var(--color-border);
    transition: border-color 0.15s;
  }
  .nav-link:hover { border-color: var(--color-primary); }
  .nav-link.accent { background: var(--color-primary); color: var(--color-primary-foreground); border-color: var(--color-primary); }
  .char-name, .player-name { color: var(--color-primary); font-size: 13px; }
  .icon-btn {
    background: none; border: none; cursor: pointer;
    color: var(--color-status-text);
    display: flex; align-items: center; padding: 2px; border-radius: 4px;
    transition: color 0.15s;
  }
  .icon-btn:hover { color: var(--color-input-text); }
  .theme-option { display: flex; align-items: center; gap: 8px; }
  .theme-swatches { display: flex; gap: 2px; }
  .swatch { display: inline-block; width: 12px; height: 12px; border-radius: 2px; border: 1px solid var(--color-border); }
  .mobile-only { display: inline-flex; }
  @media (min-width: 768px) {
    .mobile-only { display: none; }
  }
</style>
