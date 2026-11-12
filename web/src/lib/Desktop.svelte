<script lang="ts">
	import { app } from './app.svelte';
	import Tabs from './Tabs.svelte';
	import TerminalWindow from './TerminalWindow.svelte';
	import Button from './ui/Button.svelte';
	import Chip from './ui/Chip.svelte';
	import Icon from './ui/Icon.svelte';
	import SectionLabel from './ui/SectionLabel.svelte';

	const terminalCount = $derived(app.data?.running?.length ?? 0);

	// The desktop size bounds the windows; keep them reachable when it shrinks.
	function measure(node: HTMLElement) {
		const ro = new ResizeObserver(() => {
			app.desk.w = node.clientWidth;
			app.desk.h = node.clientHeight;
			for (const w of app.windows) w.clamp();
			app.retile();
		});
		ro.observe(node);
		return () => ro.disconnect();
	}
</script>

<div class={['flex min-h-0 flex-1 flex-col', app.view !== 'sessions' && 'hidden']}>
	<div class="flex min-h-12 shrink-0 flex-wrap items-center gap-2.5 border-b border-line-1 px-4 py-1.5">
		<h1 class="sr-only">Sessions</h1>
		<Button variant="primary" icon="plus" onclick={() => (app.newSessionOpen = true)}>New session</Button>
		<Tabs />
		<span class="flex-1"></span>
		<span class="font-mono text-[11.5px] whitespace-nowrap text-ink-2">{app.running + terminalCount} running</span>
		<SectionLabel>Windows</SectionLabel>
		<Chip icon="stack" pressed={app.autoDock && app.dockLayout === 'stack'} title="Dock: the first window on the left, the rest stacked on the right" onclick={() => app.setDock('stack')}>Main + stack</Chip>
		<Chip icon="columns" pressed={app.autoDock && app.dockLayout === 'columns'} title="Dock side by side" onclick={() => app.setDock('columns')}>Columns</Chip>
		<Chip icon="grid" pressed={app.autoDock && app.dockLayout === 'grid'} title="Dock in a grid" onclick={() => app.setDock('grid')}>Grid</Chip>
		<Chip icon="cascade" pressed={!app.autoDock} title="No docking: windows stay where you put them" onclick={() => app.setDock(null)}>Free</Chip>
	</div>

	<div data-desktop {@attach measure} class="relative min-h-0 flex-1 overflow-hidden bg-surface-0 max-md:min-h-[70vh]">
		{#each app.windows as w, i (w.id)}
			<TerminalWindow win={w} number={i + 1} />
		{/each}
		{#if !app.tabWindows(app.activeTab).length}
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
</div>
