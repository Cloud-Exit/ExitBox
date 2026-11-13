<script lang="ts">
	import { untrack } from 'svelte';
	import { app, OPEN_DRAG, readDrop } from './app.svelte';
	import KeyBar from './mobile/KeyBar.svelte';
	import SessionHeader from './mobile/SessionHeader.svelte';
	import Tabs from './Tabs.svelte';
	import TerminalWindow from './TerminalWindow.svelte';
	import TopActions from './TopActions.svelte';
	import Button from './ui/Button.svelte';
	import Chip from './ui/Chip.svelte';
	import Icon from './ui/Icon.svelte';
	import SectionLabel from './ui/SectionLabel.svelte';
	import { viewport } from './viewport.svelte';

	const terminalCount = $derived(app.data?.running?.length ?? 0);
	let dropOver = $state(false);
	/** A phone shows the tab's focused session; the desktop shows every window of the tab. */
	const empty = $derived(viewport.phone ? !app.shown : !app.tabWindows(app.activeTab).length);

	/** The drop zone under a sidebar drag; an open window may be dropped beside its own tile, never onto it. */
	function zoneAt(e: DragEvent) {
		const desk = (e.currentTarget as HTMLElement).getBoundingClientRect();
		return app.zoneAt(e.clientX - desk.left, e.clientY - desk.top, dragId());
	}

	function dragId() {
		const drag = app.sidebarDrag;
		return drag?.kind === 'window' ? drag.id : null;
	}

	// The desktop size bounds the windows; keep them reachable when it shrinks.
	function measure(node: HTMLElement) {
		const update = () => {
			// A phone shows one session full size: the desktop layout keeps its own sizes.
			if (viewport.phone) return;
			app.desk.w = node.clientWidth;
			app.desk.h = node.clientHeight;
			for (const w of app.windows) w.clamp();
			app.retile();
		};
		const ro = new ResizeObserver(update);
		ro.observe(node);
		// Back from a phone layout of the same size, the observer stays quiet.
		$effect(() => {
			if (!viewport.phone) untrack(update);
		});
		return () => ro.disconnect();
	}
</script>

<div class={['flex min-h-0 flex-1 flex-col', app.view !== 'sessions' && 'hidden']}>
	{#if viewport.phone}<SessionHeader />{/if}
	<div class="flex min-h-12 shrink-0 flex-wrap items-center gap-2.5 border-b border-line-1 px-4 py-1.5 max-md:hidden">
		<h1 class="sr-only">Sessions</h1>
		<Button variant="primary" icon="plus" onclick={() => (app.newSessionOpen = true)}>New session</Button>
		<!-- Tabs take the free width and scroll, so the toolbar wraps only when narrow. -->
		<Tabs />
		<span class="font-mono text-[11.5px] whitespace-nowrap text-ink-2">{app.running + terminalCount} running</span>
		<SectionLabel>Windows</SectionLabel>
		<Chip icon="stack" pressed={app.autoDock && !app.custom && app.dockLayout === 'stack'} title="Dock: the first window on the left, the rest stacked on the right" onclick={() => app.setDock('stack')}><span class="max-2xl:sr-only">Main + stack</span></Chip>
		<Chip icon="columns" pressed={app.autoDock && !app.custom && app.dockLayout === 'columns'} title="Dock side by side" onclick={() => app.setDock('columns')}><span class="max-2xl:sr-only">Columns</span></Chip>
		<Chip icon="grid" pressed={app.autoDock && !app.custom && app.dockLayout === 'grid'} title="Dock in a grid" onclick={() => app.setDock('grid')}><span class="max-2xl:sr-only">Grid</span></Chip>
		{#if app.custom}
			<Chip icon="dock" pressed tabindex={-1} title="This tab is arranged by hand. Pick Main + stack, Columns or Grid to reset it.">Custom</Chip>
		{/if}
		<Chip icon="cascade" pressed={!app.autoDock} title="No docking: windows stay where you put them" onclick={() => app.setDock(null)}><span class="max-2xl:sr-only">Free</span></Chip>
		<span class="mx-0.5 h-5 w-px bg-line-1 max-md:hidden"></span>
		<TopActions />
	</div>

	<div
		data-desktop
		{@attach measure}
		role="presentation"
		ondragover={(e) => {
			if (!e.dataTransfer?.types.includes(OPEN_DRAG)) return;
			e.preventDefault();
			dropOver = true;
			app.previewDrop(dragId(), zoneAt(e));
		}}
		ondragleave={(e) => {
			// Moving onto a window inside the desktop is not leaving it.
			const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
			if (e.clientX > r.left && e.clientX < r.right && e.clientY > r.top && e.clientY < r.bottom) return;
			dropOver = false;
			app.clearDrop();
		}}
		ondrop={(e) => {
			const drop = readDrop(e);
			const zone = zoneAt(e);
			dropOver = false;
			app.clearDrop();
			if (!drop) return;
			e.preventDefault();
			app.openDropped(drop, null, zone);
		}}
		class={['relative min-h-0 flex-1 overflow-hidden bg-surface-0 max-md:bg-inset', dropOver && !app.dropPreview && 'outline-2 -outline-offset-4 outline-accent']}
	>
		{#each app.windows as w, i (w.id)}
			<TerminalWindow win={w} number={i + 1} />
		{/each}
		{#if app.dropPreview && !viewport.phone}
			{@const r = app.dropPreview}
			<!-- Above every window, the dragged one included, so the landing spot stays visible. -->
			<div
				data-drop-preview
				class="pointer-events-none absolute z-[100000] rounded-xl border-2 border-accent bg-accent/20 transition-[left,top,width,height] duration-100"
				style:left={r.x + 'px'}
				style:top={r.y + 'px'}
				style:width={r.w + 'px'}
				style:height={r.h + 'px'}
			></div>
		{/if}
		{#if empty && viewport.phone}
			<div class="absolute inset-0 grid place-items-center bg-surface-0 text-center text-ink-2">
				<div class="flex w-full max-w-[360px] flex-col items-center gap-[12px] px-[12px] py-[24px]">
					<span class="grid size-10 place-items-center rounded-[10px] bg-surface-3 text-accent"><Icon name="terminal" size={20} /></span>
					<h2 class="text-lg font-bold text-ink-1">
						{app.tabWindows(app.activeTab).length ? 'No session shown' : app.windows.length ? 'This tab is empty' : 'No open sessions'}
					</h2>
					<span>{app.windows.length || terminalCount ? 'Pick one from the sessions list, or start a new one.' : 'Start an agent in a project folder.'}</span>
					<Button variant="primary" icon="plus" class="w-full justify-center" onclick={() => (app.newSessionOpen = true)}>New session</Button>
					{#if app.windows.length || terminalCount}
						<Button icon="terminal" class="w-full justify-center" onclick={() => (app.sessionsOpen = true)}>Open sessions</Button>
					{/if}
				</div>
			</div>
		{:else if empty}
			<div class="absolute inset-0 grid place-items-center text-center text-ink-2">
				<div class="flex max-w-[420px] flex-col items-center gap-3 p-6">
					<span class="grid size-10 place-items-center rounded-[10px] bg-surface-3 text-accent"><Icon name="terminal" size={20} /></span>
					<h2 class="text-lg font-bold text-ink-1">{app.windows.length ? 'This tab is empty' : 'No open sessions'}</h2>
					{#if terminalCount}
						<span>
							{terminalCount === 1 ? 'One session runs' : `${terminalCount} sessions run`} in a terminal. Click one in the sidebar to open it here, or start a new one.
						</span>
					{:else}
						<span>Start an agent in a project folder. Each session opens in its own window that you can move and resize.</span>
					{/if}
					<Button variant="primary" icon="plus" onclick={() => (app.newSessionOpen = true)}>New session</Button>
				</div>
			</div>
		{/if}
	</div>
	{#if viewport.phone && app.shown}
		{#key app.shown}<KeyBar win={app.shown} />{/key}
	{/if}
</div>
