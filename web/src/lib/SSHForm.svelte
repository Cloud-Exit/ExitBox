<script module lang="ts">
	import type { SSHOptions } from './api';

	/** The SSH fields as typed; the port stays text until it is sent. */
	export interface SSHFormState {
		name: string;
		host: string;
		user: string;
		port: string;
		identity: string;
	}

	export function sshForm(o?: SSHOptions): SSHFormState {
		return { name: o?.name ?? '', host: o?.host ?? '', user: o?.user ?? '', port: o?.port ? String(o.port) : '', identity: o?.identity ?? '' };
	}

	export function portValid(port: string) {
		const p = port.trim();
		return !p || (/^\d{1,5}$/.test(p) && +p >= 1 && +p <= 65535);
	}

	/** The request the form sends (internal/web SSHOptions). */
	export function sshRequest(f: SSHFormState): SSHOptions {
		return { name: f.name.trim(), host: f.host.trim(), user: f.user.trim(), port: portValid(f.port) && f.port.trim() ? +f.port.trim() : 0, identity: f.identity.trim() };
	}
</script>

<script lang="ts">
	import { untrack } from 'svelte';
	import { api, sshArgs, sshCommand, sshTarget } from './api';
	import type { SSHHost, State } from './api';
	import { app } from './app.svelte';
	import type { Win } from './app.svelte';
	import { copyText } from './clipboard';
	import IconButton from './ui/IconButton.svelte';
	import Kbd from './ui/Kbd.svelte';
	import SectionLabel from './ui/SectionLabel.svelte';
	import SelectField from './ui/SelectField.svelte';
	import TextField from './ui/TextField.svelte';

	let { data, edit, form = $bindable(), folder = $bindable() }: { data: State; edit?: Win; form: SSHFormState; folder: string } = $props();

	// ~/.ssh/config aliases, read once when the dialog opens.
	let hosts = $state.raw<SSHHost[]>([]);
	api<{ hosts: SSHHost[] }>('GET', '/api/ssh-hosts')
		.then((r) => (hosts = r.hosts))
		.catch(() => (hosts = []));

	let open = $state(false);
	/** Index into matches; -1 is none. */
	let hi = $state(-1);
	let list = $state<HTMLDivElement>();

	const typed = $derived(form.host.trim().toLowerCase());
	const matches = $derived(
		hosts.filter((h) => !typed || h.alias.toLowerCase().includes(typed) || (h.hostname ?? '').toLowerCase().includes(typed)).slice(0, 50)
	);
	const known = $derived(hosts.find((h) => h.alias === form.host.trim()));
	const listShown = $derived(open && matches.length > 0 && !(matches.length === 1 && matches[0] === known));

	$effect(() => {
		if (hi >= 0) list?.querySelector('[data-hi]')?.scrollIntoView({ block: 'nearest' });
	});

	function pick(h: SSHHost) {
		form.host = h.alias;
		hi = -1;
		open = false;
	}

	function onkeydown(e: KeyboardEvent) {
		const n = listShown ? matches.length : 0;
		if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && hosts.length) {
			e.preventDefault();
			open = true;
			const m = matches.length;
			if (!m) return;
			const step = e.key === 'ArrowDown' ? 1 : -1;
			hi = hi < 0 ? (step > 0 ? 0 : m - 1) : (hi + step + m) % m;
		} else if ((e.key === 'Enter' || (e.key === 'Tab' && !e.shiftKey)) && n && hi >= 0) {
			// Picks the highlighted alias; Enter without one starts the session.
			e.preventDefault();
			pick(matches[hi]);
		} else if (e.key === 'Escape' && n) {
			// Closes the list, not the dialog.
			e.preventDefault();
			e.stopPropagation();
			open = false;
			hi = -1;
		}
	}

	const request = $derived(sshRequest(form));
	const portOk = $derived(portValid(form.port));
	// Until an option changes, the command shown is the one the session ran.
	const initial = untrack(() => JSON.stringify(sshRequest(form)));
	const changed = $derived(JSON.stringify(request) !== initial);
	const used = $derived(edit ? sshCommand(edit.info.args) : '');
	const preview = $derived(sshCommand(sshArgs(request)));
	const shown = $derived(edit && !changed ? used : preview);

	async function copy(text: string) {
		if (await copyText(text)) app.notify('Command copied');
		else app.notify('The browser did not allow copying', true);
	}
</script>

<TextField label="Session name" placeholder={request.host ? (request.user ? request.user + '@' : '') + request.host : 'user@host if empty'} wrap="" data-autofocus={edit ? undefined : true} bind:value={form.name} />

<div class="flex flex-col gap-1.5">
	<span class="text-[11.5px] font-semibold text-ink-2" id="ssh-host-label">Host</span>
	<div class="relative">
		<input
			data-autofocus={edit ? true : undefined}
			bind:value={form.host}
			oninput={() => {
				open = true;
				hi = -1;
			}}
			onfocus={() => (open = true)}
			onblur={() => (open = false)}
			{onkeydown}
			aria-labelledby="ssh-host-label"
			aria-autocomplete="list"
			aria-expanded={listShown}
			role="combobox"
			aria-controls="ssh-host-list"
			placeholder="host, IP or ~/.ssh/config alias"
			autocomplete="off"
			spellcheck="false"
			class="block w-full min-w-0 rounded-md border border-line-1 bg-inset px-2.5 py-[7px] font-mono text-[12.5px] outline-none placeholder:font-sans placeholder:text-ink-3 hover:border-line-2 focus:border-brand"
		/>
		{#if listShown}
			<div
				bind:this={list}
				id="ssh-host-list"
				role="listbox"
				aria-label="Hosts in ~/.ssh/config"
				class="absolute inset-x-0 top-full z-10 mt-1 flex max-h-[200px] flex-col overflow-auto rounded-lg border border-line-2 bg-surface-1 shadow-float"
			>
				{#each matches as h, i (h.alias)}
					<button
						type="button"
						role="option"
						aria-selected={i === hi}
						data-hi={i === hi || undefined}
						tabindex="-1"
						onmousedown={(e) => e.preventDefault()}
						onclick={() => pick(h)}
						class={['flex min-h-9 shrink-0 items-center gap-2.5 border-b border-line-1 px-3 text-left last:border-b-0 hover:bg-surface-2', i === hi && 'bg-surface-2']}
					>
						<span class="flex-1 truncate font-mono text-[12.5px]">{h.alias}</span>
						<span class="truncate font-mono text-[11px] text-ink-2">{sshTarget(h)}</span>
						{#if i === hi}<Kbd>Enter</Kbd>{/if}
					</button>
				{/each}
			</div>
		{/if}
	</div>
	<small class="text-[11.5px] text-ink-2">
		{#if known}
			<span class="font-mono">{sshTarget(known) || known.alias}</span> from ~/.ssh/config
		{:else}
			{hosts.length ? `${hosts.length} ${hosts.length === 1 ? 'alias' : 'aliases'} in ~/.ssh/config` : 'Uses your ~/.ssh/config, agent and known_hosts.'}
		{/if}
	</small>
</div>

<div class="grid grid-cols-[2fr_1fr_3fr] gap-3 max-md:grid-cols-1">
	<TextField label="User" mono wrap="" placeholder={known?.user ?? 'from ssh config'} bind:value={form.user} />
	<TextField
		label="Port"
		mono
		wrap=""
		inputmode="numeric"
		placeholder={String(known?.port ?? 22)}
		hint={portOk ? '' : '1 to 65535'}
		aria-invalid={!portOk}
		bind:value={form.port}
	/>
	<TextField label="Identity file" mono wrap="" placeholder="optional, e.g. ~/.ssh/id_ed25519" title="A private key file on this machine. It is passed to ssh, never read here." bind:value={form.identity} />
</div>

<SelectField label="Sidebar folder" wrap="" bind:value={folder} options={[{ value: '', label: 'None' }, ...(data.folders ?? []).map((f) => ({ value: f.id, label: f.name }))]} />

<div class="flex flex-col gap-2">
	<div class="flex items-center gap-2">
		<SectionLabel>{edit && changed ? 'Reconnects as' : 'Runs as'}</SectionLabel>
		<IconButton icon="copy" label="Copy command" small onclick={() => copy(shown)} />
	</div>
	<div class="rounded-lg border border-line-1 bg-inset px-3 py-2.5 font-mono text-xs break-all whitespace-pre-wrap">{shown}</div>
	{#if edit && changed}
		<small class="font-mono text-[11px] break-all text-ink-3" title="The command the session ran">Last run: {used}</small>
	{:else}
		<small class="text-[11.5px] text-ink-2">Passwords and host keys are answered in the terminal; exitbox never stores them.</small>
	{/if}
</div>
