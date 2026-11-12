<script module lang="ts">
	import { app, sessionLabel, windowKey, type Win } from './app.svelte';
	import { sessionKey, type RunningDoc, type SavedDoc } from './api';
	import type { MenuItem } from './ui/ContextMenu.svelte';

	/** What a session menu acts on: an open window, a terminal session, or a saved one. */
	export type SessionTarget = { kind: 'window'; win: Win } | { kind: 'terminal'; run: RunningDoc } | { kind: 'saved'; saved: SavedDoc };

	/** A folder made from a menu, for the sidebar to name in place. */
	export const folderNaming = $state<{ id: string | null }>({ id: null });

	/** The folder key of a session, or null while it has no name to file. */
	export function targetKey(t: SessionTarget) {
		if (t.kind === 'window') return t.win.info.name ? windowKey(t.win.info) : null;
		if (t.kind === 'terminal') return t.run.session ? sessionKey(t.run.workspace || 'default', t.run.agent, t.run.dir, t.run.session) : null;
		return sessionKey(t.saved.workspace, t.saved.agent, t.saved.dir, t.saved.name);
	}

	export function targetName(t: SessionTarget) {
		if (t.kind === 'window') return t.win.title;
		if (t.kind === 'terminal') return t.run.renameTo || sessionLabel(t.run.session, t.run.agent, t.run.started);
		return t.saved.name;
	}

	/** Shows a window (on its tab), attaches a terminal session, or resumes a saved one. */
	export function openTarget(t: SessionTarget) {
		if (t.kind === 'window') {
			app.view = 'sessions';
			app.show(t.win);
		} else if (t.kind === 'terminal') {
			if (!t.run.noTmux) void app.attach(t.run);
		} else {
			void app.resume(t.saved);
		}
	}

	/** Closes a window and keeps its session running: in its terminal, or in the sidebar. */
	export function detach(w: Win) {
		if (w.attached) void app.stop(w);
		else app.hide(w);
	}

	/** Makes a folder, files the session in it, and has the sidebar ask for its name. */
	export async function newFolderFor(key: string) {
		if (!(await app.createFolder('New folder'))) return;
		const f = app.data?.folders.at(-1);
		if (!f) return;
		await app.moveToFolder(key, f.id);
		if (app.sidebarCollapsed) app.toggleSidebar();
		folderNaming.id = f.id;
	}

	/** The folder picker: every folder (current one checked), no folder, and a new one. */
	export function folderItems(key: string): MenuItem[] {
		const folders = app.data?.folders ?? [];
		const current = folders.find((f) => f.items.includes(key))?.id ?? null;
		const pick = (id: string | null) => () => {
			if (id !== current) void app.moveToFolder(key, id);
		};
		return [
			...folders.map((f): MenuItem => ({ label: f.name, icon: 'folder', checked: f.id === current, onselect: pick(f.id) })),
			{ label: 'No folder', icon: 'x', checked: current === null, onselect: pick(null) },
			{ type: 'separator' },
			{ label: 'New folder…', icon: 'folderPlus', onselect: () => void newFolderFor(key) }
		];
	}
</script>

<script lang="ts">
	import ContextMenu from './ui/ContextMenu.svelte';

	let {
		target,
		x,
		y,
		anchor = null,
		returnFocus = null,
		keyboard = false,
		inWindow = false,
		onrename,
		onclose
	}: {
		target: SessionTarget;
		x: number;
		y: number;
		anchor?: HTMLElement | null;
		returnFocus?: HTMLElement | null;
		keyboard?: boolean;
		/** Opened from the window's own title bar: Open would do nothing. */
		inWindow?: boolean;
		/** Starts renaming in place; left out where it cannot be renamed. */
		onrename?: () => void;
		onclose: () => void;
	} = $props();

	const name = $derived(targetName(target));

	async function confirmDetach(w: Win) {
		const where = w.attached ? 'in its terminal' : 'in the sidebar';
		const ok = await app.ask(`Detach ${w.title}?`, `The window closes; the session keeps running ${where}.`, [{ label: 'Detach', value: 'yes', tone: 'primary' }]);
		if (ok === 'yes') detach(w);
	}

	async function confirmTerminate(t: SessionTarget) {
		const n = targetName(t);
		const fromTerminal = t.kind === 'terminal' || (t.kind === 'window' && t.win.attached);
		const agent = app.agentDisplay(t.kind === 'window' ? t.win.info.agent : t.kind === 'terminal' ? t.run.agent : t.saved.agent);
		const text = fromTerminal
			? `${agent} stops, and so does the exitbox run in its terminal.`
			: t.kind === 'window' && t.win.info.name
				? `${agent} stops; its saved session stays, to resume later.`
				: `${agent} stops and its container ends.`;
		const ok = await app.ask(`Terminate ${n}?`, text, [{ label: 'Terminate', value: 'yes', tone: 'danger' }]);
		if (ok !== 'yes') return;
		if (t.kind === 'window') void app.terminate(t.win);
		else if (t.kind === 'terminal') void app.terminateRunning(t.run);
	}

	const items = $derived.by(() => {
		const t = target;
		const out: MenuItem[] = [];
		const sep = () => {
			if (out.length && out.at(-1)?.type !== 'separator') out.push({ type: 'separator' });
		};
		if (!inWindow) {
			const noTmux = t.kind === 'terminal' && t.run.noTmux;
			out.push({ label: t.kind === 'saved' ? 'Resume' : 'Open', icon: 'play', disabled: noTmux, onselect: () => openTarget(t) });
		}
		if (t.kind === 'window') {
			const w = t.win;
			out.push({ label: w.max ? 'Exit full screen' : 'Full screen', icon: w.max ? 'restore' : 'maximize', onselect: () => app.toggleMax(w) });
			if (w.min) out.push({ label: 'Restore', icon: 'restore', onselect: () => openTarget(t) });
			else out.push({ label: 'Minimize', icon: 'minus', onselect: () => app.hide(w) });
		}
		sep();
		if (onrename) out.push({ label: 'Rename', icon: 'pencil', onselect: onrename });
		if (t.kind === 'window') out.push({ label: 'Session settings', icon: 'settings', onselect: () => (app.sessionSettings = t.win) });
		const key = targetKey(t);
		if (key) out.push({ type: 'group', label: 'Move to folder', icon: 'folderInput', items: folderItems(key) });
		sep();
		if (t.kind === 'window') {
			const w = t.win;
			if (w.info.exited) {
				out.push({ label: w.attached ? 'Attach again' : 'Start again', icon: 'rotate', onselect: () => void app.restart(w, 120, 36) });
				out.push({ label: 'Close window', icon: 'x', onselect: () => void app.stop(w) });
			} else {
				out.push({ label: 'Detach', icon: 'logout', onselect: () => void confirmDetach(w) });
				out.push({ label: 'Terminate', icon: 'power', danger: true, onselect: () => void confirmTerminate(t) });
			}
		} else if (t.kind === 'terminal') {
			if (!t.run.noTmux) out.push({ label: 'Terminate', icon: 'power', danger: true, onselect: () => void confirmTerminate(t) });
		} else {
			out.push({ label: 'Forget saved session', icon: 'trash', danger: true, onselect: () => void app.forget(t.saved) });
		}
		if (out.at(-1)?.type === 'separator') out.pop();
		return out;
	});
</script>

<ContextMenu {items} {x} {y} {anchor} {returnFocus} {keyboard} label={'Actions for ' + name} {onclose} />
