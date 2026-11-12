<script lang="ts">
	import { api } from '../api';
	import type { SettingsDoc } from '../api';
	import { app } from '../app.svelte';
	import Button from '../ui/Button.svelte';
	import Callout from '../ui/Callout.svelte';
	import Icon from '../ui/Icon.svelte';
	import SectionLabel from '../ui/SectionLabel.svelte';
	import SelectField from '../ui/SelectField.svelte';
	import TextField from '../ui/TextField.svelte';
	import Toggle from '../ui/Toggle.svelte';
	import type { IconName } from '../ui/icons';
	import ListEditor from './ListEditor.svelte';
	import PageHeader from './PageHeader.svelte';
	import TerminalFont from './TerminalFont.svelte';
	import Vault from './Vault.svelte';
	import WebAccess from './WebAccess.svelte';

	const sections: { id: string; label: string; icon: IconName }[] = [
		{ id: 'general', label: 'General', icon: 'settings' },
		{ id: 'agents', label: 'Agents', icon: 'bot' },
		{ id: 'workspaces', label: 'Workspaces', icon: 'folder' },
		{ id: 'vault', label: 'Vault', icon: 'lock' },
		{ id: 'firewall', label: 'Firewall', icon: 'shield' },
		{ id: 'keybindings', label: 'Keybindings', icon: 'command' },
		{ id: 'web', label: 'Web access', icon: 'globe' },
		{ id: 'terminal', label: 'Terminal', icon: 'terminal' }
	];

	const agentInfo: Record<string, string> = {
		claude: "Anthropic's AI coding assistant",
		codex: "OpenAI's coding CLI",
		copilot: "GitHub's AI coding agent in the terminal",
		cursor: "Cursor's terminal coding agent",
		kimi: "Moonshot AI's terminal coding agent",
		opencode: 'Open-source AI code assistant',
		pi: 'Extensible multi-provider coding agent CLI',
		qwen: 'Open-source AI agent for the terminal'
	};

	let saveError = $state('');
	let saved = $state(false);

	const doc = $derived(app.settings);

	/** Counts edited fields against the loaded copy. */
	const changes = $derived.by(() => {
		if (!doc) return 0;
		const before = JSON.parse(app.settingsOrig) as Record<string, unknown>;
		const after = $state.snapshot(doc) as unknown as Record<string, unknown>;
		let n = 0;
		const walk = (a: unknown, b: unknown) => {
			if (Array.isArray(a) || Array.isArray(b)) {
				if (JSON.stringify(a) !== JSON.stringify(b)) n++;
				return;
			}
			if (a && typeof a === 'object') {
				const ao = a as Record<string, unknown>;
				const bo = (b ?? {}) as Record<string, unknown>;
				for (const k of new Set([...Object.keys(ao), ...Object.keys(bo)])) walk(ao[k], bo[k]);
				return;
			}
			if (a !== b) n++;
		};
		walk(before, after);
		return n;
	});

	async function save() {
		if (!doc) return;
		saveError = '';
		try {
			const next = await api<SettingsDoc>('PUT', '/api/settings', $state.snapshot(doc));
			app.settings = next;
			app.settingsOrig = JSON.stringify(next);
			saved = true;
			app.refresh();
		} catch (e) {
			saveError = (e as Error).message;
		}
	}

	function discard() {
		app.settings = JSON.parse(app.settingsOrig);
		saveError = '';
	}

	const isDomain = (v: string) => /^(\*\.)?[A-Za-z0-9.-]+$|^[0-9a-fA-F:.[\]]+$/.test(v);
	const isPackage = (v: string) => /^[A-Za-z0-9][A-Za-z0-9.+_-]*$/.test(v);
	const isPort = (v: string) => /^[0-9]{1,5}$/.test(v) && +v >= 1 && +v <= 65535;
</script>

<div class={['flex min-h-0 flex-1 max-md:flex-col', app.view !== 'settings' && 'hidden']}>
	<nav aria-label="Settings sections" class="flex-[0_0_220px] overflow-auto border-r border-line-1 px-2 py-3 max-md:flex max-md:flex-none max-md:flex-wrap max-md:gap-1 max-md:border-r-0 max-md:border-b">
		<button type="button" onclick={() => (app.view = 'sessions')} class="flex h-[34px] w-full items-center gap-2.5 rounded-md px-2 text-left text-[13px] text-ink-2 hover:bg-surface-2 hover:text-ink-1 max-md:w-auto">
			<Icon name="back" /> Back to sessions
		</button>
		<div class="px-2 pt-3.5 pb-1 max-md:hidden"><SectionLabel>Configure</SectionLabel></div>
		{#each sections as s (s.id)}
			<button
				type="button"
				onclick={() => (app.settingsSection = s.id)}
				aria-current={app.settingsSection === s.id ? 'page' : undefined}
				class={[
					'flex h-[34px] w-full items-center gap-2.5 rounded-md px-2 text-left text-[13px] max-md:w-auto',
					app.settingsSection === s.id ? 'bg-surface-3 text-ink-1' : 'text-ink-2 hover:bg-surface-2 hover:text-ink-1'
				]}
			>
				<Icon name={s.icon} />
				{s.label}
			</button>
		{/each}
	</nav>

	<div class="flex min-w-0 flex-1 flex-col">
		<div class="flex-1 overflow-auto">
			<div class="flex w-full max-w-[1040px] flex-col gap-5 px-8 py-7 max-md:px-4 max-md:py-5">
				{#if app.settingsSection === 'terminal'}
					<!-- Per browser: needs no server settings. -->
					<TerminalFont />
				{:else if app.settingsError && !doc}
					<Callout tone="bad">{app.settingsError}</Callout>
				{:else if !doc}
					<span class="text-ink-2">Loading settings…</span>
				{:else}
					{#if saveError}<Callout tone="bad">{saveError}</Callout>{/if}

					{#if app.settingsSection === 'general'}
						<PageHeader kind="General" title="Settings" lead="Global defaults for every session unless a run option overrides them." source="exitbox setup · General settings" />
						<div class="flex flex-wrap gap-2.5">
							<Toggle label="Auto-update agents" description="Check for new versions on every launch (slows down startup)" bind:checked={doc.autoUpdate} />
							<Toggle label="Auto-update tools to latest" description="Track newest versions for unpinned tools (Go, Node, Bun, ...)" bind:checked={doc.autoUpdateTools} />
							<Toggle label="Status bar" description="Show a status bar with version and agent info during sessions" bind:checked={doc.statusBar} />
							<Toggle
								label="Network firewall"
								description="Restrict outbound network to allowlisted domains only (disabling decreases security)"
								checked={!doc.noFirewall}
								onchange={(v) => (doc.noFirewall = !v)}
							/>
							<Toggle label="Auto-resume sessions" description="Automatically resume the last agent conversation" bind:checked={doc.autoResume} />
							<Toggle label="Pass host environment" description="Forward host environment variables into the container" checked={!doc.noEnv} onchange={(v) => (doc.noEnv = !v)} />
							<Toggle label="Read-only workspace" description="Mount workspace as read-only (agents cannot modify files)" bind:checked={doc.readOnly} />
							<Toggle label="Full Git support" description="Mount SSH agent + .gitconfig into container (exposes git identity)" bind:checked={doc.fullGit} />
							<Toggle label="RTK token optimizer" description="Use rtk to reduce CLI output tokens by 60-90%" tag="experimental" bind:checked={doc.rtk} />
						</div>
						<SectionLabel>Defaults</SectionLabel>
						<div class="flex flex-wrap gap-2.5">
							<SelectField
								label="Default workspace"
								bind:value={doc.defaultWorkspace}
								options={[{ value: '', label: 'Not set (the active workspace)' }, ...doc.workspaces.map((w) => ({ value: w.name, label: w.name }))]}
							/>
							<TextField label="Memory limit" placeholder="8g" bind:value={doc.memory} />
							<TextField label="CPU limit" placeholder="4" bind:value={doc.cpus} />
						</div>
					{:else if app.settingsSection === 'agents'}
						<PageHeader kind="Agents" title="Agents" lead="Enabled agents build their image on first run. An empty version tracks the latest release." source="exitbox setup · Agents · agents.<name>" />
						<div class="flex flex-wrap gap-2.5">
							{#each doc.agents as a (a.name)}
								<Toggle label={a.display} description={agentInfo[a.name] ?? a.name} bind:checked={a.enabled} />
							{/each}
						</div>
						<SectionLabel>Version pins</SectionLabel>
						<div class="flex flex-wrap gap-2.5">
							{#each doc.agents.filter((a) => a.enabled) as a (a.name)}
								<TextField label={a.display} mono placeholder="latest" bind:value={a.version} />
							{/each}
						</div>
					{:else if app.settingsSection === 'workspaces'}
						<PageHeader
							kind="Workspaces"
							title="Workspaces"
							lead="Named contexts with their own credentials, tools and vault. Roles, languages, tools and plugins are set up in exitbox setup; the vault is under Vault."
							source="exitbox setup · Workspace management"
						/>
						{#each doc.workspaces as w (w.name)}
							<div class="flex flex-col gap-3 rounded-xl border border-line-1 bg-surface-1 p-3.5">
								<h3 class="flex items-center gap-2 text-[15px] font-bold">
									{w.name}
									{#if w.name === doc.defaultWorkspace}<span class="rounded border border-line-2 bg-surface-2 px-1.5 font-mono text-[9.5px] font-bold text-ink-2">default</span>{/if}
									<span class="rounded border border-line-2 bg-surface-2 px-1.5 font-mono text-[9.5px] font-bold text-ink-2">
										{({ rw: 'vault rw', ro: 'vault ro', off: 'no vault' } as Record<string, string>)[w.vault ?? 'off']}
									</span>
								</h3>
								{#if w.development?.length}<span class="text-xs text-ink-2">Languages: {w.development.join(', ')}</span>{/if}
								<div class="flex flex-wrap gap-2.5">
									<TextField label="Directory scope" mono placeholder="~/code/project" hint="Sessions started in this directory use this workspace automatically." bind:value={w.directory} />
									<Toggle label="GPU passthrough" description="Expose host GPUs (Ubuntu CUDA image; needs the NVIDIA Container Toolkit)" tag="experimental" bind:checked={w.gpu} />
								</div>
								<SectionLabel>Extra packages</SectionLabel>
								<ListEditor label={'Add a package to ' + w.name} bind:items={w.packages} placeholder="Alpine package name" valid={isPackage} />
							</div>
						{/each}
					{:else if app.settingsSection === 'firewall'}
						<PageHeader
							kind="General"
							title="Firewall"
							lead="Squid allowlist. Fails closed: a missing or empty allowlist blocks all outbound destinations. example.com covers its subdomains."
							source="exitbox setup · Firewall · allowlist.yaml"
						/>
						<SectionLabel>Custom domains</SectionLabel>
						<ListEditor label="Add a domain" bind:items={doc.allowlist.custom} placeholder="mycompany.com" valid={isDomain} />
						<SectionLabel>Host ports</SectionLabel>
						<span class="text-xs text-ink-2">Ports on the host that agents may reach as localhost:PORT.</span>
						<ListEditor
							label="Add a host port"
							bind:items={() => doc.allowlist.hostPorts.map(String), (v) => (doc.allowlist.hostPorts = v.map(Number))}
							placeholder="5432"
							valid={isPort}
						/>
						{#each [['AI providers', doc.allowlist.aiProviders], ['Development', doc.allowlist.development], ['Cloud services', doc.allowlist.cloudServices], ['Common services', doc.allowlist.commonServices]] as const as [title, list] (title)}
							{#if list?.length}
								<SectionLabel>{title}</SectionLabel>
								<div class="flex flex-wrap gap-1.5">
									{#each list as d (d)}
										<span class="rounded border border-line-2 bg-surface-2 px-2 py-[3px] font-mono text-[11.5px]">{d}</span>
									{/each}
								</div>
							{/if}
						{/each}
					{:else if app.settingsSection === 'keybindings'}
						<PageHeader
							kind="General"
							title="Keybindings"
							lead="In-session menus, in tmux key notation. They work the same in browser windows."
							source="exitbox setup · Keybindings · settings.keybindings"
						/>
						<div class="flex flex-wrap gap-2.5">
							<TextField label="Workspace menu" mono placeholder={doc.keybindingDefaults?.workspaceMenu} hint="Save the current session, then switch workspace." bind:value={doc.workspaceMenu} />
							<TextField label="Session menu" mono placeholder={doc.keybindingDefaults?.sessionMenu} hint="Switch to another named session or start a new one." bind:value={doc.sessionMenu} />
						</div>
					{:else if app.settingsSection === 'vault'}
						<Vault />
					{:else if doc.web}
						<WebAccess web={doc.web} />
					{/if}
				{/if}
			</div>
		</div>

		{#if doc && !['web', 'vault', 'terminal'].includes(app.settingsSection)}
			<div class="flex flex-wrap items-center gap-2.5 border-t border-line-1 bg-surface-1 px-8 py-3 max-md:px-4">
				<Button variant="primary" icon="check" disabled={changes === 0} onclick={save}>Save configuration</Button>
				<Button variant="ghost" disabled={changes === 0} onclick={discard}>Discard</Button>
				<span class="font-mono text-[11.5px] text-ink-2">
					{saved && changes === 0 ? 'Saved' : changes === 0 ? 'No unsaved changes' : changes + (changes === 1 ? ' unsaved change' : ' unsaved changes')}
				</span>
				<span class="ml-auto text-[11.5px] text-ink-2">
					Writes <span class="font-mono">config.yaml</span> and <span class="font-mono">allowlist.yaml</span>, the same files as
					<span class="font-mono">exitbox setup</span>. Running sessions keep their settings.
				</span>
			</div>
		{/if}
	</div>
</div>
