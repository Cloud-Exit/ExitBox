<script lang="ts">
	import { app, OPEN_DRAG, sessionLabel, windowKey, type SidebarDrop, type Win } from './app.svelte';
	import { sessionKey, sshTarget, type Folder, type RunningDoc, type SavedDoc } from './api';
	import AgentIcon from './AgentIcon.svelte';
	import SessionMenu, { folderItems, folderNaming, openTarget, type SessionTarget } from './SessionMenu.svelte';
	import Brand from './ui/Brand.svelte';
	import Button from './ui/Button.svelte';
	import ContextMenu, { type MenuItem } from './ui/ContextMenu.svelte';
	import Icon from './ui/Icon.svelte';
	import IconButton from './ui/IconButton.svelte';
	import SectionLabel from './ui/SectionLabel.svelte';
	import Spinner from './ui/Spinner.svelte';
	import StatusGlyph, { type SessionState } from './ui/StatusGlyph.svelte';
import { viewport } from './viewport.svelte';
	import { longPress } from './mobile/longPress';

	/** One session in the sidebar: an open window, a terminal session, or a saved one. */
	type Row =
		| { kind: 'window'; key: string; named: boolean; workspace: string; win: Win }
		| { kind: 'terminal'; key: string; named: boolean; workspace: string; run: RunningDoc }
		| { kind: 'saved'; key: string; named: true; workspace: string; saved: SavedDoc };

	const DRAG_TYPE = 'application/x-exitbox-session';
	/** The sidebar group of SSH sessions; no workspace can have this name. */
	const SSH_GROUP = '\u0000ssh';

	const rows = $derived.by(() => {
		const d = app.data;
		if (!d) return [] as Row[];
		const out: Row[] = [];
		const running = new Set<string>();
		for (const w of app.windows) {
			const i = w.info;
			const ws = i.kind === 'ssh' ? SSH_GROUP : i.workspace || 'default';
			const key = windowKey(i);
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
	const folderOf = $derived(
		new Map([
			...folders.flatMap((f) => f.items.map((k) => [k, f.id] as const)),
			// A new unnamed run shows in its folder before it can be filed.
			...Object.entries(app.pendingFolder)
				.filter(([, f]) => folders.some((x) => x.id === f))
				.map(([id, f]) => ['window:' + id, f] as const)
		])
	);
	let query = $state('');
	const needle = $derived(query.trim().toLowerCase());

	function matches(r: Row) {
		if (r.kind === 'window' && r.win.ssh) {
			const o = r.win.info.ssh;
			return [title(r), 'ssh', o?.host ?? '', o ? sshTarget(o) : ''].some((s) => s.toLowerCase().includes(needle));
		}
		const [agent, dir] = r.kind === 'window' ? [r.win.info.agent, r.win.info.dir] : r.kind === 'terminal' ? [r.run.agent, r.run.dir] : [r.saved.agent, r.saved.dir];
		return [title(r), agent, r.workspace, dir, app.tilde(dir)].some((s) => s.toLowerCase().includes(needle));
	}

	const DAY = 86_400_000;
	/** Coarse: it only moves rows across the recently ended and 7 day marks. */
	let now = $state(Date.now());
	$effect(() => {
		const t = setInterval(() => (now = Date.now()), 60_000);
		return () => clearInterval(t);
	});

	/** When a saved session was last used, 0 when unknown. */
	const lastUsed = (r: Row) => (r.kind === 'saved' && r.saved.lastUsed ? Date.parse(r.saved.lastUsed) || 0 : 0);

	function winState(w: Win): SessionState {
		if (w.prompts.length) return 'waiting';
		// Hidden windows still run here.
		if (!w.info.exited) return 'active';
		// Its window closed, the session runs on in its terminal.
		if (w.attached) return 'detached';
		// Without a readable end time it ended just now.
		const ended = w.info.ended ? Date.parse(w.info.ended) : NaN;
		return Number.isFinite(ended) && ended <= now - app.recentDays * DAY ? 'inactive' : 'ended';
	}

	function rowState(r: Row): SessionState {
		if (r.kind === 'window') return winState(r.win);
		if (r.kind === 'terminal') return 'detached';
		return lastUsed(r) > now - app.recentDays * DAY ? 'ended' : 'inactive';
	}

	// An open window stays listed, however long ago it ended.
	const isInactive = (r: Row) => r.kind !== 'window' && rowState(r) === 'inactive';
	const byLastUsed = (a: Row, b: Row) => lastUsed(b) - lastUsed(a);

	// A search also finds inactive sessions while they are hidden.
	const shown = (r: Row) => (needle ? matches(r) : !isInactive(r) || app.showInactive);
const anyFolderMatch = $derived(!needle || rows.some((r) => folderOf.has(r.key) && shown(r)));

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
		const week = now - 7 * DAY;
		// SSH last, after every workspace; inactive rows after the rest, by when they were last used.
		return groups
			.filter((g) => g.rows.length)
			.sort((a, b) => +(a.name === SSH_GROUP) - +(b.name === SSH_GROUP))
			.map((g) => {
				const old = g.rows.filter(isInactive).sort(byLastUsed);
				return { ...g, rows: g.rows.filter((r) => !isInactive(r)), week: old.filter((r) => lastUsed(r) > week), older: old.filter((r) => lastUsed(r) <= week) };
			});
	});

	const inactiveCount = $derived(rows.filter(isInactive).length);
	const numbers = $derived(new Map(app.windows.map((w, i) => [w.id, i + 1])));
	/** On a phone the sidebar is the full-screen sessions list. */
	const phone = $derived(viewport.phone);
	const runningCount = $derived(app.running + (app.data?.running?.length ?? 0));
	const user = $derived(app.data?.user || app.me?.user || '');

	/** A folder's actions on a phone, where its hover buttons cannot show. */
	let folderMenu = $state<{ label: string; items: MenuItem[]; x: number; y: number; anchor: HTMLElement } | null>(null);

	function openFolderMenu(f: Folder, anchor: HTMLElement) {
		const r = anchor.getBoundingClientRect();
		folderMenu = {
			label: 'Folder ' + f.name,
			items: [
				{ label: 'Rename folder', icon: 'pencil', onselect: () => startEdit('folder:' + f.id, f.name) },
				{ label: 'Delete folder', icon: 'trash', danger: true, onselect: () => void app.deleteFolder(f) }
			],
			x: r.left,
			y: r.bottom,
			anchor
		};
	}

	function openSettings() {
		app.sessionsOpen = false;
		if (app.view === 'settings') app.view = 'sessions';
		else app.openSettings();
	}

	// ---------- editing names ----------

	/** What is being named: a row id (not its key, which an ended window and its next run share), a folder, or a new folder. */
	let editing = $state<string | null>(null);
	let draft = $state('');

	function startEdit(id: string, current: string) {
		menu = null;
		editing = id;
		draft = current;
	}

	// A folder made from a session menu (here or in a title bar) is named here.
	$effect(() => {
		const id = folderNaming.id;
		if (!id) return;
		folderNaming.id = null;
		const f = folders.find((x) => x.id === id);
		if (f) startEdit('folder:' + f.id, f.name);
	});

	// ---------- menus ----------

	/** The open menu: a row's session menu, or only its folder picker. */
	let menu = $state<{
		id: string;
		folders: boolean;
		x: number;
		y: number;
		anchor: HTMLElement | null;
		returnFocus: HTMLElement | null;
		keyboard: boolean;
	} | null>(null);
	/** The menu's row, current after each poll; the menu goes when its row does. */
	const menuRow = $derived(menu ? (rows.find((r) => rowId(r) === menu!.id) ?? null) : null);

	function target(r: Row): SessionTarget {
		return r.kind === 'window' ? { kind: 'window', win: r.win } : r.kind === 'terminal' ? { kind: 'terminal', run: r.run } : { kind: 'saved', saved: r.saved };
	}

	/** Opens at the pointer, or under the element when opened from the keyboard. */
	function openMenu(id: string, e: MouseEvent, anchor: HTMLElement, returnFocus: HTMLElement | null, folders = false) {
		e.preventDefault();
		const keyboard = e.detail === 0 && e.button === 0;
		const at = !e.clientX && !e.clientY ? anchor.getBoundingClientRect() : null;
		const btn = e.type === 'click' ? (e.currentTarget as HTMLElement).getBoundingClientRect() : null;
		const box = btn ?? at;
		menu = { id, folders, x: box ? box.left : e.clientX, y: box ? box.bottom + 4 : e.clientY, anchor, returnFocus, keyboard };
	}

	/** The row's own open button, where focus returns. */
	const rowButton = (el: HTMLElement) => el.querySelector<HTMLElement>(':scope > button');

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
		if (r.kind === 'terminal') return r.run.renameTo || sessionLabel(r.run.session, r.run.agent, r.run.started);
		return r.saved.name;
	}

	function renameAction(r: Row): ((name: string) => Promise<boolean>) | null {
		if (r.kind === 'window' && r.named && !(r.win.attached && r.win.info.exited)) return (n) => app.renameWindow(r.win, n);
		if (r.kind === 'terminal' && r.named) return (n) => app.renameRunning(r.run, n);
		if (r.kind === 'saved') return (n) => app.renameSaved(r.saved, n);
		return null;
	}

	const open = (r: Row) => openTarget(target(r));

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
		class="mx-1 my-1.5 block min-w-0 flex-1 rounded-md border border-brand bg-surface-0 px-2 py-1.5 text-[13px] outline-none max-md:h-[44px] max-md:text-[16px]"
	/>
{/snippet}

{#snippet row(r: Row)}
	{@const rename = renameAction(r)}
	{@const name = title(r)}
	{@const id = rowId(r)}
	{@const ending = r.kind === 'window' ? r.win.busy : r.kind === 'terminal' && !!app.terminatingRunning[r.run.container]}
	{@const st = rowState(r)}
	{@const dim = st === 'ended' || st === 'inactive'}
	<div
		class={[
			'group relative flex items-center rounded-md hover:bg-surface-2 max-md:[-webkit-touch-callout:none] max-md:select-none',
			r.kind === 'window' && app.focusedId === r.win.id && 'bg-brand-soft'
		]}
		draggable={!phone && editing !== id && !ending && !(r.kind === 'terminal' && r.run.noTmux)}
		{@attach phone && editing !== id && longPress}
		ondragstart={(e) => {
			// Onto a folder it files the session; onto the desktop or a tab it opens it.
			if (r.named) e.dataTransfer?.setData(DRAG_TYPE, r.key);
			const drop: SidebarDrop =
				r.kind === 'window' ? { kind: 'window', id: r.win.id } : r.kind === 'terminal' ? { kind: 'terminal', container: r.run.container } : { kind: 'saved', saved: r.saved };
			e.dataTransfer?.setData(OPEN_DRAG, JSON.stringify(drop));
			if (e.dataTransfer) e.dataTransfer.effectAllowed = 'copyMove';
			app.sidebarDrag = drop;
		}}
		ondragend={() => {
			// Also after Escape or a drop outside the desktop.
			app.sidebarDrag = null;
			app.dropTab = null;
			app.clearDrop();
		}}
		oncontextmenu={(e) => {
			// The rename field keeps the browser's own menu, for paste.
			if (editing === id) return;
			const el = e.currentTarget as HTMLElement;
			if (ending) {
				e.preventDefault();
				return;
			}
			openMenu(id, e, el, rowButton(el));
		}}
		role="listitem"
	>
		{#if editing === id && rename}
			{@render nameInput(rename, name, 'New session name')}
		{:else}
			<button
				type="button"
				onclick={() => open(r)}
				disabled={ending || (r.kind === 'terminal' && r.run.noTmux)}
				title={ending
					? r.kind === 'window' && r.win.rebuilding
						? 'Rebuilding'
						: r.kind === 'window' && r.win.ssh
							? 'Disconnecting'
							: 'Terminating'
					: r.kind === 'terminal' && r.run.renameTo
					? `Renames to "${r.run.renameTo}" when it ends`
					: r.kind === 'terminal'
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
				class="flex min-h-11 min-w-0 flex-1 items-center gap-2.5 py-1.5 pr-1 pl-2 text-left disabled:cursor-not-allowed disabled:opacity-60 max-md:min-h-[52px]"
			>
				<StatusGlyph state={st} size={phone ? 14 : 12} />
				<span class="flex min-w-0 flex-col">
					<span class={['truncate text-[13px] max-md:text-[14px]', dim ? 'font-medium text-ink-2' : 'font-semibold']}>{name}</span>
					<span class={['truncate font-mono text-[10.5px] max-md:text-[12px]', dim ? 'text-ink-3' : 'text-ink-2']}>
						{#if r.kind === 'window' && r.win.ssh}
							ssh · {r.win.info.ssh ? sshTarget(r.win.info.ssh) : ''}{app.tabs.length > 1 ? ' · ' + (app.tabs.find((t) => t.id === r.win.tab)?.name ?? '') : ''}{r.win.info.exited
								? ' · ended'
								: r.win.min
									? ' · hidden'
									: ''}
						{:else if r.kind === 'window'}
							{r.win.info.agent} · {app.tilde(r.win.info.dir)}{app.tabs.length > 1 ? ' · ' + (app.tabs.find((t) => t.id === r.win.tab)?.name ?? '') : ''}{r.win.attached ? ' · terminal' : ''}{r.win.info.exited ? (r.win.attached ? ' · detached' : ' · ended') : r.win.min ? ' · hidden' : ''}
						{:else if r.kind === 'terminal'}
							{r.run.agent} · {app.tilde(r.run.dir)} · terminal
						{:else}
							{r.saved.agent} · {app.tilde(r.saved.dir)}
						{/if}
					</span>
				</span>
			</button>
			{#if ending}
				<span class="mr-2 shrink-0 text-ink-2"><Spinner size={12} label={r.kind === 'window' && r.win.rebuilding ? 'Rebuilding' : r.kind === 'window' && r.win.ssh ? 'Disconnecting' : 'Terminating'} /></span>
			{:else if r.kind === 'terminal'}
				<span class="mr-2 text-ink-3 group-hover:invisible max-md:!visible"><Icon name={r.run.noTmux ? 'lock' : 'terminal'} size={12} /></span>
			{:else if r.kind === 'window'}
				<span
					class="mr-2 grid h-[20px] min-w-[20px] shrink-0 place-items-center rounded-full bg-surface-2 px-[5px] font-mono text-[10.5px] text-ink-2 group-hover:invisible max-md:!visible max-md:h-[24px] max-md:min-w-[24px] max-md:text-[11px]"
					>{numbers.get(r.win.id)}</span
				>
			{/if}
			{#if phone && !ending}
				<!-- Touch has no hover: the row's menu is one tap away (or a long press). -->
				<IconButton
					icon="more"
					label={'More actions for ' + name}
					aria-haspopup="menu"
					onclick={(e) => openMenu(id, e, e.currentTarget.closest<HTMLElement>('[role=listitem]')!, e.currentTarget)}
				/>
			{/if}
			<!-- Row actions overlay the end of the row on hover, so titles keep their width. -->
			<span
				class={[
					'absolute top-1/2 right-1 -translate-y-1/2 items-center gap-0.5 rounded-md bg-surface-2 pl-1 max-md:!hidden',
					ending ? 'hidden' : ['group-focus-within:flex group-hover:flex', menu?.id === id ? 'flex' : 'hidden']
				]}
			>
				{#if rename}
					<IconButton icon="pencil" label={'Rename ' + name} small onclick={() => startEdit(id, name)} />
				{/if}
				{#if r.named}
					<IconButton
						icon="folderInput"
						label={'Move ' + name + ' to a folder'}
						small
						aria-haspopup="menu"
						onclick={(e) => openMenu(id, e, e.currentTarget.closest<HTMLElement>('[role=listitem]')!, e.currentTarget, true)}
					/>
				{/if}
				{#if r.kind === 'saved'}
					<IconButton icon="trash" label={'Forget saved session ' + name} small class="hover:text-warn" onclick={() => app.forget(r.saved)} />
				{/if}
				<IconButton
					icon="more"
					label={'More actions for ' + name}
					small
					aria-haspopup="menu"
					onclick={(e) => openMenu(id, e, e.currentTarget.closest<HTMLElement>('[role=listitem]')!, e.currentTarget)}
				/>
			</span>
		{/if}
	</div>
{/snippet}

{#snippet folderSection(f: Folder)}
	{@const inside = rows.filter((r) => folderOf.get(r.key) === f.id)}
	{@const visibleRows = [...inside.filter((r) => shown(r) && !isInactive(r)), ...inside.filter((r) => shown(r) && isInactive(r)).sort(byLastUsed)]}
	{@const hidden = inside.length - visibleRows.length}
	{@const open = !!needle || !app.collapsed[f.id]}
	{#if !needle || visibleRows.length}
		<div class="flex flex-col gap-0.5 rounded-md" ondragover={allowDrop} ondrop={(e) => dropOn(e, f.id)} role="list" aria-label={'Folder ' + f.name}>
			<div class="group flex items-center rounded-md hover:bg-surface-2">
				{#if editing === 'folder:' + f.id}
					{@render nameInput((n) => app.renameFolder(f.id, n), f.name, 'Folder name')}
				{:else}
					<button
						type="button"
						aria-expanded={open}
						disabled={!!needle}
						onclick={() => app.toggleFolder(f.id)}
						class="flex h-8 min-w-0 items-center gap-1.5 pl-1 text-left max-md:h-[44px] max-md:flex-1 max-md:gap-[8px]"
					>
						<span class="text-ink-3"><Icon name={open ? 'chevronDown' : 'chevronRight'} size={12} /></span>
						<span class="text-ink-2"><Icon name="folder" size={13} /></span>
						<span class="truncate text-[13px] font-semibold">{f.name}</span>
					</button>
					<span class="flex-1 max-md:hidden"></span>
					<span class="mr-2 font-mono text-[10.5px] text-ink-3 group-hover:hidden max-md:!inline max-md:text-[11px]">{visibleRows.length}</span>
					{#if phone}
						<IconButton
							icon="more"
							label={'Actions for folder ' + f.name}
							aria-haspopup="menu"
							onclick={(e) => openFolderMenu(f, e.currentTarget)}
						/>
					{/if}
					<span class="mr-1 hidden items-center gap-0.5 group-focus-within:flex group-hover:flex max-md:!hidden">
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
	{/if}
{/snippet}

{#if menu && menuRow}
	{#if menu.folders}
		<ContextMenu
			items={folderItems(menuRow.key)}
			x={menu.x}
			y={menu.y}
			anchor={menu.anchor}
			returnFocus={menu.returnFocus}
			keyboard={menu.keyboard}
			label={'Move ' + title(menuRow) + ' to a folder'}
			onclose={() => (menu = null)}
		/>
	{:else}
		{@const r = menuRow}
		{@const rename = renameAction(r)}
		<SessionMenu
			target={target(r)}
			x={menu.x}
			y={menu.y}
			anchor={menu.anchor}
			returnFocus={menu.returnFocus}
			keyboard={menu.keyboard}
			onrename={rename && (!app.sidebarCollapsed || phone) ? () => startEdit(rowId(r), title(r)) : undefined}
			onclose={() => (menu = null)}
		/>
	{/if}
{/if}

{#if folderMenu}
	<ContextMenu
		items={folderMenu.items}
		x={folderMenu.x}
		y={folderMenu.y}
		anchor={folderMenu.anchor}
		returnFocus={folderMenu.anchor}
		label={folderMenu.label}
		onclose={() => (folderMenu = null)}
	/>
{/if}

{#if app.sidebarCollapsed && !phone}
	<nav aria-label="Sessions" class="flex min-h-0 w-12 shrink-0 flex-col items-center gap-1 border-r border-line-1 bg-surface-1 py-2">
		<IconButton icon="panelOpen" label="Show the sidebar" onclick={() => app.toggleSidebar()} />
		<IconButton icon="plus" label="New session" onclick={() => (app.newSessionOpen = true)} />
		<div class="my-1 h-px w-6 bg-line-1"></div>
		<div class="flex min-h-0 flex-1 flex-col items-center gap-1 overflow-auto">
			{#each app.windows as w (w.id)}
				<button
					type="button"
					title={w.title + ' · ' + app.sessionDisplay(w.info) + ' · ' + (w.ssh && w.info.ssh ? sshTarget(w.info.ssh) : app.tilde(w.info.dir))}
					aria-label={'Show ' + w.title}
					onclick={() => {
						app.view = 'sessions';
						app.show(w);
					}}
					oncontextmenu={(e) => openMenu('w:' + w.id, e, e.currentTarget, e.currentTarget)}
					class={[
						'flex h-8 w-10 shrink-0 items-center justify-center gap-px rounded-full font-mono text-[11px] hover:bg-surface-2',
						app.focusedId === w.id ? 'bg-brand-soft text-ink-1' : 'text-ink-2'
					]}
				>
					<StatusGlyph state={winState(w)} size={10} />
					{numbers.get(w.id)}
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
<nav
	aria-label="Sessions"
	class={[
		'flex min-h-0 w-[272px] shrink-0 flex-col border-r border-line-1 bg-surface-1 max-md:w-full max-md:flex-1 max-md:border-r-0',
		phone && !(app.sessionsOpen && app.view === 'sessions') && 'hidden'
	]}
>
	<div class="flex h-12 shrink-0 items-center justify-between border-b border-line-1 pr-2 pl-3.5 max-md:h-[52px] max-md:pr-[4px]">
		<Brand />
		{#if phone}
			<button type="button" aria-label="Close sessions" onclick={() => (app.sessionsOpen = false)} class="grid size-[44px] shrink-0 place-items-center rounded-[6px] text-ink-1">
				<Icon name="x" size={18} />
			</button>
		{:else}
			<IconButton icon="panelClose" label="Collapse the sidebar" onclick={() => app.toggleSidebar()} />
		{/if}
	</div>

	<div class="shrink-0 px-2 pt-2 max-md:px-[12px] max-md:pt-[12px]">
		<Button variant="primary" icon="plus" class="w-full justify-center max-md:rounded-[8px]" onclick={() => (app.newSessionOpen = true)}>New session</Button>
	</div>
	<div class="shrink-0 px-2 pt-2 max-md:px-[12px] max-md:pt-[10px]">
		<div
			class="flex h-8 items-center gap-2 rounded-md border border-line-1 bg-inset pr-1 pl-2.5 text-ink-3 hover:border-line-2 focus-within:border-brand focus-within:bg-surface-0 max-md:h-[44px] max-md:rounded-[8px] max-md:border-line-2 max-md:pl-[12px] max-md:text-ink-2"
		>
			<Icon name="search" size={13} />
			<input
				type="search"
				bind:value={query}
				placeholder="Search sessions"
				aria-label="Search sessions"
				autocomplete="off"
				spellcheck="false"
				onkeydown={(e) => {
					if (e.key === 'Escape' && query) {
						e.preventDefault();
						query = '';
					}
				}}
				class="block h-full min-w-0 flex-1 bg-transparent text-[13px] text-ink-1 outline-none placeholder:text-ink-3 max-md:text-[16px] [&::-webkit-search-cancel-button]:hidden"
			/>
			{#if query}
				<IconButton icon="x" label="Clear search" small onclick={() => (query = '')} />
			{/if}
		</div>
	</div>
	<div class="flex flex-1 flex-col gap-0.5 overflow-auto p-2 max-md:px-[12px] max-md:pt-[10px]">
		{#if anyFolderMatch}
			<div class="flex items-center gap-2 px-2 pt-1 pb-1 max-md:py-0 max-md:pr-0 max-md:pl-[4px]">
				<SectionLabel>Folders</SectionLabel>
				<span class="flex-1"></span>
				{#if phone}<span class="font-mono text-[12px] text-ink-2">{runningCount} running</span>{/if}
				<IconButton icon="folderPlus" label="New folder" small onclick={() => startEdit('new-folder', '')} />
			</div>
		{/if}
		{#if editing === 'new-folder'}
			<div class="flex">{@render nameInput((n) => app.createFolder(n), '', 'New folder name')}</div>
		{/if}
		{#each folders as f (f.id)}
			{@render folderSection(f)}
		{/each}

		{#each workspaceGroups as g (g.name)}
			<div class="flex items-center gap-2 px-2 pt-3 pb-1" ondragover={allowDrop} ondrop={(e) => dropOn(e, null)} role="presentation">
				{#if g.name === SSH_GROUP}<AgentIcon name="ssh" small />{/if}
				<SectionLabel>{g.name === SSH_GROUP ? 'SSH' : g.name}</SectionLabel>
				{#if g.isDefault}<span class="rounded border border-line-2 bg-surface-2 px-1.5 font-mono text-[9.5px] font-bold text-ink-2">default</span>{/if}
				<span class="ml-auto font-mono text-[10.5px] text-ink-2 max-md:text-[11px]">{g.vault}</span>
			</div>
			<div class="flex flex-col gap-0.5" role="list" aria-label={g.name === SSH_GROUP ? 'SSH sessions' : 'Workspace ' + g.name} ondragover={allowDrop} ondrop={(e) => dropOn(e, null)}>
				{#each g.rows as r (rowId(r))}
					{@render row(r)}
				{/each}
				{#each [{ label: 'Last 7 days', list: g.week }, { label: 'More than 7 days ago', list: g.older }].filter((x) => x.list.length) as part (part.label)}
					<div class="flex items-center gap-2 px-2 pt-2 pb-0.5" role="presentation">
						<SectionLabel sub>{part.label}</SectionLabel>
						<span class="h-px flex-1 bg-line-1"></span>
					</div>
					{#each part.list as r (rowId(r))}
						{@render row(r)}
					{/each}
				{/each}
			</div>
		{:else}
			{#if needle && !rows.some(shown)}
				<div class="px-2 py-3 text-xs text-ink-3 max-md:text-[13px]">No sessions match.</div>
			{:else if !rows.some(shown)}
				<div class="px-2 py-3 text-xs text-ink-3 max-md:text-[13px]">
					Nothing running.{inactiveCount && !app.showInactive ? ` ${inactiveCount} inactive ${inactiveCount === 1 ? 'session is' : 'sessions are'} hidden.` : ''}
				</div>
			{/if}
		{/each}
	</div>

	<div class="flex shrink-0 flex-col gap-0.5 border-t border-line-1 p-2 max-md:gap-0 max-md:px-[12px] max-md:pt-[4px]">
		<button
			type="button"
			aria-pressed={app.showInactive}
			onclick={() => app.toggleInactive()}
			class="flex h-[34px] w-full items-center gap-2.5 rounded-md px-2 text-left text-[13px] text-ink-2 hover:bg-surface-2 hover:text-ink-1 max-md:h-[44px] max-md:gap-[12px] max-md:px-[4px] max-md:text-[14px]"
		>
			<span class={['grid size-4 shrink-0 place-items-center rounded border text-surface-0', app.showInactive ? 'border-brand bg-brand' : 'border-line-3 bg-inset']}>
				{#if app.showInactive}<Icon name="check" size={12} />{/if}
			</span>
			Show inactive sessions
			<span class="ml-auto font-mono text-[10.5px] max-md:text-[12px]">{inactiveCount}</span>
		</button>
		<div class="flex items-center">
			<button
				type="button"
				onclick={openSettings}
				class={[
					'flex h-[34px] w-full items-center gap-2.5 rounded-md px-2 text-left text-[13px] max-md:h-[44px] max-md:min-w-0 max-md:flex-1 max-md:gap-[12px] max-md:px-[4px] max-md:text-[14px] max-md:text-ink-1',
					app.view === 'settings' ? 'bg-surface-3 text-ink-1' : 'text-ink-2 hover:bg-surface-2 hover:text-ink-1'
				]}
			>
				<Icon name="settings" /> Settings
			</button>
			{#if phone}
				<!-- The desktop keeps these in the toolbar. -->
				<IconButton icon="help" label="Help" onclick={() => (app.helpOpen = true)} />
				<IconButton icon={app.theme === 'dark' ? 'sun' : 'moon'} label={app.theme === 'dark' ? 'Switch to light' : 'Switch to dark'} onclick={() => app.toggleTheme()} />
			{/if}
		</div>
		{#if phone && app.data?.authEnabled}
			<div class="flex min-h-[44px] items-center gap-[12px] px-[4px]">
				<span class="text-ink-2"><Icon name="user" /></span>
				<span class="min-w-0 flex-1 truncate font-mono text-[13px]">{user}</span>
				<Button variant="ghost" icon="logout" onclick={() => app.signOut()}>Sign out</Button>
			</div>
		{/if}
		<div class="flex items-center gap-2 px-2 py-1.5 font-mono text-[10.5px] text-ink-2 max-md:px-[4px] max-md:text-[12px]">
			<Icon name="shield" size={12} />
			<span>{app.data?.runtime || 'no container runtime'} · firewall {app.data?.defaults.noFirewall ? 'off by default' : 'on'}</span>
		</div>
	</div>
</nav>
{/if}
