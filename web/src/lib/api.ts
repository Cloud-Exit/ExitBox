// Client for the exitbox web API (internal/web).

export interface Me {
	authEnabled: boolean;
	signedIn: boolean;
	user: string;
	version: string;
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
	noTmux: boolean;
	fresh: boolean;
	resume?: boolean;
	memory: string;
	cpus: string;
	allowUrls: string[];
	cols?: number;
	rows?: number;
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

/** Per-browser conveniences: window places, theme, last folder. */
export function local<T>(key: string): T | null {
	try {
		const raw = localStorage.getItem('exitbox.' + key);
		return raw ? (JSON.parse(raw) as T) : null;
	} catch {
		return null;
	}
}

export function setLocal(key: string, value: unknown) {
	try {
		if (value === null) localStorage.removeItem('exitbox.' + key);
		else localStorage.setItem('exitbox.' + key, JSON.stringify(value));
	} catch {
		// Storage off: nothing is remembered.
	}
}

export function shellQuote(s: string): string {
	return /^[A-Za-z0-9_@%+=:,./~-]+$/.test(s) ? s : "'" + s.replace(/'/g, "'\\''") + "'";
}
