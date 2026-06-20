<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import {
    Sheet,
    SheetContent,
    SheetHeader,
    SheetTitle,
    SheetDescription,
  } from '$lib/components/ui/sheet/index.js';
  import CreateSceneForm from './CreateSceneForm.svelte';

  let {
    open = $bindable(false),
    characters = [],
  }: {
    open?: boolean;
    characters?: { characterId: string; name?: string; characterName?: string }[];
  } = $props();
</script>

<Sheet bind:open>
  <SheetContent side="right" class="p-0 flex flex-col">
    <SheetHeader class="px-4 pt-4 pb-2">
      <SheetTitle>New scene</SheetTitle>
      <SheetDescription class="sr-only">Create a new scene you own</SheetDescription>
    </SheetHeader>
    <CreateSceneForm {characters} onDone={() => (open = false)} />
  </SheetContent>
</Sheet>
