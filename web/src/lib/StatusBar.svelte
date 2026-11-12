<script lang="ts">
	import { app } from './app.svelte';
	import Button from './ui/Button.svelte';
	import Kbd from './ui/Kbd.svelte';
</script>

<div class="flex min-h-[30px] shrink-0 flex-wrap items-center gap-x-3.5 gap-y-1.5 border-t border-line-1 px-4 py-1 text-[11.5px] text-ink-2">
	{#if app.autoDock}
		<span>Drag a title bar onto another window to swap them, an edge to resize. Double-click to maximize.</span>
	{:else}
		<span>Drag a title bar to move, an edge to resize, double-click to maximize.</span>
	{/if}
	<span><Kbd>Ctrl+B</Kbd> <Kbd>d</Kbd> detach</span>
	<span><Kbd>Ctrl+Alt+P</Kbd> workspaces</span>
	<span><Kbd>Ctrl+Alt+S</Kbd> sessions</span>
	<span class="flex-1"></span>
	<Button size="sm" variant="ghost" icon="sun" title="Switch light or dark" onclick={() => app.toggleTheme()}>Theme</Button>
	{#if app.data?.authEnabled}
		<Button size="sm" variant="ghost" icon="logout" onclick={() => app.signOut()}>Sign out</Button>
	{/if}
	<span class="font-mono">exitbox web · {app.data?.listen}</span>
</div>
