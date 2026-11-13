<script lang="ts">
	import { tick } from 'svelte';
	import { app, OPEN_DRAG, readDrop } from './app.svelte';
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

	// Reordering: a tab dragged past a few pixels drops before the tab under the pointer.
	let strip = $state<HTMLDivElement>();
	let drag = $state<{ id: string; x: number; moved: boolean } | null>(null);
	/** Insertion index under the pointer while dragging a tab. */
	let dropAt = $state<number | null>(null);
	/** The click that ends a drag must not select the tab. */
	let dragged = false;

	function indexAt(x: number) {
		const els = [...(strip?.querySelectorAll<HTMLElement>('[data-tab-id]') ?? [])];
		const i = els.findIndex((el) => {
			const r = el.getBoundingClientRect();
			return x < r.left + r.width / 2;
		});
		return i < 0 ? els.length : i;
	}

	function dragStart(e: PointerEvent, id: string) {
		dragged = false;
		if (e.button !== 0 || editing) return;
		drag = { id, x: e.clientX, moved: false };
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
	}

	function dragMove(e: PointerEvent) {
		if (!drag) return;
		if (!drag.moved && Math.abs(e.clientX - drag.x) < 5) return;
		drag.moved = true;
		dropAt = indexAt(e.clientX);
	}

	function dragEnd(commit: boolean) {
		if (drag?.moved) {
			dragged = true;
			if (commit && dropAt !== null) app.moveTab(drag.id, dropAt);
		}
		drag = null;
		dropAt = null;
	}

	/** Alt+Shift+Left or Right moves the focused tab. */
	async function keyMove(e: KeyboardEvent, id: string, i: number) {
		if (!e.altKey || !e.shiftKey || (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight')) return;
		e.preventDefault();
		app.moveTab(id, e.key === 'ArrowLeft' ? i - 1 : i + 2);
		await tick();
		strip?.querySelector<HTMLElement>(`[data-tab-id="${id}"] [role="tab"]`)?.focus();
	}

	function focusSelect(node: HTMLInputElement) {
		node.focus();
		node.select();
	}
</script>

<div bind:this={strip} role="tablist" aria-label="Tabs" class="flex min-w-[120px] flex-[1_1_0] items-center gap-1 overflow-x-auto">
	{#each app.tabs as t, i (t.id)}
		{@const count = app.tabWindows(t.id).length}
		{@const active = app.activeTab === t.id}
		<div
			data-tab-id={t.id}
			role="presentation"
			ondragover={(e) => {
				if (e.dataTransfer?.types.includes(OPEN_DRAG)) {
					e.preventDefault();
					app.dropTab = t.id;
				}
			}}
			ondragleave={() => {
				if (app.dropTab === t.id) app.dropTab = null;
			}}
			ondrop={(e) => {
				const drop = readDrop(e);
				app.dropTab = null;
				if (!drop) return;
				e.preventDefault();
				app.openDropped(drop, t.id, null);
			}}
			class={[
				'group relative flex h-8 shrink-0 items-center rounded-md border text-[12.5px]',
				active ? 'border-line-2 bg-surface-3 text-ink-1' : 'border-transparent text-ink-2 hover:bg-surface-2 hover:text-ink-1',
				app.dropTab === t.id && 'outline-2 outline-accent',
				drag?.moved && drag.id === t.id && 'opacity-50'
			]}
		>
			{#if drag?.moved && (dropAt === i || (dropAt === app.tabs.length && i === app.tabs.length - 1)) && app.tabs[dropAt]?.id !== drag.id && app.tabs[dropAt - 1]?.id !== drag.id}
				<span class={['pointer-events-none absolute inset-y-0.5 w-0.5 rounded-full bg-accent', dropAt === i ? '-left-[3px]' : '-right-[3px]']}></span>
			{/if}
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
					title="Drag to reorder, double-click to rename. Drop a window here to move it to this tab."
					onclick={() => {
						if (dragged) dragged = false;
						else app.setTab(t.id);
					}}
					ondblclick={() => startRename(t.id, t.name)}
					onpointerdown={(e) => dragStart(e, t.id)}
					onpointermove={dragMove}
					onpointerup={() => dragEnd(true)}
					onpointercancel={() => dragEnd(false)}
					onkeydown={(e) => keyMove(e, t.id, i)}
					class="flex h-full items-center gap-1.5 pr-1 pl-2.5 font-semibold whitespace-nowrap select-none"
				>
					{t.name}
					{#if app.tabWindows(t.id).some((w) => w.prompts.length)}
						<span title="A window here waits for your answer" class="size-1.5 rounded-full bg-amber ring-2 ring-amber/40"></span>
					{/if}
<span class={['grid h-[20px] min-w-[20px] place-items-center rounded-full px-[5px] font-mono text-[10.5px]', active ? 'bg-surface-2 text-ink-2' : 'text-ink-3']}>{count}</span>
				</button>
				{#if app.tabs.length > 1}
					<IconButton
						icon="x"
						label={'Close tab ' + t.name + (count ? ' (its windows move to the tab next to it)' : '')}
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
