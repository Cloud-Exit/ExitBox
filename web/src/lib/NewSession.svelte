<script lang="ts">
	import { untrack } from 'svelte';
	import { SvelteMap } from 'svelte/reactivity';
	import { api, local, runCommand, setLocal, shellQuote } from './api';
	import type { EnvProfiles, FolderList, ProjectDoc, RunOptions, SessionInfo, StartRequest, State } from './api';
	import { app, defaultWindowSize, windowKey } from './app.svelte';
	import type { Win } from './app.svelte';
	import { copyText } from './clipboard';
	import Button from './ui/Button.svelte';
	import Callout from './ui/Callout.svelte';
	import AgentIcon from './AgentIcon.svelte';
	import Chip from './ui/Chip.svelte';
	import Icon from './ui/Icon.svelte';
	import IconButton from './ui/IconButton.svelte';
	import Kbd from './ui/Kbd.svelte';
	import Modal from './ui/Modal.svelte';
	import SectionLabel from './ui/SectionLabel.svelte';
	import SelectField from './ui/SelectField.svelte';
	import TextField from './ui/TextField.svelte';
	import type { IconName } from './ui/icons';

	/** With edit, the dialog changes that window's session and restarts it. */
	let { data, edit }: { data: State; edit?: Win } = $props();

	// The dialog starts from the data it opened with; later polls do not reset it.
	const start = (() => {
		const d = data;
		const req: Partial<RunOptions> = untrack(() => edit?.info.request) ?? {};
		const last = edit ? null : local<{ dir: string; agent: string; folder?: string }>('last');
		const enabled = d.agents.filter((a) => a.enabled);
		const key = edit && windowKey(untrack(() => edit.info));
		return {
			dir: req.dir ?? last?.dir ?? d.projects[0]?.path ?? d.home,
			agent: req.agent ?? (enabled.find((a) => a.name === last?.agent) ?? enabled[0])?.name ?? '',
			folder: key ? (d.folders?.find((f) => f.items.includes(key))?.id ?? '') : d.folders?.some((f) => f.id === last?.folder) ? (last?.folder ?? '') : '',
			req,
			defaults: d.defaults
		};
	})();
	const req = start.req;

	let dir = $state(app.tilde(start.dir));
	let agent = $state(start.agent);
	let workspace = $state(req.workspace ?? '');
	let folder = $state(start.folder);
	let name = $state(req.name ?? '');
	let envProfile = $state(req.envProfile ?? '');
	let flags = $state({
		noFirewall: !!req.noFirewall || start.defaults.noFirewall,
		readOnly: !!req.readOnly || start.defaults.readOnly,
		noEnv: !!req.noEnv || start.defaults.noEnv,
		ollama: !!req.ollama,
		fresh: !!req.fresh
	});
	let memory = $state(req.memory || start.defaults.memory);
	let cpus = $state(req.cpus || start.defaults.cpus);
	let domains = $state(req.allowUrls?.join(', ') ?? '');
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
		// Kept until the workspace's profiles have loaded.
		envProfile: !env || profiles.includes(envProfile) ? envProfile : '',
		// Flags already on by default need no option.
		noFirewall: flags.noFirewall && !defaults.noFirewall,
		readOnly: flags.readOnly && !defaults.readOnly,
		noEnv: flags.noEnv && !defaults.noEnv,
		ollama: flags.ollama,
		fresh: flags.fresh,
		memory: memory.trim() === defaults.memory ? '' : memory.trim(),
		cpus: cpus.trim() === defaults.cpus ? '' : cpus.trim(),
		allowUrls: domains.split(/[\s,]+/).filter(Boolean),
		// Not in the form: a stored request keeps it.
		...(req.resume ? { resume: true } : {})
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
		else if (r.resume && !r.name) a.push('--resume');
		if (r.envProfile) a.push('--profile', r.envProfile);
		if (r.memory) a.push('--memory', r.memory);
		if (r.cpus) a.push('--cpus', r.cpus);
		for (const u of r.allowUrls) a.push('--allow-urls', u);
		if (edit) return runCommand(fullDir, a);
		return 'cd ' + shellQuote(dir.trim() || '~') + ' && exitbox ' + a.map(shellQuote).join(' ');
	});

	// Until an option changes, the command shown is the one the session ran.
	const initial = untrack(() => JSON.stringify(request));
	const changed = $derived(JSON.stringify(request) !== initial);
	const used = $derived(edit ? runCommand(edit.info.dir, edit.info.args) : '');
	const shown = $derived(edit && !changed ? used : preview);

	async function copy(text: string) {
		if (await copyText(text)) app.notify('Command copied');
		else app.notify('The browser did not allow copying', true);
	}

	// ---------- project folder search and completion ----------

	type Suggestion = { kind: 'project'; path: string; project: ProjectDoc } | { kind: 'dir'; path: string; head: string; tail: string };

	let input = $state<HTMLInputElement>();
	let list = $state<HTMLDivElement>();
	/** Index into suggestions; -1 is none. */
	let hi = $state(-1);
	/** Escape hides the folder completions until the next edit. */
	let dismissed = $state(false);

	const typed = $derived(dir.trim());
	const expand = (p: string) => p.replace(/^~(?=\/|$)/, data.home);

	// Exact, then path prefix, then name prefix, then substring, then workspace; ties keep the list order.
	function rank(p: ProjectDoc, q: string) {
		const t = app.tilde(p.path).toLowerCase();
		const a = p.path.toLowerCase();
		if (t === q || a === q || t + '/' === q || a + '/' === q) return 0;
		if (t.startsWith(q) || a.startsWith(q)) return 1;
		if (t.slice(t.lastIndexOf('/') + 1).startsWith(q)) return 2;
		if (t.includes(q) || a.includes(q)) return 3;
		if (p.workspace.toLowerCase().includes(q)) return 4;
		return -1;
	}

	// One row per path: two project entries can share a path.
	const allProjects = $derived([...new Map(data.projects.map((p) => [p.path, p])).values()]);

	const projects = $derived.by(() => {
		const q = typed.toLowerCase();
		if (!q) return allProjects;
		return allProjects
			.map((p, i) => ({ p, i, r: rank(p, q) }))
			.filter((x) => x.r >= 0)
			.sort((x, y) => x.r - y.r || x.i - y.i)
			.map((x) => x.p);
	});

	/** Folder listings by hidden flag and absolute path; null when unreadable. */
	const listings = new SvelteMap<string, string[] | null>();
	const loading = new Set<string>();
	const listingKey = (path: string, hidden: boolean) => (hidden ? '1' : '0') + expand(path);

	// The typed text read as a folder (children) and as parent plus name prefix (siblings).
	const completion = $derived.by(() => {
		const slash = typed.lastIndexOf('/');
		if (browse || slash < 0) return null;
		const parent = typed.slice(0, slash + 1);
		const prefix = typed.slice(slash + 1);
		return {
			parent,
			prefix,
			self: prefix && prefix !== '.' && prefix !== '..' ? typed + '/' : '',
			parentKey: listingKey(parent, showHidden || prefix.startsWith('.')),
			selfKey: prefix && prefix !== '.' && prefix !== '..' ? listingKey(typed, showHidden) : ''
		};
	});

	$effect(() => {
		const c = completion;
		if (!c) return;
		const want = [c.parentKey, c.selfKey].filter((k) => k && !listings.has(k) && !loading.has(k));
		if (!want.length) return;
		const timer = setTimeout(() => want.forEach(load), 120);
		return () => clearTimeout(timer);
	});

	function load(key: string) {
		loading.add(key);
		api<FolderList>('GET', '/api/fs?path=' + encodeURIComponent(key.slice(1)) + (key[0] === '1' ? '&hidden=1' : ''))
			.then((r) => listings.set(key, r.dirs))
			.catch(() => listings.set(key, null))
			.finally(() => loading.delete(key));
	}

	const dirs = $derived.by(() => {
		const c = completion;
		if (!c || dismissed) return [];
		const known = new Set(projects.map((p) => app.tilde(p.path) + '/'));
		const out: Suggestion[] = [];
		const add = (head: string, name: string) => {
			const path = head + name + '/';
			if (!known.has(path) && out.length < 200) out.push({ kind: 'dir', path, head, tail: name + '/' });
		};
		// Results land under their own key, so a late reply for older text never shows.
		for (const d of (c.selfKey && listings.get(c.selfKey)) || []) if (showHidden || !d.startsWith('.')) add(c.self, d);
		const p = c.prefix.toLowerCase();
		for (const d of listings.get(c.parentKey) ?? []) {
			if (d === c.prefix || !d.toLowerCase().startsWith(p)) continue;
			if (showHidden || p.startsWith('.') || !d.startsWith('.')) add(c.parent, d);
		}
		return out;
	});

	const suggestions = $derived<Suggestion[]>([...projects.map((p) => ({ kind: 'project' as const, path: p.path, project: p })), ...dirs]);

	$effect(() => {
		if (hi >= 0) list?.querySelector('[data-hi]')?.scrollIntoView({ block: 'nearest' });
	});

	function edited() {
		hi = -1;
		dismissed = false;
		browse = null;
	}

	function pick(s: Suggestion) {
		dir = s.kind === 'dir' ? s.path : app.tilde(s.path);
		edited();
		input?.focus();
	}

	/** Tab with nothing highlighted: complete a single match or the shared prefix, else highlight the first folder. */
	function tabComplete() {
		const c = completion;
		if (!c) return false;
		if (c.self && Array.isArray(listings.get(c.selfKey))) {
			dir = c.self;
			edited();
			return true;
		}
		if (!dirs.length) return false;
		if (dirs.length === 1) {
			pick(dirs[0]);
			return true;
		}
		const siblings = dirs.filter((d) => d.kind === 'dir' && d.head === c.parent).map((d) => (d.kind === 'dir' ? d.tail.slice(0, -1) : ''));
		let common = siblings[0] ?? '';
		for (const n of siblings) while (!n.toLowerCase().startsWith(common.toLowerCase())) common = common.slice(0, -1);
		if (siblings.length === dirs.length && common.length > c.prefix.length) {
			dir = c.parent + common;
			edited();
		} else hi = projects.length;
		return true;
	}

	function onkeydown(e: KeyboardEvent) {
		if (browse) return;
		const n = suggestions.length;
		const current = suggestions[hi];
		if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && n) {
			e.preventDefault();
			const step = e.key === 'ArrowDown' ? 1 : -1;
			hi = hi < 0 ? (step > 0 ? 0 : n - 1) : (hi + step + n) % n;
		} else if (e.key === 'Tab' && !e.shiftKey && !e.altKey && !e.ctrlKey && !e.metaKey) {
			if (current) pick(current);
			else if (!tabComplete()) return;
			e.preventDefault();
		} else if (e.key === 'Enter' && current) {
			e.preventDefault();
			pick(current);
		} else if (e.key === 'Escape' && (hi >= 0 || dirs.length)) {
			// Closes the suggestions, not the dialog.
			e.preventDefault();
			e.stopPropagation();
			hi = -1;
			dismissed = true;
		}
	}

	async function openFolder(path: string) {
		try {
			browse = await api<FolderList>('GET', '/api/fs?path=' + encodeURIComponent(path) + (showHidden ? '&hidden=1' : ''));
			dir = app.tilde(browse.path);
			error = '';
		} catch (e) {
			error = (e as Error).message;
		}
	}

	async function restart(w: Win) {
		if (!w.info.exited) {
			const ok = await app.ask('Restart this session?', `This ends the running ${app.agentDisplay(w.info.agent)} and starts it again with these settings.`, [
				{ label: 'Restart', value: 'yes', tone: 'danger' }
			]);
			if (ok !== 'yes') return;
		}
		busy = true;
		error = '';
		const oldKey = windowKey(w.info);
		try {
			const info = await app.restartWith(w, Math.max(40, Math.floor((w.w - 24) / 7.8)), Math.max(10, Math.floor((w.h - 48) / 17)), request);
			app.sessionSettings = null;
			if (folder === start.folder && windowKey(info) === oldKey) return;
			if (data.folders?.some((f) => f.id === folder)) app.fileNewSession(info, folder);
			else if (start.folder) app.moveToFolder(oldKey, null);
		} catch (err) {
			error = (err as Error).message;
			busy = false;
		}
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (edit) return restart(edit);
		busy = true;
		error = '';
		const size = defaultWindowSize();
		try {
			const info = await api<SessionInfo>('POST', '/api/sessions', {
				...request,
				cols: Math.max(40, Math.floor((size.w - 24) / 7.8)),
				rows: Math.max(10, Math.floor((size.h - 48) / 17))
			});
			setLocal('last', { dir: info.dir, agent: info.agent, folder });
			app.newSessionOpen = false;
			app.openWindow(info);
			if (data.folders?.some((f) => f.id === folder)) app.fileNewSession(info, folder);
			app.refresh();
		} catch (err) {
			error = (err as Error).message;
			busy = false;
		}
	}

	const close = () => {
		if (edit) app.sessionSettings = null;
		else app.newSessionOpen = false;
	};
	const title = $derived(edit ? 'Session settings' : 'New session');
</script>

<Modal label={title} onclose={close} wide>
	<form onsubmit={submit} class="flex min-h-0 flex-col">
		<div class="flex items-center gap-3 border-b border-line-1 px-5 py-4">
			<div class="flex flex-1 flex-col gap-0.5">
				<span class="inline-flex items-center gap-1.5 font-mono text-[10.5px] tracking-[0.08em] text-ink-2 uppercase">
					<span class="size-1.5 rounded-full bg-brand"></span>Session
				</span>
				<h2 class="text-xl font-bold">{title}</h2>
			</div>
			<IconButton icon="x" label="Close" onclick={close} />
		</div>

		<div class="flex min-h-0 flex-1 flex-col gap-[18px] overflow-auto px-5 py-[18px]">
			{#if error}<Callout tone="bad">{error}</Callout>{/if}

			<!-- A new session starts at its name; settings start at the folder, as before. -->
			<TextField label="Session name" placeholder="Timestamp if empty" wrap="" data-autofocus={edit ? undefined : true} bind:value={name} />

			<div class="flex flex-col gap-2">
				<SectionLabel>Project folder</SectionLabel>
				<div class="flex gap-2">
					<input
						data-autofocus={edit ? true : undefined}
						bind:this={input}
						bind:value={dir}
						oninput={edited}
						{onkeydown}
						aria-label="Project folder"
						autocomplete="off"
						spellcheck="false"
						class="block w-full min-w-0 rounded-md border border-line-1 bg-inset px-2.5 py-[7px] font-mono text-[12.5px] outline-none hover:border-line-2 focus:border-brand"
					/>
					{#if browse}
						<Button icon="back" onclick={() => (browse = null)}>Known projects</Button>
					{:else}
						<Button icon="folder" onclick={() => openFolder(dir)}>Browse</Button>
					{/if}
				</div>
				<div bind:this={list} class="flex max-h-[220px] flex-col overflow-auto rounded-lg border border-line-1">
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
						{#each suggestions as s, i (s.kind + s.path)}
							{#if s.kind === 'dir' && (i === 0 || suggestions[i - 1].kind === 'project')}
								<div class="shrink-0 border-b border-line-1 px-3 pt-2 pb-1 font-mono text-[10.5px] tracking-[0.08em] text-ink-3 uppercase">Folders</div>
							{/if}
							<button
								type="button"
								data-hi={i === hi || undefined}
								onmousedown={(e) => e.preventDefault()}
								onclick={() => pick(s)}
								class={[
									'flex min-h-10 shrink-0 items-center gap-2.5 border-b border-line-1 px-3 text-left last:border-b-0 hover:bg-surface-2',
									i === hi ? 'bg-surface-2' : s.kind === 'project' && s.path === fullDir && 'bg-brand-soft'
								]}
							>
								{#if s.kind === 'project'}
									<Icon name="folder" />
									<span class="flex-1 truncate font-mono text-[12.5px]">{app.tilde(s.path)}</span>
									<span class="text-[11.5px] whitespace-nowrap text-ink-2">{s.project.scoped ? 'directory-scoped to ' + s.project.workspace : s.project.workspace}</span>
								{:else}
									<span class="text-ink-3"><Icon name="folder" /></span>
									<span class="flex-1 truncate font-mono text-[12.5px]"><span class="text-ink-3">{s.head}</span>{s.tail}</span>
									{#if i === hi}<Kbd>Tab</Kbd>{/if}
								{/if}
							</button>
						{:else}
							<div class="px-3 py-2.5 text-xs text-ink-3">{data.projects.length ? 'No matches. Type a path or browse.' : 'No known projects yet. Type a path or browse.'}</div>
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
							size="md"
							pressed={agent === a.name}
							disabled={!a.enabled}
							title={a.enabled ? a.display : a.display + ' is not enabled. Enable it under Settings > Agents.'}
							onclick={() => (agent = a.name)}><AgentIcon name={a.name} muted={!a.enabled} />{a.display}</Chip
						>
					{/each}
				</div>
			</div>

			<div class="grid grid-cols-3 gap-3 max-lg:grid-cols-2 max-md:grid-cols-1">
				<SelectField
					label="Workspace"
					wrap=""
					bind:value={workspace}
					onchange={() => (envProfile = '')}
					options={[{ value: '', label: 'Auto (resolution order)' }, ...data.workspaces.map((w) => ({ value: w.name, label: w.name }))]}
				/>
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
				<SelectField label="Sidebar folder" wrap="" bind:value={folder} options={[{ value: '', label: 'None' }, ...(data.folders ?? []).map((f) => ({ value: f.id, label: f.name }))]} />
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
				<div class="flex items-center gap-2">
					<SectionLabel>{edit && changed ? 'Restarts as' : 'Runs as'}</SectionLabel>
					<IconButton icon="copy" label="Copy command" small onclick={() => copy(shown)} />
				</div>
				<div class="rounded-lg border border-line-1 bg-inset px-3 py-2.5 font-mono text-xs break-all whitespace-pre-wrap">{shown}</div>
				{#if edit && changed}
					<small class="font-mono text-[11px] break-all text-ink-3" title="The command the session ran">Last run: {used}</small>
				{/if}
			</div>
		</div>

		<div class="flex flex-wrap items-center gap-2 border-t border-line-1 px-5 py-3.5">
			{#if edit}
				<Button type="submit" variant="primary" icon="rotate" disabled={busy || !agent || !dir.trim()}>Restart with these settings</Button>
				<Button variant="ghost" onclick={close}>Cancel</Button>
			{:else}
				<Button type="submit" variant="primary" icon="play" disabled={busy || !agent || !dir.trim()}>Start session</Button>
				<Button variant="ghost" onclick={close}>Cancel</Button>
				<span class="ml-auto text-[11.5px] text-ink-2">The image builds on first run.</span>
			{/if}
		</div>
	</form>
</Modal>
