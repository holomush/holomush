<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
	import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
	import * as Command from '$lib/components/ui/command/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { listAllCharacters, type DirectoryCharacter } from '$lib/scenes/directoryClient';

	let {
		characterId,
		selected = [],
		onChange,
	}: { characterId: string; selected?: string[]; onChange: (ids: string[]) => void } = $props();

	let open = $state(false);
	let chars = $state<DirectoryCharacter[]>([]);
	let loadError = $state(false);

	$effect(() => {
		listAllCharacters(characterId)
			.then((c) => (chars = c))
			.catch(() => (loadError = true));
	});

	function toggle(id: string) {
		const next = selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id];
		onChange(next);
	}
</script>

<Popover.Root bind:open>
	<Popover.Trigger>
		{#snippet child({ props })}
			<Button
				{...props}
				variant="outline"
				role="combobox"
				aria-expanded={open}
				class="w-full justify-between"
				name="invite-picker"
			>
				{selected.length ? `${selected.length} selected` : 'Invite characters…'}
				<ChevronsUpDownIcon data-icon="inline-end" class="opacity-50" />
			</Button>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content class="w-[260px] p-0">
		<Command.Root>
			<Command.Input placeholder="Search characters…" />
			<Command.List>
				<Command.Empty>{loadError ? 'Failed to load.' : 'No characters found.'}</Command.Empty>
				<Command.Group>
					{#each chars as c (c.id)}
						<Command.Item
							value={c.name}
							onSelect={() => toggle(c.id)}
							data-checked={selected.includes(c.id) ? 'true' : undefined}
						>
							{c.name}
						</Command.Item>
					{/each}
				</Command.Group>
			</Command.List>
		</Command.Root>
	</Popover.Content>
</Popover.Root>
