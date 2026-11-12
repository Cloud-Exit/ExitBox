<script lang="ts">
	import { app } from '$lib/app.svelte';
	import Desktop from '$lib/Desktop.svelte';
	import Login from '$lib/Login.svelte';
	import NewSession from '$lib/NewSession.svelte';
	import NoLoginBar from '$lib/NoLoginBar.svelte';
	import Prompt from '$lib/Prompt.svelte';
	import SessionSettings from '$lib/SessionSettings.svelte';
	import Settings from '$lib/settings/Settings.svelte';
	import Sidebar from '$lib/Sidebar.svelte';
	import StatusBar from '$lib/StatusBar.svelte';
	import Toast from '$lib/Toast.svelte';
	import Button from '$lib/ui/Button.svelte';
	import Callout from '$lib/ui/Callout.svelte';

	app.boot();
</script>

<svelte:window
	onstorage={(e) => {
		if (e.key === 'exitbox.theme') app.applyTheme();
	}}
/>

{#if app.view === 'login'}
	<Login />
{:else if app.view === 'offline'}
	<div class="grid min-h-screen place-items-center p-4">
		<div class="flex flex-col items-center gap-3">
			<Callout tone="bad">Cannot reach exitbox web: {app.offlineReason}. Retrying every few seconds.</Callout>
			<Button icon="rotate" onclick={() => app.retry()}>Retry now</Button>
		</div>
	</div>
{:else if app.view === 'sessions' || app.view === 'settings'}
	<div class="flex h-screen flex-col overflow-hidden max-md:h-auto max-md:min-h-screen max-md:overflow-visible">
		<!-- Takes layout space, so the desktop measures and tiles below it. -->
		{#if app.me && !app.me.authEnabled}<NoLoginBar />{/if}
		<div class="flex min-h-0 flex-1 max-md:flex-col">
			<Sidebar />
			<main class="flex min-w-0 flex-1 flex-col">
				<Desktop />
				<Settings />
				<StatusBar />
			</main>
		</div>
	</div>
	{#if app.newSessionOpen && app.data}
		<NewSession data={app.data} />
	{/if}
	{#if app.sessionSettings && app.data}
		{#key app.sessionSettings}<SessionSettings data={app.data} win={app.sessionSettings} />{/key}
	{/if}
{/if}

<Prompt />
<Toast />
