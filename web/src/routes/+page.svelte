<script lang="ts">
	import { app } from '$lib/app.svelte';
	import Desktop from '$lib/Desktop.svelte';
	import Help from '$lib/Help.svelte';
	import Login from '$lib/Login.svelte';
	import NewSession from '$lib/NewSession.svelte';
	import NoLoginBar from '$lib/NoLoginBar.svelte';
	import Prompt from '$lib/Prompt.svelte';
	import SessionSettings from '$lib/SessionSettings.svelte';
	import Settings from '$lib/settings/Settings.svelte';
	import Sidebar from '$lib/Sidebar.svelte';
	import Toast from '$lib/Toast.svelte';
	import Button from '$lib/ui/Button.svelte';
	import Callout from '$lib/ui/Callout.svelte';
	import { viewport } from '$lib/viewport.svelte';

	app.boot();
</script>

<svelte:window
	onstorage={(e) => {
		if (e.key === 'exitbox.theme') app.applyTheme();
	}}
	onkeydown={(e) => {
		// Never from a field or a terminal (xterm types into a textarea), nor over a dialog.
		if (e.key !== '?' || e.ctrlKey || e.metaKey || e.altKey || app.view === 'login') return;
		const t = e.target as HTMLElement | null;
		if (t?.closest('input, textarea, select, [contenteditable], .xterm') || document.querySelector('[role="dialog"]')) return;
		e.preventDefault();
		app.helpOpen = true;
	}}
/>

{#if app.view === 'login'}
	<Login />
{:else if app.view === 'offline'}
	<div class="grid min-h-screen place-items-center p-4 max-md:min-h-dvh max-md:px-[12px]">
		<div class="flex flex-col items-center gap-3 max-md:w-full max-md:items-stretch">
			<Callout tone="bad">Cannot reach exitbox web: {app.offlineReason}. Retrying every few seconds.</Callout>
			<Button icon="rotate" class="max-md:justify-center" onclick={() => app.retry()}>Retry now</Button>
		</div>
	</div>
{:else if app.view === 'sessions' || app.view === 'settings'}
	<!-- On a phone the keyboard shrinks the visual viewport only (iOS): follow it so the key bar stays above the keyboard. -->
	<div
		style:height={viewport.phone && viewport.height ? viewport.height + 'px' : undefined}
		class="flex h-screen flex-col overflow-hidden max-md:h-dvh max-md:bg-surface-1 max-md:pt-[env(safe-area-inset-top)] max-md:pb-[env(safe-area-inset-bottom)]"
	>
		<!-- Takes layout space, so the desktop measures and tiles below it. -->
		{#if app.me && !app.me.authEnabled}<NoLoginBar />{/if}
		<div class="flex min-h-0 flex-1">
			<Sidebar />
			<!-- Hidden, not unmounted, under the phone's sessions list: terminals keep their connections. -->
			<main class={['flex min-w-0 flex-1 flex-col', viewport.phone && app.sessionsOpen && app.view === 'sessions' && 'hidden']}>
				<Desktop />
				<Settings />
			</main>
		</div>
	</div>
	{#if app.newSessionOpen && app.data}
		<NewSession data={app.data} />
	{/if}
	{#if app.helpOpen}<Help />{/if}
	{#if app.sessionSettings && app.data}
		{#key app.sessionSettings}<SessionSettings data={app.data} win={app.sessionSettings} />{/key}
	{/if}
{/if}

<Prompt />
<Toast />
