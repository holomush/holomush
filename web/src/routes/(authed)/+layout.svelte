<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { page } from '$app/stores';
  import SectionRail from '$lib/components/shell/SectionRail.svelte';
  import ShellFooter from '$lib/components/shell/ShellFooter.svelte';
  import { Sheet, SheetContent, SheetTitle, SheetDescription } from '$lib/components/ui/sheet';
  import { mobileNavOpen, openMobileNav, closeMobileNav } from '$lib/stores/mobileNavStore';
  import { authState } from '$lib/stores/authStore';

  let { children } = $props();
  let pathname = $derived($page.url.pathname);
  // Guests don't see registered-player-only sections (e.g. Scenes); the Rail
  // and palette share the same registry gate (ADR holomush-stds8). The /scenes
  // route + scene-access facade (INV-SCENE-64) remain the server-side guard.
  let isGuest = $derived($authState.isGuest);
</script>

<div class="shell">
  <SectionRail {pathname} {isGuest} variant="rail" />
  <div class="section-col">
    <div class="section-slot">{@render children()}</div>
    <ShellFooter {pathname} />
  </div>
</div>

<!-- Mobile drawer: same Rail, controlled by the shared store (controlled mode
     per holomush-ceon — do not bind:open through a store expression). -->
<Sheet open={$mobileNavOpen} onOpenChange={(o: boolean) => (o ? openMobileNav() : closeMobileNav())}>
  <SheetContent side="left" class="p-0 w-[260px]">
    <SheetTitle class="sr-only">Navigation</SheetTitle>
    <SheetDescription class="sr-only">Switch workspace section</SheetDescription>
    <SectionRail {pathname} {isGuest} variant="drawer" onnavigate={closeMobileNav} />
  </SheetContent>
</Sheet>

<style>
  .shell {
    display: flex;
    height: calc(100vh - var(--topbar-h));
    min-height: 0;
  }
  .section-col {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .section-slot {
    flex: 1;
    min-height: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
</style>
