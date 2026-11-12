// App state shared by the components. The app is client-only (ssr = false),
// so one module-level instance is safe.
import { api, ApiError, loadLayout, local, setLocal, setUnauthorizedHandler } from './api';
import type { Folder, Me, RunningDoc, SavedDoc, SessionInfo, SettingsDoc, State } from './api';

export const MIN_W = 280;
export const MIN_H = 160;

interface Geometry {
	x: number;
	y: number;
	w: number;
	h: number;
	max?: boolean;
	min?: boolean;
	tab?: string;
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

	constructor(info: SessionInfo) {
		this.info = info;
	}

	get id() {
		return this.info.id;
	}

	get title() {
		return this.info.renameTo || sessionLabel(this.info.name, this.info.agent, this.info.started);
	}

	get attached() {
		return this.info.kind === 'attach';
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
		this.w = Math.max(Math.min(MIN_W, dw), Math.min(this.w, dw));
		this.h = Math.max(Math.min(MIN_H, dh), Math.min(this.h, dh));
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
	/** The window a dragged window would swap with. */
	dropTarget = $state<string | null>(null);
	/** Tabs are separate desktops, each tiling its own windows. */
	tabs = $state<Tab[]>(local<Tab[]>('tabs')?.length ? local<Tab[]>('tabs')! : [{ id: 'main', name: 'Main' }]);
	activeTab = $state(local<string>('activeTab') ?? 'main');
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

	private zTop = 10;
	private poll: ReturnType<typeof setInterval> | null = null;
	private toastTimer: ReturnType<typeof setTimeout> | null = null;

	focused = $derived(this.windows.find((w) => w.id === this.focusedId) ?? null);
	running = $derived(this.windows.filter((w) => !w.info.exited).length);

	constructor() {
		setUnauthorizedHandler(() => this.showLogin());
	}

	// ---------- boot and polling ----------

	async boot() {
		this.applyTheme();
		let me: Me;
		try {
			me = await api<Me>('GET', '/api/me');
		} catch (e) {
			this.offlineReason = (e as Error).message;
			this.view = 'offline';
			return;
		}
		this.me = me;
		if (me.authEnabled && !me.signedIn) {
			this.showLogin();
			return;
		}
		// xterm measures the cell size once, so the font has to be there first.
		try {
			await Promise.all([
				document.fonts.load("13px 'Atkinson Hyperlegible Mono'"),
				document.fonts.load("bold 13px 'Atkinson Hyperlegible Mono'")
			]);
		} catch {
			// The fallback font works too.
		}
		try {
			await loadLayout();
		} catch {
			// The localStorage copy of the layout serves meanwhile.
		}
		this.applyLayout();
		try {
			this.data = await api<State>('GET', '/api/state');
		} catch {
			return;
		}
		this.view = 'sessions';
		this.sync(this.data.sessions);
		this.forgetDock(this.windows.map((w) => w.id));
		this.currentTab();
		this.focusTop();
		this.startPolling();
	}

	async refresh() {
		try {
			this.data = await api<State>('GET', '/api/state');
		} catch {
			return;
		}
		this.sync(this.data.sessions);
	}

	/** Adds windows for new sessions and drops windows of forgotten ones. */
	private sync(sessions: SessionInfo[]) {
		const live = new Set(sessions.map((s) => s.id));
		for (const info of sessions) {
			const w = this.windows.find((x) => x.id === info.id);
			if (w) w.info = info;
			else this.addWindow(info);
		}
		for (const w of this.windows.filter((x) => !live.has(x.id))) this.dropWindow(w);
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
		this.windows = [];
		this.focusedId = null;
		this.settings = null;
		this.newSessionOpen = false;
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
	}

	toggleTheme() {
		setLocal('theme', this.theme === 'dark' ? 'light' : 'dark');
		this.applyTheme();
	}

	// ---------- windows ----------

	private addWindow(info: SessionInfo) {
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
			setLocal('dockOrder', this.dockOrder);
		}
		this.retile();
		return w;
	}

	private dropWindow(w: Win) {
		setLocal('win.' + w.id, null);
		this.windows = this.windows.filter((x) => x.id !== w.id);
		if (this.focusedId === w.id) this.focusedId = null;
		this.forgetDock(this.windows.map((x) => x.id));
		this.retile();
	}

	/** Drops dock order and column widths of windows not in keep. */
	private forgetDock(keep: string[]) {
		this.dockOrder = this.dockOrder.filter((id) => keep.includes(id));
		setLocal('dockOrder', this.dockOrder);
		for (const store of [this.colWeights, this.stackWeights]) {
			for (const id of Object.keys(store)) {
				if (!keep.includes(id)) delete store[id];
			}
		}
		setLocal('colWeights', this.colWeights);
		setLocal('stackWeights', this.stackWeights);
	}

	openWindow(info: SessionInfo) {
		const w = this.windows.find((x) => x.id === info.id) ?? this.addWindow(info);
		this.view = 'sessions';
		this.show(w);
		return w;
	}

	focus(w: Win) {
		this.focusedId = w.id;
		if (w.z !== this.zTop) w.z = ++this.zTop;
	}

	show(w: Win) {
		if (w.tab !== this.activeTab) this.setTab(w.tab);
		if (w.min) {
			w.min = false;
			w.save();
			this.retile();
		}
		this.focus(w);
	}

	hide(w: Win) {
		w.min = true;
		w.save();
		this.retile();
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
		if (this.activeTab === id) this.activeTab = to;
		this.saveTabs();
		this.retile();
		this.focusTop();
	}

	/** Moves a window to another tab; the current tab closes the gap. */
	moveToTab(w: Win, id: string) {
		if (w.tab === id || !this.tabs.some((t) => t.id === id)) return;
		w.tab = id;
		w.max = false;
		w.save();
		this.retile();
		if (this.focusedId === w.id) this.focusTop();
	}

	tabWindows(id: string) {
		return this.windows.filter((w) => w.tab === id);
	}

	toggleMax(w: Win, force?: boolean) {
		w.max = force ?? !w.max;
		w.save();
		this.retile();
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
		this.dropWindow(w);
		this.refresh();
	}

	/** Ends a session for good, also when it was started in a terminal. */
	async terminate(w: Win) {
		try {
			await api('POST', '/api/sessions/' + encodeURIComponent(w.id) + '/terminate', {});
		} catch (e) {
			if (!(e instanceof ApiError && e.status === 404)) {
				this.notify((e as Error).message, true);
				return;
			}
		}
		this.dropWindow(w);
		this.refresh();
	}

	async restart(w: Win, cols: number, rows: number) {
		try {
			const info = await api<SessionInfo>('POST', '/api/sessions/' + encodeURIComponent(w.id) + '/restart', { cols, rows });
			setLocal('win.' + info.id, { x: w.x, y: w.y, w: w.w, h: w.h, max: w.max, min: false, tab: w.tab });
			// The new window takes the old one's dock slot.
			this.dockOrder = this.dockOrder.map((id) => (id === w.id ? info.id : id));
			for (const store of [this.colWeights, this.stackWeights]) {
				if (store[w.id]) store[info.id] = store[w.id];
			}
			setLocal('dockOrder', this.dockOrder);
			// Open the new window before dropping the old one: dropping prunes
			// dock entries of windows that do not exist yet.
			this.openWindow(info);
			this.dropWindow(w);
			this.refresh();
		} catch (e) {
			this.notify((e as Error).message, true);
		}
	}

	ended(w: Win, code: number) {
		w.info = { ...w.info, exited: true, exitCode: code };
		this.refresh();
	}

	// ---------- docking ----------

	/** Picks a dock layout; null is Free, where windows float. */
	setDock(layout: DockLayout | null) {
		this.autoDock = layout !== null;
		setLocal('autoDock', this.autoDock);
		if (layout) {
			this.dockLayout = layout;
			setLocal('dockLayout', layout);
			for (const w of this.windows) w.max = false;
		}
		this.retile();
		for (const w of this.windows) w.save();
	}

	/** Moves a dragged window into target's slot; the others shift along. */
	swapDock(w: Win, target: Win) {
		const order = this.tiledWindows().map((x) => x.id);
		const a = order.indexOf(w.id);
		const b = order.indexOf(target.id);
		if (a >= 0 && b >= 0) {
			const full = this.dockOrder.filter((id) => id !== w.id);
			const at = full.indexOf(target.id);
			// Before target when moving up the order, after it when moving down.
			full.splice(a < b ? at + 1 : at, 0, w.id);
			this.dockOrder = full;
			setLocal('dockOrder', full);
		}
		this.retile();
		w.save();
	}

	/** The windows the layout tiles: the visible ones of the active tab that are not maximized. */
	tiledWindows() {
		const rank = new Map(this.dockOrder.map((id, i) => [id, i]));
		return this.windows
			.filter((w) => !w.min && !w.max && w.tab === this.activeTab)
			.sort((a, b) => (rank.get(a.id) ?? 1e9) - (rank.get(b.id) ?? 1e9));
	}

	/** The tiled windows in rows: one row for columns, rows of the grid. */
	private gridRows(list: Win[]): Win[][] {
		if (this.dockLayout === 'columns') return [list];
		const cols = Math.ceil(Math.sqrt(list.length));
		const rows: Win[][] = [];
		for (let i = 0; i < list.length; i += cols) rows.push(list.slice(i, i + cols));
		return rows;
	}

	/**
	 * Starts moving a split next to a tiled window, as in a tiling window
	 * manager: the edge moves and the neighbour gives way, nothing overlaps.
	 * edge is the window side being dragged. It returns a function taking the
	 * pointer travel along that axis, or null when that side has no neighbour.
	 */
	split(w: Win, edge: 'n' | 's' | 'e' | 'w'): ((d: number) => void) | null {
		if (!this.autoDock || w.max || w.min) return null;
		const list = this.tiledWindows();
		if (list.length < 2 || !list.includes(w)) return null;
		const horizontal = edge === 'e' || edge === 'w';
		const grow = edge === 'e' || edge === 's' ? 1 : -1;
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
					return pairSplit(ownIsMain ? mainW : restW, ownIsMain ? restW : mainW, MIN_W, (share) => {
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
			return pairSplit(w.h, nb.h, MIN_H, weighted(w, nb, this.stackWeights, 'stackWeights'));
		}

		const rows = this.gridRows(list);
		const r = rows.findIndex((row) => row.includes(w));
		if (horizontal) {
			const row = rows[r];
			const i = row.indexOf(w);
			const nb = row[edge === 'e' ? i + 1 : i - 1];
			if (!nb) return null;
			return pairSplit(w.w, nb.w, MIN_W, weighted(w, nb, this.colWeights, 'colWeights'));
		}
		const nr = edge === 's' ? r + 1 : r - 1;
		if (nr < 0 || nr >= rows.length) return null;
		const rw = this.rowWeights;
		const sum = (rw[r] ?? 1) + (rw[nr] ?? 1);
		return pairSplit(w.h, rows[nr][0].h, MIN_H, (share) => {
			for (let k = 0; k < rows.length; k++) rw[k] ??= 1;
			rw[r] = sum * share;
			rw[nr] = sum * (1 - share);
			setLocal('rowWeights', rw);
		});
	}

	/** Tiles the visible windows. Runs whenever the windows or the desktop change. */
	retile() {
		if (!this.autoDock) {
			for (const w of this.windows) w.clamp();
			return;
		}
		const list = this.tiledWindows();
		const { w: dw, h: dh } = this.desk;
		if (!list.length || !dw || !dh) return;
		const gap = 8;
		// Lays out sizes proportional to weights along one axis.
		const spans = (room: number, weights: number[]) => {
			const total = weights.reduce((a, b) => a + b, 0);
			return weights.map((x) => (room * x) / total);
		};
		if (list.length === 1) {
			list[0].tile(gap, gap, dw - 2 * gap, dh - 2 * gap);
			return;
		}
		if (this.dockLayout === 'stack') {
			const [main, ...rest] = list;
			const lw = Math.round((dw - 3 * gap) * this.mainRatio);
			main.tile(gap, gap, lw, dh - 2 * gap);
			const hs = spans(dh - gap * (rest.length + 1), rest.map((w) => this.stackWeights[w.id] ?? 1));
			let y = gap;
			rest.forEach((w, i) => {
				w.tile(2 * gap + lw, y, dw - 3 * gap - lw, hs[i]);
				y += hs[i] + gap;
			});
			return;
		}
		// Columns is a grid of one row.
		const rows = this.gridRows(list);
		const hs = spans(dh - gap * (rows.length + 1), rows.map((_, k) => this.rowWeights[k] ?? 1));
		let y = gap;
		rows.forEach((row, k) => {
			const ws = spans(dw - gap * (row.length + 1), row.map((w) => this.colWeights[w.id] ?? 1));
			let x = gap;
			row.forEach((w, i) => {
				w.tile(x, y, ws[i], hs[k]);
				x += ws[i] + gap;
			});
			y += hs[k] + gap;
		});
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

	/**
	 * Opens a session dragged from the sidebar onto the desktop or a tab: a
	 * saved one resumes, a terminal one attaches, an open window moves here.
	 * Dropped on a tile, the window takes that tile's slot.
	 */
	async openDropped(drop: SidebarDrop, tabId: string | null, targetId: string | null) {
		if (tabId && tabId !== this.activeTab) this.setTab(tabId);
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
		const target = targetId ? this.windows.find((x) => x.id === targetId) : null;
		if (w && target && target !== w && this.autoDock) this.swapDock(w, target);
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

	async renameSaved(s: SavedDoc, newName: string) {
		try {
			await api('POST', '/api/saved/rename', { ...s, newName });
			await this.refresh();
			return true;
		} catch (e) {
			this.notify((e as Error).message, true);
			return false;
		}
	}

	async resume(s: { dir: string; agent: string; workspace: string; name: string }): Promise<Win | null> {
		try {
			const info = await api<SessionInfo>('POST', '/api/sessions', { ...s, cols: 120, rows: 36 });
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
			await api('POST', '/api/saved/remove', s);
			this.refresh();
		} catch (e) {
			this.notify((e as Error).message, true);
		}
	}

	// ---------- dialogs and notices ----------

	ask(title: string, text: string, choices: Choice[]): Promise<string | null> {
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

	// ---------- helpers ----------

	tilde(p: string) {
		const home = this.data?.home;
		if (home && home !== '/' && (p === home || p.startsWith(home + '/'))) return '~' + p.slice(home.length);
		return p;
	}

	agentDisplay(name: string) {
		return this.data?.agents.find((a) => a.name === name)?.display ?? name;
	}
}

export const app = new App();
