// App state shared by the components. The app is client-only (ssr = false),
// so one module-level instance is safe.
import { api, ApiError, loadLayout, local, sessionKey, setLocal, setUnauthorizedHandler } from './api';
import type { Folder, Me, RunningDoc, RunOptions, SavedDoc, SessionInfo, SettingsDoc, SSHOptions, State } from './api';
// Also applies the interface font from this browser's settings.
import { termFont } from './termFont.svelte';
import { applyZone, DEFAULTS, edgeAt, fromRects, has, insertAuto, layout, moveEdge, prune, remove, rename, sanitize, zoneAt as zoneUnder, type Node, type Rect, type Shown, type Zone } from './tiling';

export { MIN_H, MIN_W } from './tiling';
export type { Rect, Zone } from './tiling';

interface Geometry {
	x: number;
	y: number;
	w: number;
	h: number;
	max?: boolean;
	min?: boolean;
	tab?: string;
}

/** A host prompt of a session (exitbox-allow, vault) waiting for an answer. */
export interface HostPrompt {
	id: string;
	// A newer server may send kinds this page does not know.
	kind: 'allow' | 'vault-read' | 'vault-write' | 'vault-list' | 'vault-password' | (string & {});
	agent: string;
	workspace: string;
	subject: string;
	/** Answered and waiting for the server's prompt-done or prompt-error. */
	sending?: boolean;
	/** Why the last answer did not go through. */
	error?: string;
}

/** One session window on the desktop. */
export class Win {
	info = $state.raw<SessionInfo>() as SessionInfo;
	x = $state(16);
	y = $state(16);
	w = $state(720);
	h = $state(460);
	max = $state(false);
	min = $state(false);
	z = $state(0);
	connected = $state(false);
	/** The tab the window lives in. */
	tab = $state('');
	/** Host prompts waiting for an answer, oldest first. */
	prompts = $state<HostPrompt[]>([]);
	/** Answers a prompt over the window's connection; false when it could not be sent. */
	answer: ((id: string, allow: boolean, password?: string) => boolean) | null = null;
	/** Set while a terminate request is in flight. */
	terminating = $state(false);
	/** Set while a rebuild request is in flight. */
	rebuilding = $state(false);
	/** The terminal's last fitted size, for restarts from a menu. */
	cols = 120;
	rows = 36;
	/** Writes keys to the session as typed in its terminal; false when they could not be sent. */
	input: ((data: string) => boolean) | null = null;
	/** The session asked for application cursor keys (arrows send ESC O A). */
	appCursor: () => boolean = () => false;

	constructor(info: SessionInfo) {
		this.info = info;
	}

	get id() {
		return this.info.id;
	}

	get title() {
		return this.info.renameTo || sessionLabel(this.info.name, this.info.agent || this.info.kind, this.info.started);
	}

	get attached() {
		return this.info.kind === 'attach';
	}

	get ssh() {
		return this.info.kind === 'ssh';
	}

	/** Only a run started here knows how to start again with a new image. */
	get rebuildable() {
		return this.info.kind === 'run' && !!this.info.request;
	}

	/** A terminate or rebuild is in flight: the window takes no actions. */
	get busy() {
		return this.terminating || this.rebuilding;
	}

	place(x: number, y: number, w: number, h: number) {
		this.x = Math.round(x);
		this.y = Math.round(y);
		this.w = Math.round(w);
		this.h = Math.round(h);
		this.clamp();
	}

	/** Sets a tile exactly: tiles never overlap, whatever their size. */
	tile(x: number, y: number, w: number, h: number) {
		this.x = Math.round(x);
		this.y = Math.round(y);
		this.w = Math.max(1, Math.round(w));
		this.h = Math.max(1, Math.round(h));
	}

	/** Keeps a floating window inside the desktop. */
	clamp() {
		const dw = app.desk.w;
		const dh = app.desk.h;
		if (!dw || !dh) return;
		this.w = Math.max(Math.min(DEFAULTS.minW, dw), Math.min(this.w, dw));
		this.h = Math.max(Math.min(DEFAULTS.minH, dh), Math.min(this.h, dh));
		this.x = Math.min(Math.max(this.x, 0), dw - this.w);
		this.y = Math.min(Math.max(this.y, 0), dh - this.h);
	}

	save() {
		setLocal('win.' + this.id, { x: this.x, y: this.y, w: this.w, h: this.h, max: this.max, min: this.min, tab: this.tab });
	}

	restore(index: number) {
		const g = local<Geometry>('win.' + this.id);
		if (g && typeof g.x === 'number') {
			this.place(g.x, g.y, g.w, g.h);
			this.max = !!g.max;
			this.min = !!g.min;
			if (g.tab) this.tab = g.tab;
			return;
		}
		const size = defaultWindowSize();
		const n = index % 8;
		this.place(16 + n * 28, 16 + n * 28, size.w, size.h);
	}
}

/** A window's state for its status pill. */
export function sessionStatus(w: Win): { text: string; tone: 'run' | 'wait' | 'off' | 'bad' } {
	const info = w.info;
	if (info.exited && w.attached) return { text: 'detached', tone: 'off' };
	if (info.exited) return info.exitCode === 0 ? { text: 'ended', tone: 'off' } : { text: 'exit ' + info.exitCode, tone: 'bad' };
	if (!w.connected) return { text: 'connecting', tone: 'wait' };
	return w.attached ? { text: 'terminal', tone: 'run' } : w.ssh ? { text: 'connected', tone: 'run' } : { text: 'running', tone: 'run' };
}

/** A window's sidebar key; an unnamed run is keyed by window until it learns its name. */
export function windowKey(i: SessionInfo) {
	// SSH names need not be unique and nothing is saved under them: key by id.
	if (i.kind === 'ssh') return sessionKey('', 'ssh', '', i.id);
	return i.name ? sessionKey(i.workspace || 'default', i.agent, i.dir, i.name) : 'window:' + i.id;
}

/** A session's name, or the agent and start time while it has none. */
export function sessionLabel(name: string, agent: string, started: string) {
	if (name) return name;
	const t = new Date(started);
	return agent + ' ' + t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export function defaultWindowSize() {
	const dw = app.desk.w || 1200;
	const dh = app.desk.h || 700;
	return {
		w: Math.min(Math.max(560, dw * 0.6), dw - 20),
		h: Math.min(Math.max(360, dh * 0.7), dh - 20)
	};
}

export interface Choice {
	label: string;
	value: string;
	tone: 'primary' | 'secondary' | 'danger';
}

interface Prompt {
	title: string;
	text: string;
	choices: Choice[];
	resolve: (v: string | null) => void;
}

/** What names a saved session to the server, which rejects unknown fields such as lastUsed. */
const savedRef = ({ workspace, agent, dir, name }: SavedDoc) => ({ workspace, agent, dir, name });

export type View = 'loading' | 'login' | 'sessions' | 'settings' | 'offline';

export type DockLayout = 'stack' | 'columns' | 'grid';

/** The drag type of a session dragged from the sidebar to be opened. */
export const OPEN_DRAG = 'application/x-exitbox-open';

/** What a sidebar row carries when dragged onto the desktop or a tab. */
export type SidebarDrop =
	| { kind: 'window'; id: string }
	| { kind: 'terminal'; container: string }
	| { kind: 'saved'; saved: SavedDoc };

/** Reads an OPEN_DRAG payload, or null. */
export function readDrop(e: DragEvent): SidebarDrop | null {
	const raw = e.dataTransfer?.getData(OPEN_DRAG);
	if (!raw) return null;
	try {
		return JSON.parse(raw) as SidebarDrop;
	} catch {
		return null;
	}
}

export interface Tab {
	id: string;
	name: string;
}

/** Stands in for a dropped session whose window does not exist yet. */
const NEW_WINDOW = '\u0000new';

/** A drop worked out before it happens: a split tree for the tab, or a new dock order for a preset. */
type DropPlan = { tab: string; tree: Node } | { tab: string; order: string[] };

/** Split trees from storage, for tabs that still exist; never throws, a tree it cannot read is dropped. */
function loadTrees(tabs: Tab[]): Record<string, Node> {
	const out: Record<string, Node> = {};
	try {
		const raw = local<Record<string, unknown>>('tileTrees');
		if (!raw || typeof raw !== 'object') return out;
		for (const t of tabs) {
			try {
				const n = sanitize(raw[t.id]);
				if (n) out[t.id] = n;
			} catch {
				// That tab follows the preset.
			}
		}
	} catch {
		// Every tab follows the preset.
	}
	return out;
}

class App {
	me = $state.raw<Me | null>(null);
	data = $state.raw<State | null>(null);
	view = $state<View>('loading');
	loginMessage = $state('');
	offlineReason = $state('');
	windows = $state<Win[]>([]);
	focusedId = $state<string | null>(null);
	desk = $state({ w: 0, h: 0 });
	theme = $state<'dark' | 'light'>('dark');
	toast = $state<{ text: string; bad: boolean } | null>(null);
	prompt = $state.raw<Prompt | null>(null);
	newSessionOpen = $state(false);
	helpOpen = $state(false);
	/** On a phone, the sessions list covers the session screen. */
	sessionsOpen = $state(false);
	/** The window whose session settings are open. */
	sessionSettings = $state.raw<Win | null>(null);
	settingsSection = $state('general');
	/** Auto-dock tiles every visible window with dockLayout; off (Free), all windows float. */
	autoDock = $state(local<boolean>('autoDock') ?? true);
	dockLayout = $state<DockLayout>(local<DockLayout>('dockLayout') ?? 'grid');
	/** Relative column widths, by window id (1 when unset). */
	private colWeights: Record<string, number> = local<Record<string, number>>('colWeights') ?? {};
	/** Relative row heights of the grid, by row number (1 when unset). */
	private rowWeights: number[] = local<number[]>('rowWeights') ?? [];
	/** Relative heights of the stacked windows in the stack layout. */
	private stackWeights: Record<string, number> = local<Record<string, number>>('stackWeights') ?? {};
	/** Share of the width the main window takes in the stack layout. */
	private mainRatio: number = local<number>('mainRatio') ?? 0.56;
	/** Window ids in dock order; the first is the main window of the stack layout. */
	private dockOrder: string[] = local<string[]>('dockOrder') ?? [];
	/** Where a dragged window would land, in desktop pixels. */
	dropPreview = $state<Rect | null>(null);
	/** The sidebar row being dragged: drag events only reveal the payload on drop. */
	sidebarDrag: SidebarDrop | null = null;
	private planned: { key: string; plan: DropPlan } | null = null;
	/** Tabs are separate desktops, each tiling its own windows. */
	tabs = $state<Tab[]>(local<Tab[]>('tabs')?.length ? local<Tab[]>('tabs')! : [{ id: 'main', name: 'Main' }]);
	activeTab = $state(local<string>('activeTab') ?? 'main');
	/** Split trees of tabs laid out by hand; a tab without one follows the preset. */
	trees = $state.raw<Record<string, Node>>(loadTrees(this.tabs));
	/** The active tab has its own arrangement instead of the preset. */
	custom = $derived(this.autoDock && !!this.trees[this.activeTab]);
	/** False until the first state arrives: trees keep ids of windows not added yet. */
	private synced = false;
	/** The tab a dragged window would move to. */
	dropTab = $state<string | null>(null);

	/** The sidebar shrinks to a rail of window buttons. */
	sidebarCollapsed = $state(local<boolean>('sidebarCollapsed') ?? false);

	toggleSidebar() {
		this.sidebarCollapsed = !this.sidebarCollapsed;
		setLocal('sidebarCollapsed', this.sidebarCollapsed);
	}

	/** Saved sessions that do not run are hidden unless this is on. */
	showInactive = $state(local<boolean>('showInactive') ?? false);
	settings = $state<SettingsDoc | null>(null);
	settingsOrig = '';
	/** Days a saved session shows as recently ended. */
	recentDays = $derived(this.data?.recentDays ?? 3);

	private zTop = 10;
	private poll: ReturnType<typeof setInterval> | null = null;
	/** The last state request started, and the last one applied. */
	private pollSeq = 0;
	private appliedSeq = 0;
	/** Responses to requests up to this one predate a local window change. */
	private changedAt = 0;
	private pollFailures = 0;
	/** Sessions that never get a window: attached only to be terminated. */
	private noWindow = new Set<string>();
	private retryTimer: ReturnType<typeof setTimeout> | null = null;
	private booting = false;
	private toastTimer: ReturnType<typeof setTimeout> | null = null;

	focused = $derived(this.windows.find((w) => w.id === this.focusedId) ?? null);
	running = $derived(this.windows.filter((w) => !w.info.exited).length);
	/** The window a phone shows: the focused one when it is in the active tab and not hidden, else the tab's top window. */
	shown = $derived.by(() => {
		const f = this.focused;
		if (f && f.tab === this.activeTab && !f.min) return f;
		return this.windows.filter((w) => w.tab === this.activeTab && !w.min).sort((a, b) => b.z - a.z)[0] ?? null;
	});

	constructor() {
		setUnauthorizedHandler(() => this.showLogin());
	}

	// ---------- boot and polling ----------

	async boot() {
		if (this.booting) return;
		this.booting = true;
		try {
			await this.bootOnce();
		} finally {
			this.booting = false;
		}
	}

	/** Boots again now, from the offline view. */
	retry() {
		if (this.retryTimer) clearTimeout(this.retryTimer);
		this.retryTimer = null;
		void this.boot();
	}

	/** Shows the offline view and boots again shortly, so the page recovers when the server is back. */
	private goOffline(e: unknown) {
		this.pollFailures = 0;
		this.stopPolling();
		this.offlineReason = e instanceof Error ? e.message : String(e);
		this.view = 'offline';
		if (this.retryTimer) clearTimeout(this.retryTimer);
		this.retryTimer = setTimeout(() => this.retry(), 3000);
	}

	private async bootOnce() {
		this.synced = false;
		this.applyTheme();
		let me: Me;
		try {
			me = await api<Me>('GET', '/api/me');
		} catch (e) {
			this.goOffline(e);
			return;
		}
		this.me = me;
		if (me.authEnabled && !me.signedIn) {
			this.showLogin();
			return;
		}
		// xterm measures the cell size once, so the font has to be there first.
		await termFont.load();
		try {
			await loadLayout();
		} catch {
			// The localStorage copy of the layout serves meanwhile.
		}
		this.applyLayout();
		try {
			const seq = ++this.pollSeq;
			this.data = await api<State>('GET', '/api/state');
			this.appliedSeq = seq;
			this.pollFailures = 0;
		} catch (e) {
			// A 401 has already shown the login.
			if (!(e instanceof ApiError && e.status === 401)) this.goOffline(e);
			return;
		}
		this.view = 'sessions';
		this.sync(this.data.sessions);
		this.forgetDock(this.windows.map((w) => w.id), false);
		this.synced = true;
		this.retile();
		this.currentTab();
		this.focusTop();
		this.startPolling();
	}

	async refresh() {
		const seq = ++this.pollSeq;
		let data: State;
		try {
			data = await api<State>('GET', '/api/state');
		} catch (e) {
			// A 401 has already shown the login.
			if (e instanceof ApiError && e.status === 401) return;
			if (seq > this.appliedSeq && (this.view === 'sessions' || this.view === 'settings') && ++this.pollFailures >= 3) this.goOffline(e);
			return;
		}
		this.pollFailures = 0;
		// Out of order, or started before a window was added or removed here: a newer poll follows.
		if (seq <= this.appliedSeq || seq <= this.changedAt) return;
		this.appliedSeq = seq;
		this.data = data;
		this.sync(data.sessions);
	}

	/** Marks a local window change, so polls already in flight cannot undo it. */
	private changed() {
		this.changedAt = this.pollSeq;
	}

	/** Adds windows for new sessions and drops windows of forgotten ones. */
	private sync(sessions: SessionInfo[]) {
		const live = new Set(sessions.map((s) => s.id));
		for (const info of sessions) {
			if (this.noWindow.has(info.id)) continue;
			const w = this.windows.find((x) => x.id === info.id);
			if (w) w.info = info;
			else this.addWindow(info, false);
			if (info.id in this.pendingFolder && (info.name || info.exited)) this.filePending(info);
		}
		for (const w of this.windows.filter((x) => !live.has(x.id))) this.dropWindow(w, false);
	}

	private startPolling() {
		this.stopPolling();
		this.poll = setInterval(() => this.refresh(), 4000);
	}

	private stopPolling() {
		if (this.poll) clearInterval(this.poll);
		this.poll = null;
	}

	showLogin(message = '') {
		this.stopPolling();
		this.synced = false;
		this.windows = [];
		this.focusedId = null;
		this.settings = null;
		this.newSessionOpen = false;
		this.sessionSettings = null;
		this.loginMessage = message;
		this.view = 'login';
	}

	async login(username: string, password: string) {
		await api('POST', '/api/login', { username, password });
		this.view = 'loading';
		await this.boot();
	}

	async signOut() {
		try {
			await api('POST', '/api/logout', {});
		} catch {
			// Signed out anyway.
		}
		this.showLogin('Signed out. Your sessions keep running.');
	}

	// ---------- theme ----------

	applyTheme() {
		const pref = local<'dark' | 'light'>('theme');
		const dark = pref ? pref === 'dark' : !matchMedia('(prefers-color-scheme: light)').matches;
		this.theme = dark ? 'dark' : 'light';
		document.documentElement.dataset.theme = this.theme;
		// The phone browser bar takes the colour of the bars below it.
		const bar = getComputedStyle(document.documentElement).getPropertyValue('--x-surface-1').trim();
		if (bar) document.querySelector('meta[name="theme-color"]')?.setAttribute('content', bar);
	}

	toggleTheme() {
		setLocal('theme', this.theme === 'dark' ? 'light' : 'dark');
		this.applyTheme();
	}

	// ---------- windows ----------

	/** user: opened here, so the layout change is saved; a poll's stays in memory (another browser may own the layout). */
	private addWindow(info: SessionInfo, user: boolean) {
		const w = new Win(info);
		w.restore(this.windows.length);
		if (!this.tabs.some((t) => t.id === w.tab)) {
			w.tab = this.currentTab();
			w.save();
		}
		w.z = ++this.zTop;
		this.windows.push(w);
		if (!this.dockOrder.includes(w.id)) {
			this.dockOrder.push(w.id);
			if (user) setLocal('dockOrder', this.dockOrder);
		}
		this.retile(user);
		return w;
	}

	private dropWindow(w: Win, user: boolean) {
		setLocal('win.' + w.id, null);
		this.windows = this.windows.filter((x) => x.id !== w.id);
		if (this.focusedId === w.id) this.focusedId = null;
		if (this.sessionSettings === w) this.sessionSettings = null;
		this.forgetDock(
			this.windows.map((x) => x.id),
			user
		);
		this.retile(user);
	}

	/** Drops dock order, column widths and tree leaves of windows not in keep; saved only when persist. */
	private forgetDock(keep: string[], persist: boolean) {
		this.dockOrder = this.dockOrder.filter((id) => keep.includes(id));
		for (const store of [this.colWeights, this.stackWeights]) {
			for (const id of Object.keys(store)) {
				if (!keep.includes(id)) delete store[id];
			}
		}
		if (persist) {
			setLocal('dockOrder', this.dockOrder);
			setLocal('colWeights', this.colWeights);
			setLocal('stackWeights', this.stackWeights);
		}
		const trees: Record<string, Node> = {};
		for (const [tab, t] of Object.entries(this.trees)) {
			const n = this.tabs.some((x) => x.id === tab) ? prune(t, (id) => keep.includes(id)) : null;
			if (n) trees[tab] = n;
		}
		if (JSON.stringify(trees) !== JSON.stringify(this.trees)) this.setTrees(trees, persist);
	}

	/**
	 * The layout is one document on the server: a change this browser did not
	 * cause (a poll) stays in memory, so an idle browser never overwrites trees
	 * another one arranged; the next change made here saves it.
	 */
	private setTrees(trees: Record<string, Node>, persist = true) {
		this.trees = trees;
		if (persist) setLocal('tileTrees', trees);
	}

	private setTree(tab: string, t: Node | null, persist = true) {
		const next = { ...this.trees };
		if (t) next[tab] = t;
		else delete next[tab];
		this.setTrees(next, persist);
	}

	openWindow(info: SessionInfo) {
		let w = this.windows.find((x) => x.id === info.id);
		if (!w) {
			this.changed();
			w = this.addWindow(info, true);
		}
		this.view = 'sessions';
		this.sessionsOpen = false;
		this.show(w);
		return w;
	}

	focus(w: Win) {
		this.focusedId = w.id;
		if (w.z !== this.zTop) w.z = ++this.zTop;
	}

	show(w: Win) {
		this.sessionsOpen = false;
		if (w.tab !== this.activeTab) this.setTab(w.tab);
		if (w.min) {
			w.min = false;
			w.save();
			this.retile(true);
		}
		this.focus(w);
	}

	hide(w: Win) {
		w.min = true;
		w.save();
		this.retile(true);
		if (this.focusedId === w.id) {
			this.focusTop();
		}
	}

	/** Focuses the topmost visible window of the current tab. */
	private focusTop() {
		const next = this.windows.filter((x) => !x.min && x.tab === this.activeTab).sort((a, b) => b.z - a.z)[0];
		if (next) this.focus(next);
		else this.focusedId = null;
	}

	/** Takes the tabs and tiling from the loaded layout. */
	private applyLayout() {
		const tabs = local<Tab[]>('tabs');
		if (tabs?.length) this.tabs = tabs;
		this.activeTab = local<string>('activeTab') ?? this.tabs[0].id;
		this.autoDock = local<boolean>('autoDock') ?? true;
		this.dockLayout = local<DockLayout>('dockLayout') ?? 'grid';
		this.dockOrder = local<string[]>('dockOrder') ?? [];
		this.colWeights = local<Record<string, number>>('colWeights') ?? {};
		this.rowWeights = local<number[]>('rowWeights') ?? [];
		this.stackWeights = local<Record<string, number>>('stackWeights') ?? {};
		this.mainRatio = local<number>('mainRatio') ?? 0.56;
		this.trees = loadTrees(this.tabs);
		// Save once, so a layout kept only in this browser reaches the server.
		this.saveTabs();
		setLocal('autoDock', this.autoDock);
		setLocal('dockLayout', this.dockLayout);
	}

	// ---------- tabs ----------

	/** The active tab, repaired when it no longer exists. */
	private currentTab() {
		if (!this.tabs.some((t) => t.id === this.activeTab)) this.activeTab = this.tabs[0].id;
		return this.activeTab;
	}

	private saveTabs() {
		setLocal('tabs', this.tabs);
		setLocal('activeTab', this.activeTab);
	}

	setTab(id: string) {
		if (!this.tabs.some((t) => t.id === id)) return;
		this.activeTab = id;
		this.saveTabs();
		this.view = 'sessions';
		this.retile();
		this.focusTop();
	}

	addTab() {
		const id = Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
		let n = this.tabs.length + 1;
		while (this.tabs.some((t) => t.name === 'Tab ' + n)) n++;
		this.tabs.push({ id, name: 'Tab ' + n });
		this.setTab(id);
		return id;
	}

	/** Moves a tab to index to (counted before the move). */
	moveTab(id: string, to: number) {
		const from = this.tabs.findIndex((t) => t.id === id);
		if (from < 0) return;
		const dest = Math.max(0, Math.min(this.tabs.length - 1, to > from ? to - 1 : to));
		if (dest === from) return;
		const [t] = this.tabs.splice(from, 1);
		this.tabs.splice(dest, 0, t);
		this.saveTabs();
	}

	renameTab(id: string, name: string) {
		const t = this.tabs.find((x) => x.id === id);
		if (!t || !name.trim()) return;
		t.name = name.trim().slice(0, 40);
		this.saveTabs();
	}

	/** Closes a tab; its windows move to the tab before it (or after, for the first). */
	closeTab(id: string) {
		if (this.tabs.length < 2) return;
		const i = this.tabs.findIndex((t) => t.id === id);
		if (i < 0) return;
		const to = this.tabs[i === 0 ? 1 : i - 1].id;
		for (const w of this.windows) {
			if (w.tab === id) {
				w.tab = to;
				w.save();
			}
		}
		this.tabs.splice(i, 1);
		// Its windows get default places in the tab they move to.
		this.setTree(id, null);
		if (this.activeTab === id) this.activeTab = to;
		this.saveTabs();
		this.retile(true);
		this.focusTop();
	}

	/** Moves a window to another tab; the current tab closes the gap. */
	moveToTab(w: Win, id: string) {
		if (w.tab === id || !this.tabs.some((t) => t.id === id)) return;
		const from = this.trees[w.tab];
		if (from) this.setTree(w.tab, remove(from, w.id));
		w.tab = id;
		w.max = false;
		w.save();
		this.retile(true);
		if (this.focusedId === w.id) this.focusTop();
	}

	tabWindows(id: string) {
		return this.windows.filter((w) => w.tab === id);
	}

	toggleMax(w: Win, force?: boolean) {
		w.max = force ?? !w.max;
		w.save();
		this.retile(true);
	}

	async stop(w: Win) {
		try {
			await api('DELETE', '/api/sessions/' + encodeURIComponent(w.id));
		} catch (e) {
			if (!(e instanceof ApiError && e.status === 404)) {
				this.notify((e as Error).message, true);
				return;
			}
		}
		this.unfileSSH(w);
		this.changed();
		this.dropWindow(w, true);
		this.refresh();
	}

	/** Ends a session for good, also when it was started in a terminal. */
	async terminate(w: Win) {
		if (w.terminating) return;
		w.terminating = true;
		try {
			await api('POST', '/api/sessions/' + encodeURIComponent(w.id) + '/terminate', {});
		} catch (e) {
			if (!(e instanceof ApiError && e.status === 404)) {
				w.terminating = false;
				this.notify((e as Error).message, true);
				return;
			}
		}
		this.unfileSSH(w);
		this.changed();
		this.dropWindow(w, true);
		this.refresh();
	}

	async restart(w: Win, cols: number, rows: number) {
		try {
			await this.restartWith(w, cols, rows);
		} catch (e) {
			this.notify((e as Error).message, true);
		}
	}

	/** An SSH session is gone once its window closes: so is its folder entry. */
	private unfileSSH(w: Win) {
		const key = windowKey(w.info);
		if (w.ssh && this.data?.folders.some((f) => f.items.includes(key))) void this.moveToFolder(key, null);
	}

	/** Rebuilds the agent image and starts the session again in the same conversation. */
	async rebuild(w: Win) {
		if (w.busy) return;
		w.rebuilding = true;
		try {
			await this.restartWith(w, w.cols, w.rows, { rebuild: true });
		} catch (e) {
			this.notify((e as Error).message, true);
		} finally {
			w.rebuilding = false;
		}
	}

	/** Starts a window's session again, with new options when given (ssh for an SSH session); the new window takes its place. */
	async restartWith(w: Win, cols: number, rows: number, opts?: { request?: RunOptions; ssh?: SSHOptions; rebuild?: boolean }) {
		const info = await api<SessionInfo>('POST', '/api/sessions/' + encodeURIComponent(w.id) + '/restart', { cols, rows, ...opts });
		// An SSH session's folder key is its id: carry the folder to the new one.
		const oldKey = windowKey(w.info);
		const folder = w.ssh ? this.data?.folders.find((f) => f.items.includes(oldKey)) : undefined;
		if (folder) await this.saveFolders((this.data?.folders ?? []).map((f) => ({ ...f, items: f.items.map((k) => (k === oldKey ? windowKey(info) : k)) })));
		setLocal('win.' + info.id, { x: w.x, y: w.y, w: w.w, h: w.h, max: w.max, min: false, tab: w.tab });
		// The new window takes the old one's dock slot, also when a poll already added it somewhere else.
		this.dockOrder = this.dockOrder.filter((id) => id === w.id || id !== info.id).map((id) => (id === w.id ? info.id : id));
		for (const store of [this.colWeights, this.stackWeights]) {
			if (store[w.id]) store[info.id] = store[w.id];
		}
		setLocal('dockOrder', this.dockOrder);
		const trees: Record<string, Node> = {};
		for (const [tab, t] of Object.entries(this.trees)) {
			const n = has(t, w.id) ? rename(t, w.id, info.id) : remove(t, info.id);
			if (n) trees[tab] = n;
		}
		this.setTrees(trees);
		this.windows.find((x) => x.id === info.id)?.restore(0);
		// Open the new window before dropping the old one: dropping prunes dock entries of windows that do not exist yet.
		this.openWindow(info);
		this.changed();
		this.dropWindow(w, true);
		this.refresh();
		return info;
	}

	ended(w: Win, code: number) {
		w.info = { ...w.info, exited: true, exitCode: code };
		this.refresh();
	}

	// ---------- docking ----------

	/** Picks a dock layout; null is Free, where windows float. A preset also resets the active tab's own arrangement. */
	setDock(layout: DockLayout | null) {
		this.autoDock = layout !== null;
		setLocal('autoDock', this.autoDock);
		if (layout) {
			this.dockLayout = layout;
			setLocal('dockLayout', layout);
			this.setTree(this.activeTab, null);
			for (const w of this.windows) w.max = false;
		}
		this.retile(true);
		for (const w of this.windows) w.save();
	}

	/** The windows the layout tiles: the visible ones of the active tab that are not maximized. */
	tiledWindows() {
		const rank = new Map(this.dockOrder.map((id, i) => [id, i]));
		return this.windows
			.filter((w) => !w.min && !w.max && w.tab === this.activeTab)
			.sort((a, b) => (rank.get(a.id) ?? 1e9) - (rank.get(b.id) ?? 1e9));
	}

	/** Window ids in rows: one row for columns, rows of the grid. */
	private gridRows(ids: string[]): string[][] {
		if (this.dockLayout === 'columns') return [ids];
		const cols = Math.ceil(Math.sqrt(ids.length));
		const rows: string[][] = [];
		for (let i = 0; i < ids.length; i += cols) rows.push(ids.slice(i, i + cols));
		return rows;
	}

	/** The tiles of the preset layout for windows in this order. */
	private presetRects(ids: string[]): Map<string, Rect> {
		const out = new Map<string, Rect>();
		const { w: dw, h: dh } = this.desk;
		const gap = DEFAULTS.gap;
		if (!ids.length || !dw || !dh) return out;
		// Lays out sizes proportional to weights along one axis.
		const spans = (room: number, weights: number[]) => {
			const total = weights.reduce((a, b) => a + b, 0);
			return weights.map((x) => (room * x) / total);
		};
		if (ids.length === 1) {
			out.set(ids[0], { x: gap, y: gap, w: dw - 2 * gap, h: dh - 2 * gap });
			return out;
		}
		if (this.dockLayout === 'stack') {
			const [main, ...rest] = ids;
			const lw = Math.round((dw - 3 * gap) * this.mainRatio);
			out.set(main, { x: gap, y: gap, w: lw, h: dh - 2 * gap });
			const hs = spans(dh - gap * (rest.length + 1), rest.map((id) => this.stackWeights[id] ?? 1));
			let y = gap;
			rest.forEach((id, i) => {
				out.set(id, { x: 2 * gap + lw, y, w: dw - 3 * gap - lw, h: hs[i] });
				y += hs[i] + gap;
			});
			return out;
		}
		// Columns is a grid of one row.
		const rows = this.gridRows(ids);
		const hs = spans(dh - gap * (rows.length + 1), rows.map((_, k) => this.rowWeights[k] ?? 1));
		let y = gap;
		rows.forEach((row, k) => {
			const ws = spans(dw - gap * (row.length + 1), row.map((id) => this.colWeights[id] ?? 1));
			let x = gap;
			row.forEach((id, i) => {
				out.set(id, { x, y, w: ws[i], h: hs[k] });
				x += ws[i] + gap;
			});
			y += hs[k] + gap;
		});
		return out;
	}

	/** The leaves of the active tab's tree on screen: hidden and maximized windows keep a leaf but no tile. */
	private tiledShown(extra?: string): Shown {
		const ids = new Set(this.tiledWindows().map((w) => w.id));
		if (extra) ids.add(extra);
		return (id) => ids.has(id);
	}

	/** The tiles of the active tab as laid out now. */
	private tileRects(): Map<string, Rect> {
		const tree = this.trees[this.activeTab];
		return tree ? layout(tree, this.desk, DEFAULTS, this.tiledShown()) : this.presetRects(this.tiledWindows().map((w) => w.id));
	}

	/** The drop zone under a desktop point; self is the dragged window, if it has one. */
	zoneAt(x: number, y: number, self: string | null): Zone | null {
		if (!this.autoDock || !this.desk.w || !this.desk.h) return null;
		return zoneUnder({ x, y }, this.desk, this.tileRects(), self);
	}

	/** Works out a drop of window id (NEW_WINDOW for a session still to open) on the active tab. */
	private planDrop(id: string, zone: Zone): DropPlan {
		const tab = this.activeTab;
		const ids = this.tiledWindows().map((w) => w.id);
		const tree = this.trees[tab];
		if (!tree && zone.kind === 'swap') {
			// A preset tab keeps swapping by dock order.
			const a = ids.indexOf(id);
			const b = ids.indexOf(zone.target);
			const order = this.dockOrder.filter((x) => x !== id);
			const at = order.indexOf(zone.target);
			// Before target when moving up the order, after it when moving down.
			if (at < 0) order.push(id);
			else order.splice(a >= 0 && a < b ? at + 1 : at, 0, id);
			return { tab, order };
		}
		// A preset tab becomes a tree that reproduces its tiles first.
		const base = tree ?? fromRects(this.presetRects(ids));
		return { tab, tree: applyZone(base, id, zone, this.desk, DEFAULTS, this.tiledShown(id)) };
	}

	private planRects(plan: DropPlan, id: string): Map<string, Rect> {
		if ('tree' in plan) return layout(plan.tree, this.desk, DEFAULTS, this.tiledShown(id));
		const ids = this.tiledWindows().map((w) => w.id);
		if (!ids.includes(id)) ids.push(id);
		const rank = new Map(plan.order.map((x, i) => [x, i]));
		return this.presetRects(ids.sort((a, b) => (rank.get(a) ?? 1e9) - (rank.get(b) ?? 1e9)));
	}

	/** Shows where window id (null: a session still to open) would land in zone; null zone clears it. */
	previewDrop(id: string | null, zone: Zone | null) {
		if (!zone) {
			this.clearDrop();
			return;
		}
		const wid = id ?? NEW_WINDOW;
		const key = wid + JSON.stringify(zone);
		if (this.planned?.key !== key) this.planned = { key, plan: this.planDrop(wid, zone) };
		const r = this.planRects(this.planned.plan, wid).get(wid);
		const next = r ? { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.w), h: Math.round(r.h) } : null;
		const cur = this.dropPreview;
		if (!next || !cur || next.x !== cur.x || next.y !== cur.y || next.w !== cur.w || next.h !== cur.h) this.dropPreview = next;
	}

	clearDrop() {
		this.dropPreview = null;
		this.planned = null;
	}

	/** Ends a title bar drag: the window lands in zone, or goes back to its tile. */
	land(w: Win, zone: Zone | null) {
		this.clearDrop();
		if (zone && this.autoDock) this.commitDrop(this.planDrop(w.id, zone), w.id);
		else this.retile(true);
	}

	private commitDrop(plan: DropPlan, id: string) {
		const w = this.windows.find((x) => x.id === id);
		if (!w || plan.tab !== this.activeTab || !this.autoDock) {
			this.retile();
			return;
		}
		if ('tree' in plan) this.setTree(plan.tab, rename(plan.tree, NEW_WINDOW, id));
		else {
			this.dockOrder = [...new Set([...plan.order.map((x) => (x === NEW_WINDOW ? id : x)), ...this.dockOrder])];
			setLocal('dockOrder', this.dockOrder);
		}
		w.tab = plan.tab;
		w.min = false;
		w.max = false;
		this.retile(true);
		w.save();
	}

	/**
	 * Starts moving a split next to a tiled window, as in a tiling window
	 * manager: the edge moves and the neighbour gives way, nothing overlaps.
	 * edge is the window side being dragged. It returns a function taking the
	 * pointer travel along that axis, or null when that side has no neighbour.
	 */
	split(w: Win, edge: 'n' | 's' | 'e' | 'w'): ((d: number) => void) | null {
		if (!this.autoDock || w.max || w.min) return null;
		const tab = this.activeTab;
		const tree = this.trees[tab];
		if (tree) {
			const from = edgeAt(tree, w.id, edge, this.desk, DEFAULTS, this.tiledShown());
			if (from === null) return null;
			// Resolved on the current tree each move: a corner moves two splits, and a poll may change the tree mid-drag.
			return (d: number) => {
				const cur = this.trees[tab];
				if (!cur || tab !== this.activeTab) return;
				const next = moveEdge(cur, w.id, edge, from + d, this.desk, DEFAULTS, this.tiledShown());
				if (!next) return;
				this.setTree(tab, next);
				this.retile();
			};
		}
		const list = this.tiledWindows();
		if (list.length < 2 || !list.includes(w)) return null;
		const horizontal = edge === 'e' || edge === 'w';
		const grow = edge === 'e' || edge === 's' ? 1 : -1;
		const { minW, minH } = DEFAULTS;
		// Two sizes along one axis that trade space: own and the neighbour's.
		const pairSplit = (own: number, other: number, min: number, apply: (ownShare: number) => void) => {
			const pair = own + other;
			return (d: number) => {
				const next = Math.min(Math.max(own + grow * d, min), pair - min);
				apply(next / pair);
				this.retile();
			};
		};
		const weighted = (a: Win, b: Win, store: Record<string, number>, key: string) => {
			const sum = (store[a.id] ?? 1) + (store[b.id] ?? 1);
			return (share: number) => {
				store[a.id] = sum * share;
				store[b.id] = sum * (1 - share);
				setLocal(key, store);
			};
		};

		if (this.dockLayout === 'stack') {
			const [main, ...rest] = list;
			if (horizontal) {
				// The one vertical split: main on the left, the stack on the right.
				if ((w === main && edge === 'e') || (w !== main && edge === 'w')) {
					const ownIsMain = w === main;
					const mainW = main.w;
					const restW = rest[0].w;
					return pairSplit(ownIsMain ? mainW : restW, ownIsMain ? restW : mainW, minW, (share) => {
						this.mainRatio = ownIsMain ? share : 1 - share;
						setLocal('mainRatio', this.mainRatio);
					});
				}
				return null;
			}
			// Between two windows of the stack.
			const i = rest.indexOf(w);
			const nb = rest[edge === 's' ? i + 1 : i - 1];
			if (i < 0 || !nb) return null;
			return pairSplit(w.h, nb.h, minH, weighted(w, nb, this.stackWeights, 'stackWeights'));
		}

		const rows = this.gridRows(list.map((x) => x.id)).map((row) => row.map((id) => list.find((x) => x.id === id)!));
		const r = rows.findIndex((row) => row.includes(w));
		if (horizontal) {
			const row = rows[r];
			const i = row.indexOf(w);
			const nb = row[edge === 'e' ? i + 1 : i - 1];
			if (!nb) return null;
			return pairSplit(w.w, nb.w, minW, weighted(w, nb, this.colWeights, 'colWeights'));
		}
		const nr = edge === 's' ? r + 1 : r - 1;
		if (nr < 0 || nr >= rows.length) return null;
		const rw = this.rowWeights;
		const sum = (rw[r] ?? 1) + (rw[nr] ?? 1);
		return pairSplit(w.h, rows[nr][0].h, minH, (share) => {
			for (let k = 0; k < rows.length; k++) rw[k] ??= 1;
			rw[r] = sum * share;
			rw[nr] = sum * (1 - share);
			setLocal('rowWeights', rw);
		});
	}

	/**
	 * Brings the active tab's tree in line with its windows: leaves of windows
	 * that left the tab or closed drop out; hidden and maximized windows keep
	 * their leaf, so they come back to their own slot; new tiled windows split
	 * the focused tile (else the largest). persist saves a change made here.
	 */
	private reconcile(list: Win[], persist: boolean): Node | null {
		const tab = this.activeTab;
		const before = this.trees[tab];
		if (!before) return null;
		const mine = new Set(this.tabWindows(tab).map((w) => w.id));
		const known = new Set(this.windows.map((w) => w.id));
		const shown = this.tiledShown();
		let t = prune(before, (id) => mine.has(id) || (!this.synced && !known.has(id)));
		for (const w of list) {
			if (has(t, w.id)) continue;
			const near = this.focusedId && this.focusedId !== w.id && shown(this.focusedId) && has(t, this.focusedId) ? this.focusedId : null;
			t = insertAuto(t, w.id, near, this.desk, DEFAULTS, shown);
		}
		if (JSON.stringify(t) !== JSON.stringify(before)) this.setTree(tab, t, persist);
		else if (persist) this.setTrees(this.trees);
		return t;
	}

	/** Tiles the visible windows. Runs whenever the windows or the desktop change; persist when a change made here caused it. */
	retile(persist = false) {
		if (!this.autoDock) {
			for (const w of this.windows) w.clamp();
			return;
		}
		const list = this.tiledWindows();
		if (!this.desk.w || !this.desk.h) return;
		const tree = this.reconcile(list, persist);
		const rects = tree ? layout(tree, this.desk, DEFAULTS, this.tiledShown()) : this.presetRects(list.map((w) => w.id));
		for (const w of list) {
			const r = rects.get(w.id);
			if (r) w.tile(r.x, r.y, r.w, r.h);
		}
	}

	// ---------- folders ----------

	/** Folders the user collapsed, per browser. */
	collapsed = $state<Record<string, boolean>>(local<Record<string, boolean>>('collapsed') ?? {});

	toggleFolder(id: string) {
		this.collapsed[id] = !this.collapsed[id];
		setLocal('collapsed', this.collapsed);
	}

	private async saveFolders(folders: Folder[]) {
		if (!this.data) return false;
		const before = this.data;
		this.data = { ...before, folders };
		try {
			const res = await api<{ folders: Folder[] }>('PUT', '/api/folders', { folders });
			this.data = { ...this.data, folders: res.folders };
			return true;
		} catch (e) {
			this.data = { ...this.data, folders: before.folders };
			this.notify((e as Error).message, true);
			return false;
		}
	}

	createFolder(name: string) {
		const id = Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
		return this.saveFolders([...(this.data?.folders ?? []), { id, name, items: [] }]);
	}

	renameFolder(id: string, name: string) {
		return this.saveFolders((this.data?.folders ?? []).map((f) => (f.id === id ? { ...f, name } : f)));
	}

	async deleteFolder(f: Folder) {
		if (f.items.length) {
			const ok = await this.ask('Delete folder?', `"${f.name}" holds ${f.items.length} session${f.items.length === 1 ? '' : 's'}. They move back under their workspace; nothing is stopped or forgotten.`, [
				{ label: 'Delete folder', value: 'yes', tone: 'danger' }
			]);
			if (ok !== 'yes') return;
		}
		await this.saveFolders((this.data?.folders ?? []).filter((x) => x.id !== f.id));
	}

	/** Folders picked for new unnamed runs, by window id: folders hold session names, learned from the container. */
	pendingFolder = $state<Record<string, string>>({});

	/** Files a just started session in a folder, once it has a name to file. */
	fileNewSession(info: SessionInfo, folderId: string) {
		if (info.name) this.moveToFolder(windowKey(info), folderId);
		else this.pendingFolder[info.id] = folderId;
	}

	private filePending(info: SessionInfo) {
		const folderId = this.pendingFolder[info.id];
		delete this.pendingFolder[info.id];
		if (info.name && this.data?.folders.some((f) => f.id === folderId)) this.moveToFolder(windowKey(info), folderId);
	}

	/** Files a session in a folder, or takes it out with folderId null. */
	moveToFolder(key: string, folderId: string | null) {
		const folders = (this.data?.folders ?? []).map((f) => ({
			...f,
			items: f.id === folderId ? [...f.items.filter((k) => k !== key), key] : f.items.filter((k) => k !== key)
		}));
		return this.saveFolders(folders);
	}

	// ---------- saved and terminal sessions ----------

	toggleInactive() {
		this.showInactive = !this.showInactive;
		setLocal('showInactive', this.showInactive);
	}

	/** Opens a window on a session started in a terminal. */
	async attach(r: RunningDoc): Promise<Win | null> {
		try {
			const info = await api<SessionInfo>('POST', '/api/attach', { container: r.container, cols: 120, rows: 36 });
			const w = this.openWindow(info);
			this.refresh();
			return w;
		} catch (e) {
			this.notify((e as Error).message, true);
			return null;
		}
	}

	/** Terminal sessions being terminated, by container. */
	terminatingRunning = $state<Record<string, boolean>>({});

	/** Ends a session started in a terminal without opening a window on it. */
	async terminateRunning(r: RunningDoc) {
		if (this.terminatingRunning[r.container]) return;
		this.terminatingRunning[r.container] = true;
		let id = '';
		try {
			id = (await api<SessionInfo>('POST', '/api/attach', { container: r.container, cols: 120, rows: 36 })).id;
			// A poll during the terminate must not open a window on it.
			this.noWindow.add(id);
			await api('POST', '/api/sessions/' + encodeURIComponent(id) + '/terminate', {});
		} catch (e) {
			// Still attached after a failed terminate: show its window, so it can be retried.
			this.noWindow.delete(id);
			this.notify((e as Error).message, true);
		}
		await this.refresh();
		delete this.terminatingRunning[r.container];
	}

	/**
	 * Opens a session dragged from the sidebar onto the desktop or a tab: a
	 * saved one resumes, a terminal one attaches, an open window moves here.
	 * Dropped in a zone of the desktop, it lands there once its window exists.
	 */
	async openDropped(drop: SidebarDrop, tabId: string | null, zone: Zone | null) {
		this.clearDrop();
		if (tabId && tabId !== this.activeTab) this.setTab(tabId);
		// Planned against the tiles as they were when dropped.
		const plan = zone && this.autoDock ? this.planDrop(drop.kind === 'window' ? drop.id : NEW_WINDOW, zone) : null;
		let w: Win | null = null;
		if (drop.kind === 'window') {
			w = this.windows.find((x) => x.id === drop.id) ?? null;
			if (w) {
				if (w.tab !== this.activeTab) this.moveToTab(w, this.activeTab);
				this.show(w);
			}
		} else if (drop.kind === 'terminal') {
			const r = this.data?.running?.find((x) => x.container === drop.container);
			if (r) w = await this.attach(r);
		} else {
			w = await this.resume(drop.saved);
		}
		if (w && plan) this.commitDrop(plan, w.id);
	}

	/** Renames a window's session; a running one takes the name when it ends. */
	async renameWindow(w: Win, name: string) {
		try {
			w.info = await api<SessionInfo>('POST', '/api/sessions/' + encodeURIComponent(w.id) + '/rename', { name });
			if (w.info.renameTo) this.notify(`Renames to "${w.info.renameTo}" when the session ends.`);
			this.refresh();
			return true;
		} catch (e) {
			this.notify((e as Error).message, true);
			return false;
		}
	}

	/** Renames a session started in a terminal; it takes the name when it ends. */
	async renameRunning(r: RunningDoc, name: string) {
		try {
			await api('POST', '/api/running/rename', { container: r.container, name });
			this.notify(`Renames to "${name}" when the session ends.`);
			await this.refresh();
			return true;
		} catch (e) {
			this.notify((e as Error).message, true);
			return false;
		}
	}

	async renameSaved(s: SavedDoc, newName: string) {
		try {
			await api('POST', '/api/saved/rename', { ...savedRef(s), newName });
			await this.refresh();
			return true;
		} catch (e) {
			this.notify((e as Error).message, true);
			return false;
		}
	}

	async resume(s: { dir: string; agent: string; workspace: string; name: string }): Promise<Win | null> {
		try {
			const info = await api<SessionInfo>('POST', '/api/sessions', { ...savedRef(s), cols: 120, rows: 36 });
			const w = this.openWindow(info);
			this.refresh();
			return w;
		} catch (e) {
			this.notify((e as Error).message, true);
			return null;
		}
	}

	async forget(s: { dir: string; agent: string; workspace: string; name: string }) {
		const ok = await this.ask(
			'Forget saved session?',
			`Removes "${s.name}" (${s.agent} in ${this.tilde(s.dir)}). The agent conversation cannot be resumed afterwards.`,
			[{ label: 'Forget', value: 'yes', tone: 'danger' }]
		);
		if (ok !== 'yes') return;
		try {
			await api('POST', '/api/saved/remove', savedRef(s));
			this.refresh();
		} catch (e) {
			this.notify((e as Error).message, true);
		}
	}

	// ---------- dialogs and notices ----------

	ask(title: string, text: string, choices: Choice[]): Promise<string | null> {
		// One dialog at a time: a new question cancels the open one.
		this.prompt?.resolve(null);
		return new Promise((resolve) => {
			this.prompt = {
				title,
				text,
				choices,
				resolve: (v) => {
					this.prompt = null;
					resolve(v);
				}
			};
		});
	}

	notify(text: string, bad = false) {
		this.toast = { text, bad };
		if (this.toastTimer) clearTimeout(this.toastTimer);
		this.toastTimer = setTimeout(() => (this.toast = null), bad ? 8000 : 3500);
	}

	// ---------- settings ----------

	settingsError = $state('');

	async loadSettings() {
		try {
			const doc = await api<SettingsDoc>('GET', '/api/settings');
			this.settings = doc;
			this.settingsOrig = JSON.stringify(doc);
			this.settingsError = '';
		} catch (e) {
			this.settingsError = (e as Error).message;
		}
	}

	openSettings() {
		this.view = 'settings';
		if (!this.settings) this.loadSettings();
	}

	/** Set by enableLogin; the Web access page focuses its username field once. */
	focusWebLogin = $state(false);

	/** Opens Settings at Web access to set a login. */
	enableLogin() {
		this.settingsSection = 'web';
		this.focusWebLogin = true;
		this.openSettings();
	}

	/** Reloads who is signed in, after the login settings change. */
	async loadMe() {
		try {
			this.me = await api<Me>('GET', '/api/me');
		} catch {
			// The next boot or poll reports a lost server.
		}
	}

	// ---------- helpers ----------

	tilde(p: string) {
		const home = this.data?.home;
		if (home && home !== '/' && (p === home || p.startsWith(home + '/'))) return '~' + p.slice(home.length);
		return p;
	}

	/** What a session runs, for labels: the agent's name, or SSH. */
	sessionDisplay(i: { kind: string; agent: string }) {
		return i.kind === 'ssh' ? 'SSH' : this.agentDisplay(i.agent);
	}

	agentDisplay(name: string) {
		return this.data?.agents.find((a) => a.name === name)?.display ?? name;
	}
}

export const app = new App();
