<script lang="ts">
	import { app, OPEN_DRAG, sessionLabel, type SidebarDrop, type Win } from './app.svelte';
	import { sessionKey, type Folder, type RunningDoc, type SavedDoc } from './api';
	import Brand from './ui/Brand.svelte';
	import Icon from './ui/Icon.svelte';
	import IconButton from './ui/IconButton.svelte';
	import SectionLabel from './ui/SectionLabel.svelte';

	/** One session in the sidebar: an open window, a terminal session, or a saved one. */
	type Row =
		| { kind: 'window'; key: string; named: boolean; workspace: string; win: Win }
		| { kind: 'terminal'; key: string; named: boolean; workspace: string; run: RunningDoc }
		| { kind: 'saved'; key: string; named: true; workspace: string; saved: SavedDoc };

	const DRAG_TYPE = 'application/x-exitbox-session';

	const rows = $derived.by(() => {
		const d = app.data;
		if (!d) return [] as Row[];
		const out: Row[] = [];
		const running = new Set<string>();
		for (const w of app.windows) {
			const i = w.info;
			const ws = i.workspace || 'default';
			const key = i.name ? sessionKey(ws, i.agent, i.dir, i.name) : 'window:' + i.id;
			if (!i.exited) running.add(key);
			out.push({ kind: 'window', key, named: !!i.name, workspace: ws, win: w });
		}
		const attached = new Set(app.windows.filter((w) => w.attached && !w.info.exited).map((w) => w.info.container));
		for (const r of d.running ?? []) {
			// Until the next poll, a container just attached is listed twice.
			if (attached.has(r.container)) continue;
			const ws = r.workspace || 'default';
			const key = r.session ? sessionKey(ws, r.agent, r.dir, r.session) : 'container:' + r.container;
			running.add(key);
			out.push({ kind: 'terminal', key, named: !!r.session, workspace: ws, run: r });
		}
		for (const s of d.saved) {
			const key = sessionKey(s.workspace, s.agent, s.dir, s.name);
			if (!running.has(key)) out.push({ kind: 'saved', key, named: true, workspace: s.workspace, saved: s });
		}
		return out;
	});

	const folders = $derived(app.data?.folders ?? []);
	const folderOf = $derived(new Map(folders.flatMap((f) => f.items.map((k) => [k, f.id] as const))));
	const shown = (r: Row) => r.kind !== 'saved' || app.showInactive;

	const workspaceGroups = $derived.by(() => {
		const d = app.data;
		if (!d) return [];
		const groups = d.workspaces.map((w) => ({
			name: w.name,
			vault: { rw: 'vault rw', ro: 'vault ro', off: 'no vault' }[w.vault],
			isDefault: w.isDefault,
			rows: [] as Row[]
		}));
		for (const r of rows) {
			if (folderOf.has(r.key) || !shown(r)) continue;
			let g = groups.find((x) => x.name === r.workspace);
			if (!g) {
				g = { name: r.workspace, vault: '', isDefault: false, rows: [] };
				groups.push(g);
			}
			g.rows.push(r);
		}
		return groups.filter((g) => g.rows.length);
	});

	const inactiveCount = $derived(rows.filter((r) => r.kind === 'saved').length);
	const numbers = $derived(new Map(app.windows.map((w, i) => [w.id, i + 1])));

	// ---------- editing names ----------

	/** What is being named: a row key, a folder id, or a new folder. */
	let editing = $state<string | null>(null);
	let draft = $state('');
	/** The row whose folder menu is open. */
	let moving = $state<string | null>(null);

	function startEdit(id: string, current: string) {
		moving = null;
		editing = id;
		draft = current;
	}

	let saving = false;

	async function commit(save: (name: string) => Promise<boolean>, current: string) {
		// Enter commits, and the input's blur (also when it goes away) would again.
		if (editing === null || saving) return;
		const name = draft.trim();
		if (!name || name === current) {
			editing = null;
			return;
		}
		saving = true;
		try {
			if (await save(name)) editing = null;
		} finally {
			saving = false;
		}
	}

	function focusSelect(node: HTMLInputElement) {
		node.focus();
		node.select();
	}

	/** Unique per row: an ended window and its next run share a session key. */
	function rowId(r: Row) {
		if (r.kind === 'window') return 'w:' + r.win.id;
		if (r.kind === 'terminal') return 't:' + r.run.container;
		return 's:' + r.key;
	}

	function title(r: Row) {
		if (r.kind === 'window') return r.win.title;
		if (r.kind === 'terminal') return sessionLabel(r.run.session, r.run.agent, r.run.started);
		return r.saved.name;
	}

	function renameAction(r: Row): ((name: string) => Promise<boolean>) | null {
		if (r.kind === 'window' && !r.win.attached && r.named) return (n) => app.renameWindow(r.win, n);
		if (r.kind === 'saved') return (n) => app.renameSaved(r.saved, n);
		return null;
	}

	function open(r: Row) {
		if (r.kind === 'window') {
			app.view = 'sessions';
			app.show(r.win);
		} else if (r.kind === 'terminal') {
			if (!r.run.noTmux) app.attach(r.run);
		} else {
			app.resume(r.saved);
		}
	}

	async function newFolderFor(key: string) {
		moving = null;
		const name = 'New folder';
		if (await app.createFolder(name)) {
			const f = app.data?.folders.at(-1);
			if (f) {
				await app.moveToFolder(key, f.id);
				startEdit('folder:' + f.id, f.name);
			}
		}
	}

	function dropOn(e: DragEvent, folderId: string | null) {
		const key = e.dataTransfer?.getData(DRAG_TYPE);
		if (!key) return;
		e.preventDefault();
		if ((folderOf.get(key) ?? null) !== folderId) app.moveToFolder(key, folderId);
	}

	function allowDrop(e: DragEvent) {
		if (e.dataTransfer?.types.includes(DRAG_TYPE)) e.preventDefault();
	}
</script>

{#snippet nameInput(save: (name: string) => Promise<boolean>, current: string, label: string)}
	<input
		bind:value={draft}
		{@attach focusSelect}
		aria-label={label}
		spellcheck="false"
		onkeydown={(e) => {
			if (e.key === 'Enter') {
				e.preventDefault();
				commit(save, current);
			} else if (e.key === 'Escape') {
				e.preventDefault();
				editing = null;
			}
		}}
		onblur={() => {
			// After a save the row may be gone: check before touching its values.
			if (editing !== null && !saving) commit(save, current);
		}}
		class="mx-1 my-1.5 block min-w-0 flex-1 rounded-md border border-brand bg-surface-0 px-2 py-1.5 text-[13px] outline-none"
	/>
{/snippet}

{#snippet row(r: Row)}
	{@const rename = renameAction(r)}
	{@const name = title(r)}
	<div
		class={['group relative flex items-center rounded-md hover:bg-surface-2', r.kind === 'window' && app.focusedId === r.win.id && 'bg-brand-soft']}
		draggable={editing !== r.key && !(r.kind === 'terminal' && r.run.noTmux)}
		ondragstart={(e) => {
			// Onto a folder it files the session; onto the desktop or a tab it opens it.
			if (r.named) e.dataTransfer?.setData(DRAG_TYPE, r.key);
			const drop: SidebarDrop =
				r.kind === 'window' ? { kind: 'window', id: r.win.id } : r.kind === 'terminal' ? { kind: 'terminal', container: r.run.container } : { kind: 'saved', saved: r.saved };
			e.dataTransfer?.setData(OPEN_DRAG, JSON.stringify(drop));
			if (e.dataTransfer) e.dataTransfer.effectAllowed = 'copyMove';
		}}
		role="listitem"
	>
		{#if editing === r.key && rename}
			{@render nameInput(rename, name, 'New session name')}
		{:else}
			<button
				type="button"
				onclick={() => open(r)}
				disabled={r.kind === 'terminal' && r.run.noTmux}
				title={r.kind === 'terminal'
					? r.run.noTmux
						? 'Runs without tmux (--no-tmux): use it in the terminal that started it.'
						: 'Started in a terminal. Click to open it here too.'
					: r.kind === 'saved'
						? 'Resume this saved session'
						: r.win.min
							? 'Hidden window. Click to show it.'
							: r.win.info.renameTo
								? `Renames to "${r.win.info.renameTo}" when it ends`
								: undefined}
				class="flex min-h-11 min-w-0 flex-1 items-center gap-2.5 py-1.5 pr-1 pl-2 text-left disabled:cursor-not-allowed disabled:opacity-60"
			>
				<span
					class={[
						'size-[7px] shrink-0 rounded-full',
						r.kind === 'saved' && 'bg-ink-4',
						r.kind === 'terminal' && 'bg-accent',
						r.kind === 'window' && (r.win.info.exited ? 'bg-ink-4' : r.win.min ? 'bg-amber' : 'bg-brand')
					]}
				></span>
				<span class="flex min-w-0 flex-col">
					<span class={['truncate text-[13px]', r.kind === 'saved' ? 'font-medium text-ink-2' : 'font-semibold']}>{name}</span>
					<span class={['truncate font-mono text-[10.5px]', r.kind === 'saved' ? 'text-ink-3' : 'text-ink-2']}>
						{#if r.kind === 'window'}
							{r.win.info.agent} · {app.tilde(r.win.info.dir)}{app.tabs.length > 1 ? ' · ' + (app.tabs.find((t) => t.id === r.win.tab)?.name ?? '') : ''}{r.win.attached ? ' · terminal' : ''}{r.win.info.exited ? (r.win.attached ? ' · detached' : ' · ended') : r.win.min ? ' · hidden' : ''}
						{:else if r.kind === 'terminal'}
							{r.run.agent} · {app.tilde(r.run.dir)} · terminal
						{:else}
							{r.saved.agent} · {app.tilde(r.saved.dir)}
						{/if}
					</span>
				</span>
			</button>
			{#if r.kind === 'terminal'}
				<span class="mr-2 text-ink-3 group-hover:invisible"><Icon name={r.run.noTmux ? 'lock' : 'terminal'} size={12} /></span>
			{:else if r.kind === 'window'}
				<span class="mr-2 min-w-[18px] shrink-0 rounded bg-surface-2 px-1.5 text-center font-mono text-[10.5px] text-ink-2 group-hover:invisible">{numbers.get(r.win.id)}</span>
			{/if}
			<!-- Row actions overlay the end of the row on hover, so titles keep their width. -->
			<span
				class={[
					'absolute top-1/2 right-1 -translate-y-1/2 items-center gap-0.5 rounded-md bg-surface-2 pl-1 group-focus-within:flex group-hover:flex',
					moving === r.key ? 'flex' : 'hidden'
				]}
			>
				{#if rename}
					<IconButton icon="pencil" label={'Rename ' + name} small onclick={() => startEdit(r.key, name)} />
				{/if}
				{#if r.named}
					<IconButton icon="folderInput" label={'Move ' + name + ' to a folder'} small onclick={() => (moving = moving === r.key ? null : r.key)} />
				{/if}
				{#if r.kind === 'saved'}
					<IconButton icon="trash" label={'Forget saved session ' + name} small class="hover:text-warn" onclick={() => app.forget(r.saved)} />
				{/if}
			</span>
		{/if}

		{#if moving === r.key}
			<div class="absolute top-full right-1 z-20 mt-1 flex w-52 flex-col rounded-lg border border-line-2 bg-surface-1 p-1 shadow-float" role="menu">
				{#each folders as f (f.id)}
					<button type="button" role="menuitem" onclick={() => { moving = null; app.moveToFolder(r.key, f.id); }} class="flex h-8 items-center gap-2 rounded px-2 text-left text-[13px] hover:bg-surface-2">
						<Icon name="folder" size={12} /><span class="flex-1 truncate">{f.name}</span>
						{#if folderOf.get(r.key) === f.id}<Icon name="check" size={12} />{/if}
					</button>
				{/each}
				{#if folderOf.has(r.key)}
					<button type="button" role="menuitem" onclick={() => { moving = null; app.moveToFolder(r.key, null); }} class="flex h-8 items-center gap-2 rounded px-2 text-left text-[13px] text-ink-2 hover:bg-surface-2">
						<Icon name="x" size={12} />Take out of folder
					</button>
				{/if}
				<button type="button" role="menuitem" onclick={() => newFolderFor(r.key)} class="flex h-8 items-center gap-2 rounded px-2 text-left text-[13px] text-ink-2 hover:bg-surface-2">
					<Icon name="folderPlus" size={12} />New folder
				</button>
			</div>
		{/if}
	</div>
{/snippet}

{#snippet folderSection(f: Folder)}
	{@const inside = rows.filter((r) => folderOf.get(r.key) === f.id)}
	{@const visibleRows = inside.filter(shown)}
	{@const hidden = inside.length - visibleRows.length}
	{@const open = !app.collapsed[f.id]}
	<div class="flex flex-col gap-0.5 rounded-md" ondragover={allowDrop} ondrop={(e) => dropOn(e, f.id)} role="list" aria-label={'Folder ' + f.name}>
		<div class="group flex items-center rounded-md hover:bg-surface-2">
			{#if editing === 'folder:' + f.id}
				{@render nameInput((n) => app.renameFolder(f.id, n), f.name, 'Folder name')}
			{:else}
				<button type="button" aria-expanded={open} onclick={() => app.toggleFolder(f.id)} class="flex h-8 min-w-0 items-center gap-1.5 pl-1 text-left">
					<span class="text-ink-3"><Icon name={open ? 'chevronDown' : 'chevronRight'} size={12} /></span>
					<span class="text-ink-2"><Icon name="folder" size={13} /></span>
					<span class="truncate text-[13px] font-semibold">{f.name}</span>
				</button>
				<span class="flex-1"></span>
				<span class="mr-2 font-mono text-[10.5px] text-ink-3 group-hover:hidden">{visibleRows.length}</span>
				<span class="mr-1 hidden items-center gap-0.5 group-focus-within:flex group-hover:flex">
					<IconButton icon="pencil" label={'Rename folder ' + f.name} small onclick={() => startEdit('folder:' + f.id, f.name)} />
					<IconButton icon="trash" label={'Delete folder ' + f.name} small class="hover:text-warn" onclick={() => app.deleteFolder(f)} />
				</span>
			{/if}
		</div>
		{#if open}
			<div class="flex flex-col gap-0.5 pl-3">
				{#each visibleRows as r (rowId(r))}
					{@render row(r)}
				{/each}
				{#if !visibleRows.length}
					<div class="px-2 pb-1.5 text-xs text-ink-3">
						{hidden ? `${hidden} inactive ${hidden === 1 ? 'session' : 'sessions'} hidden` : 'Empty. Drag sessions here.'}
					</div>
				{/if}
			</div>
		{/if}
	</div>
{/snippet}

<svelte:window
	onkeydown={(e) => {
		if (e.key === 'Escape' && moving) moving = null;
	}}
/>

{#if app.sidebarCollapsed}
	<nav aria-label="Sessions" class="flex min-h-0 w-12 shrink-0 flex-col items-center gap-1 border-r border-line-1 bg-surface-1 py-2 max-md:w-full max-md:flex-row max-md:border-r-0 max-md:border-b max-md:px-2">
		<IconButton icon="panelOpen" label="Show the sidebar" onclick={() => app.toggleSidebar()} />
		<div class="my-1 h-px w-6 bg-line-1 max-md:hidden"></div>
		<div class="flex min-h-0 flex-1 flex-col items-center gap-1 overflow-auto max-md:flex-row">
			{#each app.windows as w (w.id)}
				<button
					type="button"
					title={w.title + ' · ' + w.info.agent + ' · ' + app.tilde(w.info.dir)}
					aria-label={'Show ' + w.title}
					onclick={() => {
						app.view = 'sessions';
						app.show(w);
					}}
					class={[
						'relative grid size-8 shrink-0 place-items-center rounded-md font-mono text-[11px] hover:bg-surface-2',
						app.focusedId === w.id ? 'bg-brand-soft text-ink-1' : 'text-ink-2'
					]}
				>
					{numbers.get(w.id)}
					<span class={['absolute right-1 bottom-1 size-1.5 rounded-full', w.info.exited ? 'bg-ink-4' : w.min ? 'bg-amber' : 'bg-brand']}></span>
				</button>
			{/each}
		</div>
		<IconButton
			icon="settings"
			label="Settings"
			class={app.view === 'settings' ? 'bg-surface-3 text-ink-1' : ''}
			onclick={() => (app.view === 'settings' ? (app.view = 'sessions') : app.openSettings())}
		/>
	</nav>
{:else}
<nav aria-label="Sessions" class="flex min-h-0 w-[272px] shrink-0 flex-col border-r border-line-1 bg-surface-1 max-md:max-h-[40vh] max-md:w-full max-md:flex-none max-md:border-r-0 max-md:border-b">
	<div class="flex h-12 shrink-0 items-center justify-between border-b border-line-1 pr-2 pl-3.5">
		<Brand />
		<IconButton icon="panelClose" label="Collapse the sidebar" onclick={() => app.toggleSidebar()} />
	</div>

	{#if moving}
		<!-- Closes the folder menu on an outside click. -->
		<button type="button" aria-label="Close menu" tabindex="-1" class="fixed inset-0 z-10 cursor-default" onclick={() => (moving = null)}></button>
	{/if}

	<div class="flex flex-1 flex-col gap-0.5 overflow-auto p-2">
		<div class="flex items-center gap-2 px-2 pt-1 pb-1">
			<SectionLabel>Folders</SectionLabel>
			<span class="flex-1"></span>
			<IconButton icon="folderPlus" label="New folder" small onclick={() => startEdit('new-folder', '')} />
		</div>
		{#if editing === 'new-folder'}
			<div class="flex">{@render nameInput((n) => app.createFolder(n), '', 'New folder name')}</div>
		{/if}
		{#each folders as f (f.id)}
			{@render folderSection(f)}
		{/each}

		{#each workspaceGroups as g (g.name)}
			<div class="flex items-center gap-2 px-2 pt-3 pb-1" ondragover={allowDrop} ondrop={(e) => dropOn(e, null)} role="presentation">
				<SectionLabel>{g.name}</SectionLabel>
				{#if g.isDefault}<span class="rounded border border-line-2 bg-surface-2 px-1.5 font-mono text-[9.5px] font-bold text-ink-2">default</span>{/if}
				<span class="ml-auto font-mono text-[10.5px] text-ink-2">{g.vault}</span>
			</div>
			<div class="flex flex-col gap-0.5" role="list" aria-label={'Workspace ' + g.name} ondragover={allowDrop} ondrop={(e) => dropOn(e, null)}>
				{#each g.rows as r (rowId(r))}
					{@render row(r)}
				{/each}
			</div>
		{:else}
			{#if !rows.some(shown)}
				<div class="px-2 py-3 text-xs text-ink-3">
					Nothing running.{inactiveCount && !app.showInactive ? ` ${inactiveCount} saved ${inactiveCount === 1 ? 'session is' : 'sessions are'} hidden.` : ''}
				</div>
			{/if}
		{/each}
	</div>

	<div class="flex shrink-0 flex-col gap-0.5 border-t border-line-1 p-2">
		<button
			type="button"
			aria-pressed={app.showInactive}
			onclick={() => app.toggleInactive()}
			class="flex h-[34px] w-full items-center gap-2.5 rounded-md px-2 text-left text-[13px] text-ink-2 hover:bg-surface-2 hover:text-ink-1"
		>
			<span class={['grid size-4 shrink-0 place-items-center rounded border text-surface-0', app.showInactive ? 'border-brand bg-brand' : 'border-line-3 bg-inset']}>
				{#if app.showInactive}<Icon name="check" size={12} />{/if}
			</span>
			Show inactive sessions
			<span class="ml-auto font-mono text-[10.5px]">{inactiveCount}</span>
		</button>
		<button
			type="button"
			onclick={() => (app.view === 'settings' ? (app.view = 'sessions') : app.openSettings())}
			class={['flex h-[34px] w-full items-center gap-2.5 rounded-md px-2 text-left text-[13px]', app.view === 'settings' ? 'bg-surface-3 text-ink-1' : 'text-ink-2 hover:bg-surface-2 hover:text-ink-1']}
		>
			<Icon name="settings" /> Settings
		</button>
		<div class="flex items-center gap-2 px-2 py-1.5 font-mono text-[10.5px] text-ink-2">
			<Icon name="shield" size={12} />
			<span>{app.data?.runtime || 'no container runtime'} · firewall {app.data?.defaults.noFirewall ? 'off by default' : 'on'}</span>
		</div>
	</div>
</nav>
{/if}
