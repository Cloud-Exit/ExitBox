<script lang="ts">
	import { app } from './app.svelte';
	import Icon from './ui/Icon.svelte';
	import IconButton from './ui/IconButton.svelte';

	// Inline rename: the tab being renamed and its draft.
	let editing = $state<string | null>(null);
	let draft = $state('');

	function startRename(id: string, name: string) {
		editing = id;
		draft = name;
	}

	function commit() {
		if (editing) app.renameTab(editing, draft);
		editing = null;
	}

	function focusSelect(node: HTMLInputElement) {
		node.focus();
		node.select();
	}
</script>

<div role="tablist" aria-label="Tabs" class="flex min-w-0 shrink items-center gap-1 overflow-x-auto">
	{#each app.tabs as t (t.id)}
		{@const count = app.tabWindows(t.id).length}
		{@const active = app.activeTab === t.id}
		<div
			data-tab-id={t.id}
			class={[
				'group flex h-8 shrink-0 items-center rounded-md border text-[12.5px]',
				active ? 'border-line-2 bg-surface-3 text-ink-1' : 'border-transparent text-ink-2 hover:bg-surface-2 hover:text-ink-1',
				app.dropTab === t.id && 'outline-2 outline-accent'
			]}
		>
			{#if editing === t.id}
				<input
					bind:value={draft}
					{@attach focusSelect}
					aria-label="Tab name"
					spellcheck="false"
					maxlength="40"
					onkeydown={(e) => {
						if (e.key === 'Enter') {
							e.preventDefault();
							commit();
						} else if (e.key === 'Escape') {
							e.preventDefault();
							editing = null;
						}
					}}
					onblur={commit}
					class="mx-1 h-6 w-28 rounded border border-brand bg-surface-0 px-1.5 text-[12.5px] outline-none"
				/>
			{:else}
				<button
					type="button"
					role="tab"
					aria-selected={active}
					title="Double-click to rename. Drop a window here to move it to this tab."
					onclick={() => app.setTab(t.id)}
					ondblclick={() => startRename(t.id, t.name)}
					class="flex h-full items-center gap-1.5 pr-1 pl-2.5 font-semibold whitespace-nowrap"
				>
					{t.name}
					<span class={['min-w-[18px] rounded px-1 text-center font-mono text-[10.5px]', active ? 'bg-surface-2 text-ink-2' : 'text-ink-3']}>{count}</span>
				</button>
				{#if app.tabs.length > 1}
					<IconButton
						icon="x"
						label={'Close tab ' + t.name + (count ? ' (its windows move to the next tab)' : '')}
						small
						class={['mr-0.5', !active && 'invisible group-hover:visible']}
						onclick={() => app.closeTab(t.id)}
					/>
				{:else}
					<span class="w-1.5"></span>
				{/if}
			{/if}
		</div>
	{/each}
	<button type="button" aria-label="New tab" title="New tab" onclick={() => app.addTab()} class="grid size-8 shrink-0 place-items-center rounded-md text-ink-3 hover:bg-surface-2 hover:text-ink-1">
		<Icon name="plus" />
	</button>
</div>
