<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { page } from '$app/stores';
  import AdminNav from '$lib/components/admin/AdminNav.svelte';
  import * as Avatar from '$lib/components/ui/avatar';
  import * as Breadcrumb from '$lib/components/ui/breadcrumb';
  import { Toaster } from '$lib/components/ui/sonner';
  import { authState } from '$lib/stores/authStore';
  import { setAdminSections, clearAdminSections } from '$lib/stores/adminNavStore';

  let { data, children } = $props();

  // Read off the path rather than a route param: the active section is the
  // same fact for every child route, including ones with their own directory,
  // and the layout should not have to know which of them matched.
  let activeId = $derived($page.url.pathname.split('/')[2] || undefined);
  let activeName = $derived(data.sections.find((s) => s.id === activeId)?.displayName);
  let initials = $derived(
    ($authState.playerName ?? '')
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase() ?? '')
      .join('') || '·',
  );

  // The one shipped drawer lives in the parent layout and is closed over its
  // own rail, so a component mounted here cannot draw into it. The rail reads
  // this singleton itself instead — the same crossing mobileNavStore already
  // makes between the top bar and that layout. Cleared on teardown so the
  // group cannot outlive the route that supplied it.
  $effect(() => {
    setAdminSections(data.sections);
    return () => clearAdminSections();
  });
</script>

<!-- THE ONE TOASTER, mounted once for the whole admin portal. A second mount
     anywhere under /admin would double every receipt. It sits outside the
     frame so a mutation's receipt is not tied to which branch rendered. -->
<Toaster />

{#if data.loadFailed}
  <!-- One shared state, byte-identical for every caller. It names nothing
       about who is asking and nothing about what is behind it. -->
  <div class="retry" role="status">
    <p class="retry-copy">Couldn't load the admin portal. Try again.</p>
    <a class="retry-action" href="/admin" data-sveltekit-reload>Try again</a>
  </div>
{:else}
  <div class="adminframe">
    <div class="adminnav-col">
      <AdminNav sections={data.sections} {activeId} />
      <!-- Returns at the narrowed strip's foot, where the nav's own copy is
           clipped. Rendered only inside the 1023px band so it never doubles
           up at full width. Fallback only: no media path exists yet. -->
      <div class="rail-identity">
        <Avatar.Root class="size-7">
          <Avatar.Fallback class="identity-plate">{initials}</Avatar.Fallback>
        </Avatar.Root>
      </div>
    </div>

    <div class="content-col">
      <!-- BREADCRUMB STRIP ONLY. The drawer is opened by the control the top
           bar already ships, which is already hidden above 768px. Do not
           "complete" this with a second one: two controls opening one drawer,
           sitting inches apart, is worse than none. -->
      <div class="mobilebar">
        <Breadcrumb.Root>
          <Breadcrumb.List>
            <Breadcrumb.Item><Breadcrumb.Link href="/admin">Admin</Breadcrumb.Link></Breadcrumb.Item>
            {#if activeName}
              <Breadcrumb.Separator />
              <Breadcrumb.Item><Breadcrumb.Page>{activeName}</Breadcrumb.Page></Breadcrumb.Item>
            {/if}
          </Breadcrumb.List>
        </Breadcrumb.Root>
      </div>

      <div class="content-slot">{@render children()}</div>
    </div>
  </div>
{/if}

<style>
  /* Two columns. The rail is inherited from the parent shell one level up;
     rendering a second one here would double it.

     Every rule below is a VIEWPORT @media at the literals the shipped rail
     already uses, so the rail's collapse and this frame's collapse fire at the
     same moment BY CONSTRUCTION rather than by an argument about how wide some
     element happens to be. Nothing here declares containment: an element with
     layout containment becomes the containing block for every fixed-position
     descendant, and the authed subtree has several — plus every overlay, whose
     height percentages resolve against the viewport. */
  .adminframe {
    display: flex;
    flex: 1;
    min-height: 0;
  }
  .adminnav-col {
    width: var(--adminnav-w);
    flex: none;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: var(--color-sidebar-background);
    border-right: 1px solid var(--color-border);
    transition: width 180ms ease;
  }
  .content-col {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .content-slot {
    flex: 1;
    min-height: 0;
    overflow: auto;
  }

  .rail-identity {
    display: none;
    align-items: center;
    justify-content: center;
    padding: 8px 0 12px;
  }
  .rail-identity :global(.identity-plate) {
    background: color-mix(in srgb, var(--color-primary) 16%, transparent);
    border: 1px solid color-mix(in srgb, var(--color-primary) 32%, transparent);
    color: var(--color-primary);
    font-size: 11px;
    font-weight: 600;
  }

  .mobilebar {
    display: none;
    align-items: center;
    padding: 8px 12px;
    border-bottom: 1px solid var(--color-border);
  }

  .retry {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 16px;
    padding: 32px;
  }
  .retry-copy {
    font-family: var(--font-sans, system-ui);
    font-size: 14px;
    color: var(--color-muted-foreground);
  }
  .retry-action {
    font-family: var(--font-sans, system-ui);
    font-size: 12px;
    font-weight: 600;
    color: var(--color-primary);
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 10px 16px;
    min-height: 44px;
    display: inline-flex;
    align-items: center;
    text-decoration: none;
  }
  .retry-action:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }

  /* The nav narrows to a rail-width strip in THIS column, sitting adjacent to
     the inherited rail so the two read as one continuous column. The entries
     stay rendered here, where the awaited list actually is. */
  @media (max-width: 1023px) {
    .adminnav-col {
      width: var(--rail-w);
    }
    .rail-identity {
      display: flex;
    }
  }

  @media (max-width: 767px) {
    .adminnav-col {
      width: 0;
      border-right-width: 0;
    }
    .rail-identity {
      display: none;
    }
    .mobilebar {
      display: flex;
    }
  }
</style>
