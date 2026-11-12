<script lang="ts">
	import { api } from '../api';
	import { app } from '../app.svelte';
	import Button from '../ui/Button.svelte';
	import Callout from '../ui/Callout.svelte';
	import Icon from '../ui/Icon.svelte';
	import IconButton from '../ui/IconButton.svelte';
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
	}

	let vaults = $state<VaultDoc[]>([]);
	let loadError = $state('');
	let cards = $state<Record<string, Card>>({});

	function card(ws: string): Card {
		cards[ws] ??= { password: '', confirm: '', createReadOnly: false, unlocked: false, keys: [], newKey: '', newValue: '', error: '', busy: false };
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

	async function call(ws: string, action: string, body: Record<string, unknown>) {
		const c = card(ws);
		c.busy = true;
		c.error = '';
		try {
			return await api<{ keys?: string[] }>('POST', `/api/vault/${encodeURIComponent(ws)}/${action}`, body);
		} catch (e) {
			c.error = (e as Error).message;
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
		const res = await call(ws, 'keys', { password: c.password });
		if (res) {
			c.keys = res.keys ?? [];
			c.unlocked = true;
		}
	}

	function lock(ws: string) {
		const c = card(ws);
		c.password = '';
		c.unlocked = false;
		c.keys = [];
		c.newValue = '';
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

	async function destroy(ws: string) {
		const ok = await app.ask(
			'Destroy this vault?',
			`Permanently deletes the encrypted vault of ${ws} and every secret in it. This cannot be undone.`,
			[{ label: 'Destroy vault', value: 'yes', tone: 'danger' }]
		);
		if (ok !== 'yes') return;
		if (await call(ws, 'destroy', { confirm: ws })) {
			lock(ws);
			app.notify(`Vault of ${ws} destroyed.`);
			await load();
		}
	}

	const validKey = (k: string) => /^[A-Za-z0-9_]{1,128}$/.test(k);
</script>

<PageHeader
	kind="Workspace"
	title="Vault"
	lead="One encrypted store per workspace (AES-256, Argon2id). With the vault on, .env files are hidden in sessions and agents ask for secrets through exitbox-vault, with an approval for each read. Values you store here are never shown again."
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
							<div class="flex min-h-10 items-center gap-2 border-b border-line-1 px-3 last:border-b-0">
								<span class="flex-1 truncate font-mono text-[12.5px]">{k}</span>
								<span class="font-mono text-xs text-ink-3">••••••••</span>
								<IconButton icon="pencil" label={'Replace the value of ' + k} small onclick={() => (c.newKey = k)} />
								<IconButton icon="trash" label={'Delete ' + k} small class="hover:text-warn" onclick={() => removeSecret(v.workspace, k)} />
							</div>
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
					<TextField label="Key" mono placeholder="API_TOKEN" hint="Letters, digits and underscores." wrap="flex-[1_1_200px]" bind:value={c.newKey} />
					<TextField label="Value" type="password" autocomplete="off" hint={c.keys.includes(c.newKey.trim()) ? 'Replaces the stored value.' : 'Write-only: it is never shown again.'} wrap="flex-[2_1_280px]" bind:value={c.newValue} />
					<Button type="submit" variant="primary" icon="check" disabled={c.busy || !validKey(c.newKey.trim()) || !c.newValue}>
						{c.keys.includes(c.newKey.trim()) ? 'Replace' : 'Store'}
					</Button>
				</form>
			{/if}

			<div class="flex justify-end">
				<Button size="sm" variant="ghost" class="text-warn" onclick={() => destroy(v.workspace)}>Destroy vault</Button>
			</div>
		{/if}
	</div>
{/each}
