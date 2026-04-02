<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { LogOut, ArrowLeftRight, Palette } from 'lucide-svelte';
  import { authState, clearAuth } from '$lib/stores/authStore';
  import {
    activeTheme,
    themePreferences,
    setTheme,
    setTerminalBlackBackground,
    getAvailableThemes,
  } from '$lib/stores/themeStore';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { goto } from '$app/navigation';
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
  import defaultDark from '$lib/theme/default-dark.json';
  import defaultLight from '$lib/theme/default-light.json';
  import classicDark from '$lib/theme/classic-dark.json';
  import classicLight from '$lib/theme/classic-light.json';
  import type { Theme } from '$lib/theme/types';

  const themeData: Record<string, Theme> = {
    'default-dark': defaultDark as Theme,
    'default-light': defaultLight as Theme,
    'classic-dark': classicDark as Theme,
    'classic-light': classicLight as Theme,
  };

  const client = createClient(WebService, transport);
  const availableThemes = getAvailableThemes();

  let themeId = $derived($themePreferences.themeId);

  async function handleLogout() {
    try {
      await client.webLogout({ playerSessionToken: $authState.playerSessionToken ?? '' });
    } catch {
      /* best effort */
    }
    clearAuth();
    goto('/');
  }

  function handleSwitchCharacter() {
    goto('/characters');
  }

  function displayName(id: string): string {
    return id.split('-').map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(' ');
  }
</script>

<header>
  <div class="left">
    <a href="/" class="logo">
      <span class="logo-icon">H</span>
      <span class="logo-text">HoloMUSH</span>
    </a>
  </div>
  <nav class="right">
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
                  <span
                    class="swatch"
                    style="background: {themeData[theme.id]?.colors.background ?? '#000'}"
                  ></span>
                  <span
                    class="swatch"
                    style="background: {themeData[theme.id]?.colors.primary ?? '#888'}"
                  ></span>
                  <span
                    class="swatch"
                    style="background: {themeData[theme.id]?.colors.accent ?? '#888'}"
                  ></span>
                </span>
                {displayName(theme.id)}
              </span>
            </DropdownMenu.RadioItem>
          {/each}
        </DropdownMenu.RadioGroup>
        <DropdownMenu.Separator />
        <DropdownMenu.CheckboxItem
          bind:checked={$themePreferences.terminalBlackBackground}
          onCheckedChange={(v) => setTerminalBlackBackground(v === true)}
        >
          Black terminal background
        </DropdownMenu.CheckboxItem>
      </DropdownMenu.Content>
    </DropdownMenu.Root>

    {#if !$authState.playerSessionToken && !$authState.sessionId}
      <a href="/login" class="nav-link">Login</a>
      <a href="/register" class="nav-link accent">Register</a>
    {:else if $authState.sessionId && $authState.characterName}
      <span class="char-name">{$authState.characterName}</span>
      <button class="icon-btn" onclick={handleSwitchCharacter} title="Switch character" aria-label="Switch character">
        <ArrowLeftRight size={16} />
      </button>
      <button class="icon-btn" onclick={handleLogout} title="Logout" aria-label="Log out">
        <LogOut size={16} />
      </button>
    {:else if $authState.playerSessionToken}
      <span class="player-name">{$authState.playerName}</span>
      <button class="icon-btn" onclick={handleLogout} title="Logout" aria-label="Log out">
        <LogOut size={16} />
      </button>
    {/if}
  </nav>
</header>

<style>
  header {
    height: 36px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 12px;
    background: var(--color-surface);
    border-bottom: 1px solid var(--color-border);
    flex-shrink: 0;
    font-size: 13px;
  }

  .left {
    display: flex;
    align-items: center;
  }

  .logo {
    display: flex;
    align-items: center;
    gap: 6px;
    text-decoration: none;
    color: var(--color-input-text);
  }

  .logo-icon {
    width: 22px;
    height: 22px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--color-primary);
    color: var(--color-primary-foreground);
    border-radius: 4px;
    font-weight: bold;
    font-size: 12px;
    flex-shrink: 0;
  }

  .logo-text {
    color: var(--color-primary);
    font-weight: 600;
    letter-spacing: 0.05em;
  }

  .right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .nav-link {
    color: var(--color-input-text);
    text-decoration: none;
    padding: 2px 8px;
    border-radius: 4px;
    border: 1px solid var(--color-border);
    transition: border-color 0.15s;
  }

  .nav-link:hover {
    border-color: var(--color-primary);
  }

  .nav-link.accent {
    background: var(--color-primary);
    color: var(--color-primary-foreground);
    border-color: var(--color-primary);
  }

  .char-name,
  .player-name {
    color: var(--color-primary);
    font-size: 13px;
  }

  .icon-btn {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--color-status-text);
    display: flex;
    align-items: center;
    padding: 2px;
    border-radius: 4px;
    transition: color 0.15s;
  }

  .icon-btn:hover {
    color: var(--color-input-text);
  }

  .theme-option {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .theme-swatches {
    display: flex;
    gap: 2px;
  }

  .swatch {
    display: inline-block;
    width: 12px;
    height: 12px;
    border-radius: 2px;
    border: 1px solid var(--color-border);
  }
</style>
