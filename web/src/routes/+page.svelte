<script lang="ts">
	import { app } from '$lib/app.svelte';
	import Desktop from '$lib/Desktop.svelte';
	import Login from '$lib/Login.svelte';
	import NewSession from '$lib/NewSession.svelte';
	import Prompt from '$lib/Prompt.svelte';
	import Settings from '$lib/settings/Settings.svelte';
	import Sidebar from '$lib/Sidebar.svelte';
	import StatusBar from '$lib/StatusBar.svelte';
	import Toast from '$lib/Toast.svelte';
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
		<Callout tone="bad">Cannot reach exitbox web: {app.offlineReason}</Callout>
	</div>
{:else if app.view === 'sessions' || app.view === 'settings'}
	<div class="flex h-screen overflow-hidden max-md:h-auto max-md:min-h-screen max-md:flex-col max-md:overflow-visible">
		<Sidebar />
		<main class="flex min-w-0 flex-1 flex-col">
			<Desktop />
			<Settings />
			<StatusBar />
		</main>
	</div>
	{#if app.newSessionOpen && app.data}
		<NewSession data={app.data} />
	{/if}
{/if}

<Prompt />
<Toast />
