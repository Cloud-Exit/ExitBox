<script lang="ts">
	import { app } from './app.svelte';
	import Brand from './ui/Brand.svelte';
	import Button from './ui/Button.svelte';
	import Callout from './ui/Callout.svelte';
	import TextField from './ui/TextField.svelte';

	let username = $state('');
	let password = $state('');
	let error = $state('');
	let busy = $state(false);

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			await app.login(username, password);
		} catch (err) {
			error = (err as Error).message;
			busy = false;
		}
	}
</script>

<div class="grid min-h-screen place-items-center p-4">
	<form onsubmit={submit} class="flex w-full max-w-[380px] flex-col gap-3.5 rounded-xl border border-line-1 bg-surface-1 p-6 shadow-float">
		<Brand />
		<span class="text-[13px] text-ink-2">{app.loginMessage || 'Sign in to manage your agent sessions.'}</span>
		{#if error}<Callout tone="bad">{error}</Callout>{/if}
		<TextField label="Username" name="username" autocomplete="username" required bind:value={username} wrap="" />
		<TextField label="Password" name="password" type="password" autocomplete="current-password" required bind:value={password} wrap="" />
		<Button type="submit" variant="primary" disabled={busy} class="self-start">Sign in</Button>
	</form>
</div>
