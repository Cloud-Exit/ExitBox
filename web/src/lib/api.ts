// Client for the exitbox web API (internal/web).

export interface Me {
	authEnabled: boolean;
	signedIn: boolean;
	user: string;
	version: string;
	/** The server only accepts connections from this machine. */
	loopback: boolean;
}

export interface AgentDoc {
	name: string;
	display: string;
	enabled: boolean;
	version?: string;
}

export interface WorkspaceDoc {
	name: string;
	isDefault: boolean;
	directory?: string;
	vault: 'rw' | 'ro' | 'off';
	gpu: boolean;
	packages: string[];
	development: string[];
}

export interface EnvProfiles {
	profiles: string[];
	default: string;
	error?: string;
}

export interface ProjectDoc {
	path: string;
	workspace: string;
	scoped: boolean;
}

export interface SavedDoc {
	workspace: string;
	agent: string;
	dir: string;
	name: string;
}

export interface SessionInfo {
	id: string;
	kind: 'run' | 'attach';
	agent: string;
	dir: string;
	workspace: string;
	name: string;
	args: string[];
	started: string;
	exited: boolean;
	exitCode: number;
	viewers: number;
	container?: string;
	renameTo?: string;
	renameError?: string;
	/** How a run session was started here; missing when unknown. */
	request?: RunOptions;
}

/** An agent container that runs now but has no window: started in a terminal. */
export interface RunningDoc {
	container: string;
	agent: string;
	dir: string;
	workspace: string;
	session: string;
	started: string;
	noTmux: boolean;
	/** A rename queued until the session ends. */
	renameTo?: string;
}

export interface Defaults {
	noFirewall: boolean;
	readOnly: boolean;
	noEnv: boolean;
	autoResume: boolean;
	fullGit: boolean;
	memory: string;
	cpus: string;
}

export interface State {
	version: string;
	runtime: string;
	authEnabled: boolean;
	user: string;
	listen: string;
	home: string;
	agents: AgentDoc[];
	workspaces: WorkspaceDoc[];
	defaults: Defaults;
	projects: ProjectDoc[];
	saved: SavedDoc[];
	sessions: SessionInfo[];
	running: RunningDoc[];
	folders: Folder[];
}

/** A sidebar folder; items are sessionKey values. */
export interface Folder {
	id: string;
	name: string;
	items: string[];
}

/** Identifies a named session across runs (internal/web sessionKey). */
export function sessionKey(workspace: string, agent: string, dir: string, name: string) {
	return [workspace, agent, dir, name].join('\t');
}

export interface StartRequest {
	dir: string;
	agent: string;
	workspace: string;
	name: string;
	envProfile: string;
	noFirewall: boolean;
	readOnly: boolean;
	noEnv: boolean;
	ollama: boolean;
	fresh: boolean;
	resume?: boolean;
	memory: string;
	cpus: string;
	allowUrls: string[];
	cols?: number;
	rows?: number;
}

/** The options a session keeps (internal/web RunOptions). */
export type RunOptions = Omit<StartRequest, 'cols' | 'rows'>;

/** The shell command that starts a run session. */
export function runCommand(dir: string, args: string[]) {
	return 'cd ' + shellQuote(dir) + ' && exitbox ' + args.map(shellQuote).join(' ');
}

export interface WorkspaceEdit {
	name: string;
	directory: string;
	gpu: boolean;
	packages: string[];
	vault?: string;
	development?: string[];
}

export interface SettingsDoc {
	autoUpdate: boolean;
	autoUpdateTools: boolean;
	statusBar: boolean;
	rtk: boolean;
	defaultWorkspace: string;
	noFirewall: boolean;
	readOnly: boolean;
	noEnv: boolean;
	autoResume: boolean;
	fullGit: boolean;
	memory: string;
	cpus: string;
	workspaceMenu: string;
	sessionMenu: string;
	agents: AgentDoc[];
	workspaces: WorkspaceEdit[];
	allowlist: {
		aiProviders?: string[];
		development?: string[];
		cloudServices?: string[];
		commonServices?: string[];
		custom: string[];
		hostPorts: number[];
	};
	web?: { listen: string; listenActive: string; username: string; authEnabled: boolean };
	keybindingDefaults?: { workspaceMenu: string; sessionMenu: string };
}

export interface FolderList {
	path: string;
	parent: string;
	dirs: string[];
}

export class ApiError extends Error {
	constructor(
		public status: number,
		message: string
	) {
		super(message);
	}
}

/** Called when a request comes back 401, so the app can show the login. */
let onUnauthorized: () => void = () => {};
export function setUnauthorizedHandler(fn: () => void) {
	onUnauthorized = fn;
}

export async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
	const headers: Record<string, string> = { 'X-ExitBox': '1' };
	const init: RequestInit = { method, credentials: 'same-origin', headers };
	if (body !== undefined) {
		headers['Content-Type'] = 'application/json';
		init.body = JSON.stringify(body);
	}
	const res = await fetch(path, init);
	let data: unknown = null;
	try {
		data = await res.json();
	} catch {
		data = null;
	}
	if (res.status === 401 && path !== '/api/login') {
		onUnauthorized();
		throw new ApiError(401, 'Sign in first');
	}
	if (!res.ok) {
		const msg = (data as { error?: string } | null)?.error ?? res.statusText;
		throw new ApiError(res.status, msg);
	}
	return data as T;
}

// The desktop layout (tabs, tiling, window places) lives on the server so
// it survives restarts and matches in every browser. Other keys (theme,
// sidebar state, last folder) are per-browser conveniences in localStorage.
const LAYOUT_KEYS = new Set(['tabs', 'activeTab', 'autoDock', 'dockLayout', 'dockOrder', 'colWeights', 'rowWeights', 'stackWeights', 'mainRatio']);
const isLayoutKey = (key: string) => LAYOUT_KEYS.has(key) || key.startsWith('win.');

let layout: Record<string, unknown> | null = null;
let saveTimer: ReturnType<typeof setTimeout> | null = null;

/** Loads the server's layout; until then layout keys come from localStorage. */
export async function loadLayout() {
	layout = await api<Record<string, unknown>>('GET', '/api/layout');
}

function saveLayoutSoon() {
	if (saveTimer) clearTimeout(saveTimer);
	saveTimer = setTimeout(() => {
		saveTimer = null;
		if (layout) api('PUT', '/api/layout', layout).catch(() => {});
	}, 400);
}

export function local<T>(key: string): T | null {
	if (layout && isLayoutKey(key) && key in layout) return layout[key] as T;
	try {
		const raw = localStorage.getItem('exitbox.' + key);
		return raw ? (JSON.parse(raw) as T) : null;
	} catch {
		return null;
	}
}

export function setLocal(key: string, value: unknown) {
	if (layout && isLayoutKey(key)) {
		if (value === null) delete layout[key];
		else layout[key] = JSON.parse(JSON.stringify(value));
		saveLayoutSoon();
	}
	try {
		if (value === null) localStorage.removeItem('exitbox.' + key);
		else localStorage.setItem('exitbox.' + key, JSON.stringify(value));
	} catch {
		// Storage off: nothing more is remembered here.
	}
}

export function shellQuote(s: string): string {
	return /^[A-Za-z0-9_@%+=:,./~-]+$/.test(s) ? s : "'" + s.replace(/'/g, "'\\''") + "'";
}
