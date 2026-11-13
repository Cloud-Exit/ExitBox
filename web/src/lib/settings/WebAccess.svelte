<script lang="ts">
	import { untrack } from 'svelte';
	import { api } from '../api';
	import { app } from '../app.svelte';
	import Button from '../ui/Button.svelte';
	import Callout from '../ui/Callout.svelte';
	import Icon from '../ui/Icon.svelte';
	import SectionLabel from '../ui/SectionLabel.svelte';
	import TextField from '../ui/TextField.svelte';
	import PageHeader from './PageHeader.svelte';

	let { web }: { web: { listen: string; listenActive: string; username: string; authEnabled: boolean; recentDays: number } } = $props();

	// Edits start from the saved values when the page opens.
	const initial = untrack(() => ({ listen: web.listen, username: web.username, recentDays: String(web.recentDays) }));
	let listen = $state(initial.listen);
	let username = $state(initial.username);
	let recentDays = $state(initial.recentDays);
	let password = $state('');
	let confirm = $state('');
	let error = $state('');
	let saving = $state(false);

	async function save(disable: boolean) {
		if (saving) return;
		error = '';
		if (!disable && password !== confirm) {
			error = 'The passwords do not match.';
			return;
		}
		const days = recentDays.trim();
		if (!disable && !/^-?\d+$/.test(days)) {
			error = 'Recently ended days must be a whole number.';
			return;
		}
		saving = true;
		try {
			const res = await api<{ authEnabled: boolean; signedOut: boolean; restartNeeded: boolean; listen: string }>('PUT', '/api/settings/web', {
				listen,
				username,
				// Turning the login off needs no password; never send one that is not used.
				password: disable ? '' : password,
				disable,
				// Turning off the login leaves the days as saved.
				recentDays: disable ? undefined : Number(days)
			});
			// Saved: the fields must not hold the password any longer.
			password = '';
			confirm = '';
			app.notify(res.restartNeeded ? `Saved. Restart exitbox web to listen on ${res.listen}.` : 'Saved.');
			if (res.signedOut) {
				app.showLogin('Login settings changed. Sign in again.');
				return;
			}
			await Promise.all([app.refresh(), app.loadSettings(), app.loadMe()]);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function turnOff() {
		const ok = await app.ask('Turn off the login?', `Anyone who can reach ${web.listenActive} can then run agents on this machine.`, [
			{ label: 'Turn off', value: 'yes', tone: 'danger' }
		]);
		if (ok === 'yes') save(true);
	}

	// The "Enable login" bar sends the user here to type a username.
	function focusIfAsked(node: HTMLInputElement) {
		if (!app.focusWebLogin) return;
		app.focusWebLogin = false;
		node.focus();
	}
</script>

<PageHeader
	kind="exitbox web"
	title="Web access"
	lead="Sessions mount host folders, so anyone who can open this page can reach your files. Keep it on 127.0.0.1, or set a login before listening beyond this machine."
	source="exitbox setup · Web access · config.yaml web"
/>
<Callout>
	<Icon name="shield" />
	<span>
		{web.authEnabled ? `Login is on for ${web.username}.` : 'No login. Only this machine can connect.'}
		Listening on <span class="font-mono">{web.listenActive}</span>.
	</span>
</Callout>
<div class="flex flex-wrap gap-2.5">
	<TextField label="Listen address" mono placeholder="127.0.0.1:4848" hint="Takes effect when exitbox web restarts." bind:value={listen} />
	<TextField label="Username" autocomplete="username" bind:value={username} {@attach focusIfAsked} />
</div>
<div class="flex flex-wrap gap-2.5">
	<TextField
		label="New password"
		type="password"
		autocomplete="new-password"
		hint={web.authEnabled ? 'Empty keeps the current password.' : 'At least 8 characters.'}
		bind:value={password}
	/>
	<TextField label="Confirm password" type="password" autocomplete="new-password" bind:value={confirm} />
</div>
<div class="flex flex-wrap gap-2.5">
	<TextField
		label="Recently ended, days"
		inputmode="numeric"
		wrap="flex-[0_1_260px]"
		hint="Ended sessions stay red in the sidebar this long."
		bind:value={recentDays}
	/>
</div>
<!-- By the buttons: on a phone the top of the page is off screen when Save is pressed. -->
{#if error}<Callout tone="bad">{error}</Callout>{/if}
<div class="flex flex-wrap gap-2">
	<Button variant="primary" icon="lock" disabled={saving} onclick={() => save(false)}>{web.authEnabled ? 'Save' : username.trim() || password ? 'Turn on login' : 'Save'}</Button>
	{#if web.authEnabled}<Button variant="ghost" disabled={saving} onclick={turnOff}>Turn off login</Button>{/if}
</div>
<SectionLabel>Run as a service</SectionLabel>
<div class="rounded-lg border border-line-1 bg-inset px-3 py-2.5 font-mono text-xs">exitbox web --service</div>
<span class="text-xs text-ink-2">
	Installs a systemd user unit on Linux or a launchd agent on macOS that starts exitbox web at login. Remove it with
	<span class="font-mono">exitbox web --remove-service</span>.
</span>
