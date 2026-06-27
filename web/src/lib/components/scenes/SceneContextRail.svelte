<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { cn } from '$lib/utils.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Separator } from '$lib/components/ui/separator/index.js';
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
  import type { WorkspaceScene } from '$lib/scenes/types';
  import { sceneStateDotClass } from '$lib/scenes/stateStyle';
  import { endSceneAction, pauseSceneAction, resumeSceneAction } from '$lib/scenes/lifecycleFlow';
  import { inviteCharacters, kickAction, transferAction, leaveAction } from '$lib/scenes/membershipFlow';
  import CharacterMultiSelect from './CharacterMultiSelect.svelte';

  let { scene }: { scene: WorkspaceScene | null } = $props();

  let isOwner = $derived(!!scene && scene.ownerId === scene.asCharacterId);
  let isParticipant = $derived(!!scene && (scene.role === 'owner' || scene.role === 'member'));
  let showPause = $derived(isOwner && scene?.state === 'active');
  let showEnd = $derived(isOwner && (scene?.state === 'active' || scene?.state === 'paused'));
  let showResume = $derived(isParticipant && scene?.state === 'paused');

  function formatRelativeTime(ms: bigint): string {
    if (!ms) return '';
    const diffMs = Date.now() - Number(ms);
    const mins = Math.floor(diffMs / 60_000);
    if (mins < 1) return 'just now';
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    return `${Math.floor(hours / 24)}d ago`;
  }

  const stateLabel: Record<string, string> = {
    active: 'Active',
    paused: 'Paused',
    ended: 'Ended',
    published: 'Published',
  };

  let inviteIds = $state<string[]>([]);
  let canManage = $derived(scene?.state === 'active' || scene?.state === 'paused');
  let lifecycleErr = $state('');
  let membershipErr = $state('');

  // The rail is reused across scene switches (persistent in ScenesShell), so
  // clear any surfaced error when the active scene changes — a stale alert from
  // one scene must not bleed into the next.
  $effect(() => {
    void scene?.sceneId;
    lifecycleErr = '';
    membershipErr = '';
  });

  async function runLifecycle(
    action: (a: { sceneId: string; characterId: string }) => Promise<void>,
  ): Promise<void> {
    if (!scene) return;
    lifecycleErr = '';
    try {
      await action({ sceneId: scene.sceneId, characterId: scene.asCharacterId });
    } catch (e) {
      lifecycleErr = e instanceof Error ? e.message : 'Action failed';
    }
  }

  // Membership actions (transfer/kick/invite/leave) have heterogeneous argument
  // shapes, so this takes a thunk rather than runLifecycle's uniform action.
  // It never rejects: failures (network, or a PermissionDenied from the facade
  // self-gate) land in membershipErr for the user instead of becoming an
  // unhandled promise rejection.
  async function runMembership(action: () => Promise<void>): Promise<void> {
    membershipErr = '';
    try {
      await action();
    } catch (e) {
      membershipErr = e instanceof Error ? e.message : 'Action failed';
    }
  }
</script>

<aside
  class="flex flex-col gap-0 overflow-y-auto border-l border-border bg-card h-full"
  aria-label="Scene context"
>
  {#if scene}
    <!-- Scene panel -->
    <section class="p-4 pb-3">
      <h2 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">Scene</h2>
      <div class="space-y-1.5">
        <div class="flex items-center gap-2">
          <span
            class={cn('size-2 shrink-0 rounded-full', sceneStateDotClass(scene.state))}
            aria-hidden="true"
          ></span>
          <span class="text-sm font-medium leading-snug">{scene.title}</span>
        </div>
        {#if scene.locationId}
          <p class="text-xs text-muted-foreground pl-4">@ {scene.locationId}</p>
        {/if}
        <p class="text-xs text-muted-foreground pl-4">{stateLabel[scene.state] ?? scene.state}</p>
        {#if scene.tags.length > 0}
          <div class="flex flex-wrap gap-1 pl-4 pt-1">
            {#each scene.tags as tag (tag)}
              <Badge variant="outline" class="text-[10px] h-4 px-1.5">{tag}</Badge>
            {/each}
          </div>
        {/if}
      </div>
      {#if showPause || showResume || showEnd}
        <div class="flex flex-wrap gap-1.5 pl-4 pt-2">
          {#if showPause}
            <Button variant="outline" size="sm" class="h-6 text-xs"
              onclick={() => runLifecycle(pauseSceneAction)}>Pause</Button>
          {/if}
          {#if showResume}
            <Button variant="outline" size="sm" class="h-6 text-xs"
              onclick={() => runLifecycle(resumeSceneAction)}>Resume</Button>
          {/if}
          {#if showEnd}
            <Button variant="outline" size="sm" class="h-6 text-xs text-destructive"
              onclick={() => runLifecycle(endSceneAction)}>End</Button>
          {/if}
        </div>
        {#if lifecycleErr}
          <p class="text-xs text-destructive pl-4 pt-1">{lifecycleErr}</p>
        {/if}
      {/if}
    </section>

    <Separator />

    <!-- Roster panel — populated once getScene returns data (bead .8.25).
         Falls back to acting-alt placeholder when participants is empty/absent. -->
    <section class="p-4 pb-3">
      <h2 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
        Roster
      </h2>
      <!-- Participants (owner/member) -->
      <div class="space-y-1">
        <p class="text-[11px] text-muted-foreground mb-1">Participants</p>
        {#if scene.participants && scene.participants.length > 0}
          {#each scene.participants as p (p.id)}
            <div class="flex items-center gap-2 text-sm">
              <span
                class="size-6 rounded-full bg-[var(--brand-cyan-deep)] flex items-center justify-center text-[10px] font-semibold text-white shrink-0"
                aria-hidden="true"
              >
                {p.name.slice(0, 2).toUpperCase()}
              </span>
              <span class="truncate">{p.name}</span>
              {#if p.id === scene.asCharacterId}
                <Badge variant="secondary" class="text-[9px] h-3.5 px-1 ml-auto shrink-0">you</Badge>
              {/if}
              {#if isOwner && p.id !== scene.asCharacterId && canManage}
                <DropdownMenu.Root>
                  <DropdownMenu.Trigger>
                    {#snippet child({ props })}
                      <button
                        {...props}
                        class="ml-auto px-1 text-muted-foreground"
                        aria-label={`Manage ${p.name}`}
                      >⋯</button>
                    {/snippet}
                  </DropdownMenu.Trigger>
                  <DropdownMenu.Content align="end">
                    <DropdownMenu.Item
                      onSelect={() =>
                        runMembership(() =>
                          transferAction({
                            sceneId: scene.sceneId,
                            characterId: scene.asCharacterId,
                            newOwnerCharacterId: p.id,
                          }),
                        )}
                    >
                      Transfer ownership
                    </DropdownMenu.Item>
                    <DropdownMenu.Item
                      variant="destructive"
                      onSelect={() =>
                        runMembership(() =>
                          kickAction({
                            sceneId: scene.sceneId,
                            characterId: scene.asCharacterId,
                            targetCharacterId: p.id,
                          }),
                        )}
                    >
                      Kick
                    </DropdownMenu.Item>
                  </DropdownMenu.Content>
                </DropdownMenu.Root>
              {/if}
            </div>
          {/each}
        {:else if scene.asCharacterName}
          <!-- Placeholder: acting alt only, until .8.25 populates the roster -->
          <div class="flex items-center gap-2 text-sm">
            <span
              class="size-6 rounded-full bg-[var(--brand-cyan-deep)] flex items-center justify-center text-[10px] font-semibold text-white shrink-0"
              aria-hidden="true"
            >
              {scene.asCharacterName.slice(0, 2).toUpperCase()}
            </span>
            <span class="truncate">{scene.asCharacterName}</span>
            <Badge variant="secondary" class="text-[9px] h-3.5 px-1 ml-auto shrink-0">you</Badge>
          </div>
        {:else}
          <p class="text-xs text-muted-foreground italic">Loading roster…</p>
        {/if}
      </div>

      <!-- Invite picker + Leave for participants (owner/member) -->
      {#if isParticipant}
        <div class="mt-2 space-y-1.5">
          <CharacterMultiSelect
            characterId={scene.asCharacterId}
            selected={inviteIds}
            onChange={(ids) => (inviteIds = ids)}
          />
          {#if inviteIds.length}
            <Button
              size="sm"
              class="h-6 text-xs"
              disabled={!canManage}
              onclick={() =>
                runMembership(() =>
                  inviteCharacters({
                    sceneId: scene.sceneId,
                    characterId: scene.asCharacterId,
                    targetIds: inviteIds,
                  }).then(() => {
                    inviteIds = [];
                  }),
                )}
            >
              Invite {inviteIds.length}
            </Button>
          {/if}
          {#if !isOwner}
            <Button
              variant="outline"
              size="sm"
              class="h-6 text-xs"
              disabled={!canManage}
              onclick={() =>
                runMembership(() =>
                  leaveAction({ sceneId: scene.sceneId, characterId: scene.asCharacterId }),
                )}
            >Leave</Button>
          {/if}
        </div>
      {/if}

      {#if membershipErr}
        <p class="text-xs text-destructive pt-2" role="alert">{membershipErr}</p>
      {/if}

      <!-- Observers listed separately per INV-SCENE-61 -->
      {#if scene.observers && scene.observers.length > 0}
        <div class="mt-3 space-y-1">
          <p class="text-[11px] text-muted-foreground mb-1">Observers</p>
          {#each scene.observers as obs (obs.id)}
            <div class="flex items-center gap-2 text-sm text-muted-foreground">
              <span
                class="size-6 rounded-full border border-border flex items-center justify-center text-[10px] font-semibold shrink-0"
                aria-hidden="true"
              >
                {obs.name.slice(0, 2).toUpperCase()}
              </span>
              <span class="truncate">{obs.name}</span>
              {#if obs.id === scene.asCharacterId}
                <Badge variant="outline" class="text-[9px] h-3.5 px-1 ml-auto shrink-0">watching</Badge>
              {/if}
            </div>
          {/each}
        </div>
      {:else if scene.role === 'observer'}
        <!-- Placeholder for acting observer when list not yet populated -->
        <div class="mt-3 space-y-1">
          <p class="text-[11px] text-muted-foreground mb-1">Observers</p>
          <div class="flex items-center gap-2 text-sm text-muted-foreground">
            <span
              class="size-6 rounded-full border border-border flex items-center justify-center text-[10px] font-semibold shrink-0"
              aria-hidden="true"
            >
              {scene.asCharacterName.slice(0, 2).toUpperCase()}
            </span>
            <span class="truncate">{scene.asCharacterName}</span>
            <Badge variant="outline" class="text-[9px] h-3.5 px-1 ml-auto shrink-0">watching</Badge>
          </div>
        </div>
      {/if}
    </section>

    <Separator />

    <!-- Activity panel -->
    <section class="p-4">
      <h2 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
        Activity
      </h2>
      <div class="space-y-1.5">
        <div class="flex justify-between text-xs">
          <span class="text-muted-foreground">Entries</span>
          <span class="font-medium tabular-nums">{scene.entryCount?.toString() ?? '0'}</span>
        </div>
        {#if scene.lastActivityMs}
          <div class="flex justify-between text-xs">
            <span class="text-muted-foreground">Last activity</span>
            <span class="font-medium">{formatRelativeTime(scene.lastActivityMs)}</span>
          </div>
        {/if}
      </div>
    </section>
  {:else}
    <div class="flex flex-1 items-center justify-center p-8 text-center">
      <p class="text-sm text-muted-foreground">Select a scene to view details</p>
    </div>
  {/if}
</aside>
