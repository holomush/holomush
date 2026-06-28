<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import {
    Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription,
  } from '$lib/components/ui/sheet/index.js';
  import SceneSettingsForm from './SceneSettingsForm.svelte';

  let { open = $bindable(false), sceneId, characterId }: {
    open?: boolean;
    sceneId: string;
    characterId: string;
  } = $props();
</script>

<Sheet bind:open>
  <SheetContent side="right" class="p-0 flex flex-col">
    <SheetHeader class="px-4 pt-4 pb-2">
      <SheetTitle>Scene settings</SheetTitle>
      <SheetDescription class="sr-only">Edit settings for this scene</SheetDescription>
    </SheetHeader>
    {#if open}
      <SceneSettingsForm {sceneId} {characterId} onDone={() => (open = false)} />
    {/if}
  </SheetContent>
</Sheet>
