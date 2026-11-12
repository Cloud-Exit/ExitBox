<script lang="ts">
	import { untrack } from 'svelte';
	import { api, local, setLocal, shellQuote } from './api';
	import type { EnvProfiles, FolderList, SessionInfo, StartRequest, State } from './api';
	import { app, defaultWindowSize } from './app.svelte';
	import Button from './ui/Button.svelte';
	import Callout from './ui/Callout.svelte';
	import Chip from './ui/Chip.svelte';
	import Icon from './ui/Icon.svelte';
	import IconButton from './ui/IconButton.svelte';
	import Modal from './ui/Modal.svelte';
	import SectionLabel from './ui/SectionLabel.svelte';
	import SelectField from './ui/SelectField.svelte';
	import TextField from './ui/TextField.svelte';
	import type { IconName } from './ui/icons';

	let { data }: { data: State } = $props();

	// The dialog starts from the data it opened with; later polls do not reset it.
	const start = (() => {
		const d = data;
		const last = local<{ dir: string; agent: string }>('last');
		const enabled = d.agents.filter((a) => a.enabled);
		return {
			dir: last?.dir ?? d.projects[0]?.path ?? d.home,
			agent: (enabled.find((a) => a.name === last?.agent) ?? enabled[0])?.name ?? '',
			defaults: d.defaults
		};
	})();

	let dir = $state(app.tilde(start.dir));
	let agent = $state(start.agent);
	let workspace = $state('');
	let name = $state('');
	let envProfile = $state('');
	let flags = $state({
		noFirewall: start.defaults.noFirewall,
		readOnly: start.defaults.readOnly,
		noEnv: start.defaults.noEnv,
		ollama: false,
		fresh: false
	});
	let memory = $state(start.defaults.memory);
	let cpus = $state(start.defaults.cpus);
	let domains = $state('');
	let browse = $state.raw<FolderList | null>(null);
	/** Dotfiles, and on Windows entries marked Hidden. */
	let showHidden = $state(local<boolean>('showHidden') ?? false);
	let error = $state('');
	let busy = $state(false);

	const defaults = start.defaults;

	const fullDir = $derived(dir.trim().replace(/^~(?=\/|$)/, data.home));

	const resolvedWs = $derived.by(() => {
		if (workspace) return workspace;
		const p = data.projects.find((x) => x.path === fullDir);
		if (p) return p.workspace;
		const scoped = data.workspaces.find((w) => w.directory && w.directory === fullDir);
		if (scoped) return scoped.name;
		return data.workspaces.find((w) => w.isDefault)?.name ?? '';
	});
	// Env profiles live in each workspace's KV store: read them once when the
	// dialog opens, not on every state poll.
	let envByWs = $state<Record<string, EnvProfiles>>({});
	for (const w of untrack(() => data.workspaces)) {
		api<EnvProfiles>('GET', '/api/env-profiles?workspace=' + encodeURIComponent(w.name))
			.then((r) => (envByWs[w.name] = r))
			.catch((e) => (envByWs[w.name] = { profiles: [], default: '', error: (e as Error).message }));
	}
	const env = $derived(envByWs[resolvedWs]);
	const profiles = $derived(env?.profiles ?? []);

	const flagDefs: { key: keyof typeof flags; label: string; icon?: IconName; locked: boolean }[] = [
		{ key: 'noFirewall', label: 'Firewall off', icon: 'shield', locked: defaults.noFirewall },
		{ key: 'readOnly', label: 'Read-only workspace', icon: 'lock', locked: defaults.readOnly },
		{ key: 'noEnv', label: 'No host environment', icon: 'eyeOff', locked: defaults.noEnv },
		{ key: 'ollama', label: 'Host Ollama', icon: 'bot', locked: false },
		{ key: 'fresh', label: 'Fresh session', icon: 'rotate', locked: false }
	];

	const request = $derived<StartRequest>({
		dir: dir.trim(),
		agent,
		workspace,
		name: name.trim(),
		envProfile: profiles.includes(envProfile) ? envProfile : '',
		// Flags already on by default need no option.
		noFirewall: flags.noFirewall && !defaults.noFirewall,
		readOnly: flags.readOnly && !defaults.readOnly,
		noEnv: flags.noEnv && !defaults.noEnv,
		ollama: flags.ollama,
		fresh: flags.fresh,
		memory: memory.trim() === defaults.memory ? '' : memory.trim(),
		cpus: cpus.trim() === defaults.cpus ? '' : cpus.trim(),
		allowUrls: domains.split(/[\s,]+/).filter(Boolean)
	});

	// Mirrors buildArgs in internal/web/api.go for the preview.
	const preview = $derived.by(() => {
		const r = request;
		const a = ['run', r.agent || '<agent>'];
		if (r.workspace) a.push('--workspace', r.workspace);
		if (r.noFirewall) a.push('--no-firewall');
		if (r.readOnly) a.push('--read-only');
		if (r.noEnv) a.push('--no-env');
		if (r.ollama) a.push('--ollama');
		if (r.name) a.push('--name', r.name);
		if (r.fresh) a.push('--no-resume');
		if (r.envProfile) a.push('--profile', r.envProfile);
		if (r.memory) a.push('--memory', r.memory);
		if (r.cpus) a.push('--cpus', r.cpus);
		for (const u of r.allowUrls) a.push('--allow-urls', u);
		return 'cd ' + shellQuote(dir.trim() || '~') + ' && exitbox ' + a.map(shellQuote).join(' ');
	});

	async function openFolder(path: string) {
		try {
			browse = await api<FolderList>('GET', '/api/fs?path=' + encodeURIComponent(path) + (showHidden ? '&hidden=1' : ''));
			dir = app.tilde(browse.path);
			error = '';
		} catch (e) {
			error = (e as Error).message;
		}
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		const size = defaultWindowSize();
		try {
			const info = await api<SessionInfo>('POST', '/api/sessions', {
				...request,
				cols: Math.max(40, Math.floor((size.w - 24) / 7.8)),
				rows: Math.max(10, Math.floor((size.h - 48) / 17))
			});
			setLocal('last', { dir: info.dir, agent: info.agent });
			app.newSessionOpen = false;
			app.openWindow(info);
			app.refresh();
		} catch (err) {
			error = (err as Error).message;
			busy = false;
		}
	}

	const close = () => (app.newSessionOpen = false);
</script>

<Modal label="New session" onclose={close} wide>
	<form onsubmit={submit} class="flex min-h-0 flex-col">
		<div class="flex items-center gap-3 border-b border-line-1 px-5 py-4">
			<div class="flex flex-1 flex-col gap-0.5">
				<span class="inline-flex items-center gap-1.5 font-mono text-[10.5px] tracking-[0.08em] text-ink-2 uppercase">
					<span class="size-1.5 rounded-full bg-brand"></span>Session
				</span>
				<h2 class="text-xl font-bold">New session</h2>
			</div>
			<IconButton icon="x" label="Close" onclick={close} />
		</div>

		<div class="flex min-h-0 flex-1 flex-col gap-[18px] overflow-auto px-5 py-[18px]">
			{#if error}<Callout tone="bad">{error}</Callout>{/if}

			<div class="flex flex-col gap-2">
				<SectionLabel>Project folder</SectionLabel>
				<div class="flex gap-2">
					<input
						data-autofocus
						bind:value={dir}
						aria-label="Project folder"
						spellcheck="false"
						class="block w-full min-w-0 rounded-md border border-line-1 bg-inset px-2.5 py-[7px] font-mono text-[12.5px] outline-none hover:border-line-2 focus:border-brand"
					/>
					{#if browse}
						<Button icon="back" onclick={() => (browse = null)}>Known projects</Button>
					{:else}
						<Button icon="folder" onclick={() => openFolder(dir)}>Browse</Button>
					{/if}
				</div>
				<div class="flex max-h-[220px] flex-col overflow-auto rounded-lg border border-line-1">
					{#if browse}
						{#if browse.parent}
							{@const parent = browse.parent}
							<button type="button" onclick={() => openFolder(parent)} class="flex min-h-10 shrink-0 items-center gap-2.5 border-b border-line-1 px-3 text-left hover:bg-surface-2">
								<Icon name="up" /><span class="flex-1 font-mono text-[12.5px]">..</span><span class="text-[11.5px] text-ink-2">up</span>
							</button>
						{/if}
						{#each browse.dirs as d (d)}
							{@const path = browse.path.replace(/\/$/, '') + '/' + d}
							<button type="button" onclick={() => openFolder(path)} class="flex min-h-10 shrink-0 items-center gap-2.5 border-b border-line-1 px-3 text-left last:border-b-0 hover:bg-surface-2">
								<Icon name="folder" /><span class="flex-1 truncate font-mono text-[12.5px]">{d}</span>
							</button>
						{:else}
							<div class="px-3 py-2.5 text-xs text-ink-3">No folders here</div>
						{/each}
					{:else}
						{#each data.projects as p (p.path)}
							<button
								type="button"
								onclick={() => (dir = app.tilde(p.path))}
								class={['flex min-h-10 shrink-0 items-center gap-2.5 border-b border-line-1 px-3 text-left last:border-b-0 hover:bg-surface-2', p.path === fullDir && 'bg-brand-soft']}
							>
								<Icon name="folder" />
								<span class="flex-1 truncate font-mono text-[12.5px]">{app.tilde(p.path)}</span>
								<span class="text-[11.5px] whitespace-nowrap text-ink-2">{p.scoped ? 'directory-scoped to ' + p.workspace : p.workspace}</span>
							</button>
						{:else}
							<div class="px-3 py-2.5 text-xs text-ink-3">No known projects yet. Type a path or browse.</div>
						{/each}
					{/if}
				</div>
				{#if browse}
					{@const current = browse.path}
					<label class="flex w-fit cursor-pointer items-center gap-2 text-xs text-ink-2">
						<input
							type="checkbox"
							bind:checked={showHidden}
							onchange={() => {
								setLocal('showHidden', showHidden);
								openFolder(current);
							}}
							class="size-3.5 accent-brand"
						/>
						Show hidden files
					</label>
				{/if}
				<small class="text-[11.5px] text-ink-2">
					Mounted at <span class="font-mono">/workspace</span>. {workspace ? 'Workspace forced to ' : 'Workspace resolves to '}{resolvedWs || 'none'}.
				</small>
			</div>

			<div class="flex flex-col gap-2">
				<SectionLabel>Agent</SectionLabel>
				<div class="flex flex-wrap gap-1.5">
					{#each data.agents as a (a.name)}
						<Chip
							pressed={agent === a.name}
							disabled={!a.enabled}
							title={a.enabled ? a.display : a.display + ' is not enabled. Enable it under Settings > Agents.'}
							onclick={() => (agent = a.name)}>{a.name}</Chip
						>
					{/each}
				</div>
			</div>

			<div class="grid grid-cols-3 gap-3 max-md:grid-cols-1">
				<SelectField
					label="Workspace"
					wrap=""
					bind:value={workspace}
					onchange={() => (envProfile = '')}
					options={[{ value: '', label: 'Auto (resolution order)' }, ...data.workspaces.map((w) => ({ value: w.name, label: w.name }))]}
				/>
				<TextField label="Session name" placeholder="Timestamp if empty" wrap="" bind:value={name} />
				<SelectField
					label="Env profile"
					wrap=""
					bind:value={envProfile}
					title={env?.error ?? ''}
					options={[
						{ value: '', label: env?.default ? `Workspace default (${env.default})` : 'Workspace default' },
						...profiles.map((p) => ({ value: p, label: p }))
					]}
				/>
			</div>

			<div class="flex flex-col gap-2">
				<SectionLabel>Run options</SectionLabel>
				<div class="flex flex-wrap gap-1.5">
					{#each flagDefs as f (f.key)}
						<Chip
							icon={f.icon}
							pressed={f.locked || flags[f.key]}
							disabled={f.locked}
							title={f.locked ? 'On by default (Settings > General)' : undefined}
							onclick={() => (flags[f.key] = !flags[f.key])}>{f.label}</Chip
						>
					{/each}
				</div>
				<div class="grid grid-cols-[1fr_1fr_2fr] gap-3 max-md:grid-cols-1">
					<TextField label="Memory" wrap="" bind:value={memory} />
					<TextField label="CPUs" wrap="" bind:value={cpus} />
					<TextField label="Extra allowed domains" placeholder="api.example.com, cdn.example.com" wrap="" bind:value={domains} />
				</div>
			</div>

			<div class="flex flex-col gap-2">
				<SectionLabel>Runs as</SectionLabel>
				<div class="rounded-lg border border-line-1 bg-inset px-3 py-2.5 font-mono text-xs break-all whitespace-pre-wrap">{preview}</div>
			</div>
		</div>

		<div class="flex flex-wrap items-center gap-2 border-t border-line-1 px-5 py-3.5">
			<Button type="submit" variant="primary" icon="play" disabled={busy || !agent || !dir.trim()}>Start session</Button>
			<Button variant="ghost" onclick={close}>Cancel</Button>
			<span class="ml-auto text-[11.5px] text-ink-2">The image builds on first run.</span>
		</div>
	</form>
</Modal>
