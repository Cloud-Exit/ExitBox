<script lang="ts">
	import { untrack } from 'svelte';
	import { api } from '../api';
	import { app } from '../app.svelte';
	import Button from '../ui/Button.svelte';
	import Callout from '../ui/Callout.svelte';
	import Icon from '../ui/Icon.svelte';
	import IconButton from '../ui/IconButton.svelte';
	import Modal from '../ui/Modal.svelte';
	import SectionLabel from '../ui/SectionLabel.svelte';
	import TextField from '../ui/TextField.svelte';
	import Toggle from '../ui/Toggle.svelte';
	import PageHeader from './PageHeader.svelte';

	interface VaultDoc {
		workspace: string;
		initialized: boolean;
		enabled: boolean;
		readOnly: boolean;
	}

	/** Per-workspace page state. The password lives here only, never in storage. */
	interface Card {
		password: string;
		confirm: string;
		createReadOnly: boolean;
		unlocked: boolean;
		keys: string[];
		newKey: string;
		newValue: string;
		error: string;
		busy: boolean;
		/** Bumped by lock: secret answers to calls made before it are dropped. */
		gen: number;
		/** The secret being edited in its row, its value, and whether it shows. */
		editKey: string;
		editValue: string;
		editShown: boolean;
	}

	let vaults = $state<VaultDoc[]>([]);
	let loadError = $state('');
	let cards = $state<Record<string, Card>>({});

	function card(ws: string): Card {
		cards[ws] ??= {
			password: '',
			confirm: '',
			createReadOnly: false,
			unlocked: false,
			keys: [],
			newKey: '',
			newValue: '',
			error: '',
			busy: false,
			gen: 0,
			editKey: '',
			editValue: '',
			editShown: false
		};
		return cards[ws];
	}

	async function load() {
		try {
			const list = await api<VaultDoc[]>('GET', '/api/vault');
			// Cards exist before the template reads them, so rendering never writes state.
			for (const v of list) card(v.workspace);
			vaults = list;
			loadError = '';
		} catch (e) {
			loadError = (e as Error).message;
		}
	}
	load();

	/** With secret set, an answer that shows secrets or unlocks the card is dropped once the card was locked since. */
	async function call(ws: string, action: string, body: Record<string, unknown>, secret = false) {
		const c = card(ws);
		const gen = c.gen;
		const stale = () => secret && c.gen !== gen;
		c.busy = true;
		c.error = '';
		try {
			const res = await api<{ keys?: string[]; value?: string }>('POST', `/api/vault/${encodeURIComponent(ws)}/${action}`, body);
			return stale() ? null : res;
		} catch (e) {
			if (!stale()) c.error = (e as Error).message;
			return null;
		} finally {
			c.busy = false;
		}
	}

	async function create(ws: string) {
		const c = card(ws);
		if (c.password !== c.confirm) {
			c.error = 'The passwords do not match.';
			return;
		}
		if (await call(ws, 'create', { password: c.password, readOnly: c.createReadOnly })) {
			app.notify(`Vault created for ${ws}.`);
			await load();
			await unlock(ws);
			c.confirm = '';
		}
	}

	async function unlock(ws: string) {
		const c = card(ws);
		const res = await call(ws, 'keys', { password: c.password }, true);
		if (res) {
			c.keys = res.keys ?? [];
			c.unlocked = true;
		}
	}

	function lock(ws: string) {
		const c = card(ws);
		c.gen++;
		c.password = '';
		c.confirm = '';
		c.unlocked = false;
		c.keys = [];
		c.newValue = '';
		cancelEdit(ws);
	}

	// Settings stays mounted behind the sessions view: leaving it drops every held password.
	$effect(() => {
		if (app.view === 'settings') return;
		untrack(() => {
			for (const ws of Object.keys(cards)) lock(ws);
		});
	});

	/** Opens the row's editor with the current value, hidden. */
	async function startEdit(ws: string, key: string) {
		const c = card(ws);
		const res = await call(ws, 'reveal', { password: c.password, key }, true);
		if (!res) return;
		c.editKey = key;
		c.editValue = res.value ?? '';
		c.editShown = false;
	}

	function cancelEdit(ws: string) {
		const c = card(ws);
		c.editKey = '';
		c.editValue = '';
		c.editShown = false;
	}

	async function saveEdit(ws: string) {
		const c = card(ws);
		if (await call(ws, 'set', { password: c.password, key: c.editKey, value: c.editValue })) {
			app.notify(`Updated ${c.editKey}.`);
			cancelEdit(ws);
		}
	}

	function focusInput(node: HTMLInputElement) {
		node.focus();
	}

	async function setSecret(ws: string) {
		const c = card(ws);
		const key = c.newKey.trim();
		const replaces = c.keys.includes(key);
		if (await call(ws, 'set', { password: c.password, key, value: c.newValue })) {
			app.notify(replaces ? `Updated ${key}.` : `Stored ${key}.`);
			c.newKey = '';
			c.newValue = '';
			await unlock(ws);
		}
	}

	async function removeSecret(ws: string, key: string) {
		const ok = await app.ask('Delete secret?', `Deletes ${key} from the ${ws} vault. Sessions that read it will no longer find it.`, [
			{ label: 'Delete', value: 'yes', tone: 'danger' }
		]);
		if (ok !== 'yes') return;
		if (await call(ws, 'delete', { password: card(ws).password, key })) await unlock(ws);
	}

	async function setConfig(v: VaultDoc, enabled: boolean, readOnly: boolean) {
		if (await call(v.workspace, 'config', { enabled, readOnly })) await load();
	}

	/** The destroy dialog: the workspace name must be typed, and that typed text is what the server checks. */
	let destroying = $state<{ ws: string; typed: string; error: string; busy: boolean } | null>(null);

	function destroy(ws: string) {
		destroying = { ws, typed: '', error: '', busy: false };
	}

	async function confirmDestroy() {
		const d = destroying;
		if (!d || d.busy || d.typed !== d.ws) return;
		const c = card(d.ws);
		d.busy = true;
		c.busy = true;
		d.error = '';
		try {
			await api('POST', `/api/vault/${encodeURIComponent(d.ws)}/destroy`, { confirm: d.typed });
			lock(d.ws);
			destroying = null;
			app.notify(`Vault of ${d.ws} destroyed.`);
			await load();
		} catch (e) {
			// A 409 means a session of this workspace runs.
			d.error = (e as Error).message;
		} finally {
			d.busy = false;
			c.busy = false;
		}
	}

	const validKey = (k: string) => /^[A-Za-z0-9_]{1,128}$/.test(k);
</script>

<PageHeader
	kind="Workspace"
	title="Vault"
	lead="One encrypted store per workspace (AES-256, Argon2id). With the vault on, .env files are hidden in sessions and agents ask for secrets through exitbox-vault, with an approval for each read. Values stay hidden: edit one to see it, behind the eye."
	source="exitbox vault · workspaces.items[].vault"
/>
{#if loadError}<Callout tone="bad">{loadError}</Callout>{/if}

{#each vaults as v (v.workspace)}
	{@const c = card(v.workspace)}
	<div class="flex flex-col gap-3 rounded-xl border border-line-1 bg-surface-1 p-3.5">
		<h3 class="flex items-center gap-2 text-[15px] font-bold">
			{v.workspace}
			<span class="rounded border border-line-2 bg-surface-2 px-1.5 font-mono text-[9.5px] font-bold text-ink-2">
				{!v.initialized ? 'no vault' : !v.enabled ? 'off' : v.readOnly ? 'read-only' : 'read and write'}
			</span>
			{#if c.unlocked}<span class="inline-flex items-center gap-1 text-xs font-medium text-accent-fg"><Icon name="lock" size={12} />unlocked</span>{/if}
		</h3>
		{#if c.error}<Callout tone="bad">{c.error}</Callout>{/if}

		{#if !v.initialized}
			<span class="text-xs text-ink-2">Create a vault to keep this workspace's secrets encrypted and out of .env files.</span>
			<div class="flex flex-wrap gap-2.5">
				<TextField label="Vault password" type="password" autocomplete="new-password" hint="At least 8 characters. It cannot be recovered." bind:value={c.password} />
				<TextField label="Confirm password" type="password" autocomplete="new-password" bind:value={c.confirm} />
			</div>
			<div class="flex flex-wrap gap-2.5">
				<Toggle label="Read-only" description="Agents can read secrets but not store new ones" bind:checked={c.createReadOnly} />
			</div>
			<div><Button variant="primary" icon="lock" disabled={c.busy || c.password.length < 8} onclick={() => create(v.workspace)}>Create vault</Button></div>
		{:else}
			<div class="flex flex-wrap gap-2.5">
				<Toggle label="Use the vault in sessions" description="Hide .env files and serve secrets through exitbox-vault" checked={v.enabled} onchange={(on) => setConfig(v, on, v.readOnly)} />
				<Toggle label="Read-only" description="Agents can read secrets but not store new ones" checked={v.readOnly} onchange={(ro) => setConfig(v, v.enabled, ro)} />
			</div>

			{#if !c.unlocked}
				<form
					class="flex flex-wrap items-end gap-2"
					onsubmit={(e) => {
						e.preventDefault();
						unlock(v.workspace);
					}}
				>
					<TextField label="Vault password" type="password" autocomplete="current-password" wrap="flex-[0_1_320px]" bind:value={c.password} />
					<Button type="submit" icon="lock" disabled={c.busy || !c.password}>Unlock to manage secrets</Button>
				</form>
			{:else}
				<div class="flex items-center gap-2">
					<SectionLabel>Secrets</SectionLabel>
					<span class="font-mono text-[10.5px] text-ink-3">{c.keys.length}</span>
					<span class="flex-1"></span>
					<Button size="sm" variant="ghost" icon="lock" onclick={() => lock(v.workspace)}>Lock</Button>
				</div>
				{#if c.keys.length}
					<div class="flex flex-col overflow-hidden rounded-lg border border-line-1">
						{#each c.keys as k (k)}
							{#if c.editKey === k}
								<form
									class="flex flex-wrap items-center gap-2 border-b border-line-1 bg-surface-2 px-3 py-2 last:border-b-0"
									onsubmit={(e) => {
										e.preventDefault();
										saveEdit(v.workspace);
									}}
								>
									<span class="w-full truncate font-mono text-[12.5px] font-semibold">{k}</span>
									<div class="flex min-w-0 flex-[1_1_320px] items-center rounded-md border border-brand bg-surface-0">
										<input
											type={c.editShown ? 'text' : 'password'}
											bind:value={c.editValue}
											{@attach focusInput}
											aria-label={'Value of ' + k}
											autocomplete="off"
											spellcheck="false"
											onkeydown={(e) => {
												if (e.key === 'Escape') {
													e.preventDefault();
													cancelEdit(v.workspace);
												}
											}}
											class="block min-w-0 flex-1 bg-transparent px-2.5 py-[7px] font-mono text-[12.5px] outline-none max-md:h-[44px] max-md:text-[16px]"
										/>
										<IconButton
											icon={c.editShown ? 'eyeOff' : 'eye'}
											label={c.editShown ? 'Hide the value' : 'Show the value'}
											small
											class="mr-1"
											onclick={() => (c.editShown = !c.editShown)}
										/>
									</div>
									<Button type="submit" size="sm" variant="primary" icon="check" disabled={c.busy || !c.editValue}>Save</Button>
									<Button size="sm" variant="ghost" onclick={() => cancelEdit(v.workspace)}>Cancel</Button>
								</form>
							{:else}
								<div class="flex min-h-10 items-center gap-2 border-b border-line-1 px-3 last:border-b-0">
									<span class="flex-1 truncate font-mono text-[12.5px]">{k}</span>
									<span class="font-mono text-xs text-ink-3">••••••••</span>
									<IconButton icon="pencil" label={'Edit ' + k} small disabled={c.busy} onclick={() => startEdit(v.workspace, k)} />
									<IconButton icon="trash" label={'Delete ' + k} small class="hover:text-warn" disabled={c.busy} onclick={() => removeSecret(v.workspace, k)} />
								</div>
							{/if}
						{/each}
					</div>
				{:else}
					<span class="text-xs text-ink-3">No secrets yet.</span>
				{/if}
				<form
					class="flex flex-wrap items-end gap-2.5"
					onsubmit={(e) => {
						e.preventDefault();
						setSecret(v.workspace);
					}}
				>
					<TextField label="New secret" mono placeholder="API_TOKEN" hint="Letters, digits and underscores." wrap="flex-[1_1_200px]" bind:value={c.newKey} />
					<TextField label="Value" type="password" autocomplete="off" hint={c.keys.includes(c.newKey.trim()) ? 'Replaces the stored value.' : 'Hidden as you type.'} wrap="flex-[2_1_280px]" bind:value={c.newValue} />
					<Button type="submit" variant="primary" icon="check" disabled={c.busy || !validKey(c.newKey.trim()) || !c.newValue}>
						{c.keys.includes(c.newKey.trim()) ? 'Replace' : 'Store'}
					</Button>
				</form>
			{/if}

			<div class="flex flex-wrap items-center justify-end gap-2">
				<Button size="sm" variant="ghost" class="text-warn" disabled={c.busy} onclick={() => destroy(v.workspace)}>Destroy vault</Button>
			</div>
		{/if}
	</div>
{/each}

{#if destroying}
	{@const d = destroying}
	<Modal label="Destroy this vault?" onclose={() => !d.busy && (destroying = null)}>
		<form
			class="flex min-h-0 flex-1 flex-col"
			onsubmit={(e) => {
				e.preventDefault();
				confirmDestroy();
			}}
		>
			<div class="border-b border-line-1 px-5 py-4 max-md:px-[16px]"><h2 class="text-base font-bold">Destroy this vault?</h2></div>
			<div class="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-5 py-[18px] text-[13px] text-ink-2 max-md:px-[16px] max-md:text-[14px]">
				<span>Permanently deletes the encrypted vault of <span class="font-semibold text-ink-1">{d.ws}</span> and every secret in it. This cannot be undone.</span>
				<TextField label={`Type ${d.ws} to confirm`} mono autocapitalize="off" wrap="flex-none" disabled={d.busy} bind:value={d.typed} />
				{#if d.error}<Callout tone="bad">{d.error}</Callout>{/if}
			</div>
			<div class="flex flex-wrap items-center gap-2 border-t border-line-1 px-5 py-3.5 max-md:flex-col max-md:items-stretch max-md:px-[16px]">
				<Button type="submit" variant="danger" class="max-md:w-full max-md:justify-center" disabled={d.busy || d.typed !== d.ws}>Destroy vault</Button>
				<!-- Cancel takes the focus: a stray Enter must never destroy. -->
				<Button variant="ghost" data-autofocus class="max-md:w-full max-md:justify-center max-md:border-line-2" disabled={d.busy} onclick={() => (destroying = null)}>Cancel</Button>
			</div>
		</form>
	</Modal>
{/if}
