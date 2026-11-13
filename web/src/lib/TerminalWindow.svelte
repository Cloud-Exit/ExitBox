<script lang="ts">
	import { untrack } from 'svelte';
	import { Terminal } from '@xterm/xterm';
	import { FitAddon } from '@xterm/addon-fit';
	import { WebLinksAddon } from '@xterm/addon-web-links';
	import { app, MIN_H, MIN_W, sessionStatus, type Win, type Zone } from './app.svelte';
	import { viewport } from './viewport.svelte';
	import { withMods } from './mobile/keys.svelte';
	import { copyText, parseOsc52 } from './clipboard';
	import { termFont } from './termFont.svelte';
	import SessionMenu, { confirmRebuild, detach } from './SessionMenu.svelte';
	import PromptCard from './PromptCard.svelte';
	import AgentIcon from './AgentIcon.svelte';
	import { sshTarget } from './api';
	import Button from './ui/Button.svelte';
	import Chip from './ui/Chip.svelte';
	import IconButton from './ui/IconButton.svelte';
	import Spinner from './ui/Spinner.svelte';
	import StatusPill from './ui/StatusPill.svelte';

	let { win, number }: { win: Win; number: number } = $props();

	const isMac = /Mac|iPhone|iPad/.test(navigator.platform);
	let busy = $state<'' | 'drag' | 'resize'>('');
	let term: Terminal | null = null;
	let applyFont: (() => void) | null = null;
	let cols = 120;
	let rows = 36;

	const focused = $derived(app.focusedId === win.id);
	/** Tiled by the dock layout: resizing moves splits, dragging reorders. */
	const tiled = $derived(app.autoDock && !win.max);
	/** The edges of a tile that border a neighbour; only they resize. */
	const splitSides = $derived.by(() => {
		if (!tiled) return new Set<string>();
		// Recompute when the tiles move.
		void [win.x, win.y, win.w, win.h, app.windows.length, app.dockLayout, app.trees];
		const sides = (['n', 's', 'e', 'w'] as const).filter((d) => app.split(win, d) !== null);
		const set = new Set<string>(sides);
		for (const c of ['ne', 'nw', 'se', 'sw']) if (set.has(c[0]) && set.has(c[1])) set.add(c);
		return set;
	});
	const info = $derived(win.info);
	const status = $derived(sessionStatus(win));

	function termTheme() {
		const cs = getComputedStyle(document.documentElement);
		const v = (n: string) => cs.getPropertyValue(n).trim();
		const light = app.theme === 'light';
		return {
			background: v('--x-inset'),
			foreground: v('--x-ink-1'),
			cursor: v('--x-ink-1'),
			cursorAccent: v('--x-inset'),
			selectionBackground: light ? '#00335630' : '#6ab8ff40',
			black: light ? '#001625' : '#0f1d2c',
			brightBlack: light ? '#5a7a92' : '#5a7692',
			red: light ? '#b10f07' : '#ff6b5e',
			brightRed: light ? '#d4271e' : '#ff8a80',
			green: light ? '#008236' : '#39d66a',
			brightGreen: light ? '#0a9c45' : '#62e88a',
			yellow: light ? '#b76300' : '#ffa842',
			brightYellow: light ? '#d07a10' : '#ffc46e',
			blue: light ? '#1a5a8f' : '#6ab8ff',
			brightBlue: light ? '#2a72b0' : '#94ccff',
			magenta: light ? '#8a3fb0' : '#c792ea',
			brightMagenta: light ? '#a050c8' : '#ddb3f5',
			cyan: light ? '#007a8a' : '#5fd7e6',
			brightCyan: light ? '#0094a6' : '#8ee8f2',
			white: light ? '#334d63' : '#c5d4e3',
			brightWhite: light ? '#001625' : '#ffffff'
		};
	}

	// Follow the app theme.
	$effect(() => {
		void app.theme;
		if (term) term.options.theme = termTheme();
	});

	// Follow the font settings live.
	$effect(() => {
		void [termFont.css, termFont.size, termFont.lineHeight, termFont.weight];
		untrack(() => applyFont?.());
	});

	// Focus the terminal when this window becomes the focused one; a pending prompt takes the keyboard instead.
	// Not on a phone: focus there pops up the on-screen keyboard; a tap on the terminal does it.
	$effect(() => {
		if (!focused || viewport.phone || win.min || win.busy || win.prompts.length) return;
		untrack(() => {
			const at = document.activeElement;
			const typing = lastFocus instanceof HTMLInputElement && lastFocus.type === 'password' && !lastFocus.isConnected;
			lastFocus = null;
			// The rest of a password typed into a card that just went must not reach the agent.
			if (typing && (!at || at === document.body)) holdKeys();
			else if (!held) term?.focus();
		});
	});

	/** The last element focused in the session area: tells a card's password field from the terminal. */
	let lastFocus: Element | null = null;
	/** Keys go nowhere until a pause after the card being typed in went; a click or a later key ends it. */
	let held = $state(false);
	let heldAt = 0;
	let area: HTMLElement | undefined;
	const HOLD_MS = 800;

	function holdKeys() {
		held = true;
		heldAt = Date.now();
		area?.focus({ preventScroll: true });
	}

	function heldKey(e: KeyboardEvent) {
		if (!held || e.target !== area) return;
		// Page shortcuts too: a held key is not a command.
		e.preventDefault();
		e.stopPropagation();
		if (Date.now() - heldAt < HOLD_MS) heldAt = Date.now();
		else {
			held = false;
			term?.focus();
		}
	}

	/** The session area: remembers what had focus in it and holds keys while held. */
	function keyHold(node: HTMLElement) {
		area = node;
		const focusIn = (e: FocusEvent) => (lastFocus = e.target as Element);
		const focusOut = (e: FocusEvent) => {
			if (e.target === node) held = false;
		};
		node.addEventListener('focusin', focusIn);
		node.addEventListener('focusout', focusOut);
		node.addEventListener('keydown', heldKey);
		return () => {
			node.removeEventListener('focusin', focusIn);
			node.removeEventListener('focusout', focusOut);
			node.removeEventListener('keydown', heldKey);
		};
	}

	// Keys typed while the session terminates go nowhere.
	$effect(() => {
		if (win.busy) term?.blur();
	});

	/** The card that may take the keyboard: the oldest one not waiting for the server. */
	const keyPrompt = $derived(win.prompts.find((p) => !p.sending)?.id ?? null);

	/** Other windows waiting for an answer, hidden behind this one while it is maximized. */
	const waiting = $derived(win.max ? app.windows.filter((w) => w !== win && w.prompts.length) : []);

	function showWaiting() {
		const other = waiting[0];
		if (!other) return;
		// A maximized window covers the others of its tab.
		if (other.tab === win.tab) app.toggleMax(win, false);
		app.view = 'sessions';
		app.show(other);
	}

	/**
	 * Mounts xterm and keeps a WebSocket to the session. Output arrives as
	 * binary frames; keys and resizes go out as JSON text frames.
	 */
	function terminal(node: HTMLElement) {
		// Runs once per window: nothing read here may re-run the attachment.
		return untrack(() => mountTerminal(node));
	}

	/** Opens a link from the terminal in a new tab; only web links. */
	function openLink(event: MouseEvent, uri: string) {
		let url: URL;
		try {
			url = new URL(uri);
		} catch {
			return;
		}
		if (url.protocol !== 'http:' && url.protocol !== 'https:') return;
		event.preventDefault();
		window.open(url.href, '_blank', 'noopener,noreferrer');
	}

	function mountTerminal(node: HTMLElement) {
		const t = new Terminal({
			fontFamily: termFont.css,
			fontSize: termFont.size,
			lineHeight: termFont.lineHeight,
			fontWeight: termFont.weight,
			fontWeightBold: termFont.weightBold,
			cursorBlink: true,
			scrollback: 5000,
			theme: termTheme(),
			// Option+drag selects on a Mac while an app has the mouse, as Shift+drag does elsewhere.
			macOptionClickForcesSelection: true,
			// OSC 8 hyperlinks, as agents print them.
			linkHandler: { activate: openLink, allowNonHttpProtocols: false }
		});
		const fit = new FitAddon();
		t.loadAddon(fit);
		// Plain URLs in the output.
		t.loadAddon(new WebLinksAddon(openLink));
		t.open(node);
		term = t;

		let ws: WebSocket | null = null;
		let disposed = false;
		/** True while the replay of a (re)connect is being written. */
		let replaying = true;
		let retry = 0;
		let lastSize = '';
		let timer: ReturnType<typeof setTimeout> | null = null;

		const send = (msg: { t: string; [k: string]: unknown }) => {
			// No keys reach a session that is being terminated.
			if (msg.t === 'i' && win.busy) return;
			if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg));
		};
		// A card goes only on the server's prompt-done; prompt-error brings it back.
		win.answer = (id, allow, password) => {
			const p = win.prompts.find((x) => x.id === id);
			if (!p || p.sending) return false;
			if (ws?.readyState !== WebSocket.OPEN) {
				p.error = 'Not connected, try again';
				return false;
			}
			p.error = '';
			p.sending = true;
			ws.send(JSON.stringify(password ? { t: 'answer', id, allow, password } : { t: 'answer', id, allow }));
			return true;
		};
		// The phone key bar types through here, under the same rules as the keyboard.
		win.input = (d) => {
			if (win.busy || ws?.readyState !== WebSocket.OPEN) return false;
			send({ t: 'i', d });
			t.scrollToBottom();
			return true;
		};
		win.appCursor = () => t.modes.applicationCursorKeysMode;

		const fitNow = () => {
			if (disposed || win.min || app.view !== 'sessions' || !node.clientWidth) return;
			try {
				fit.fit();
			} catch {
				return;
			}
			cols = t.cols;
			rows = t.rows;
			win.cols = cols;
			win.rows = rows;
			const size = cols + 'x' + rows;
			if (size !== lastSize && ws?.readyState === WebSocket.OPEN) {
				lastSize = size;
				send({ t: 'r', c: cols, r: rows });
			}
		};
		let pending = false;
		const fitSoon = () => {
			if (pending) return;
			pending = true;
			requestAnimationFrame(() => {
				pending = false;
				fitNow();
			});
		};

		let connectedOnce = false;
		const connect = () => {
			// An ended session still gets one connection, for its replay.
			if (disposed || (win.info.exited && connectedOnce)) return;
			connectedOnce = true;
			const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
			const sock = new WebSocket(`${proto}//${location.host}/api/sessions/${encodeURIComponent(win.id)}/attach`);
			sock.binaryType = 'arraybuffer';
			ws = sock;
			let first = true;
			// The replay holds old terminal queries (tmux asks for the cursor
			// position and device attributes when it attaches). xterm answers
			// queries as it parses them; answers to the replay must not reach
			// the session, or they show up as typed text in the agent's prompt.
			replaying = true;
			let replayParsing = 0;
			sock.onopen = () => {
				retry = 0;
				lastSize = '';
				win.connected = true;
				fitNow();
			};
			sock.onmessage = (ev) => {
				if (typeof ev.data === 'string') {
					try {
						const msg = JSON.parse(ev.data);
						if (msg?.t === 'live') {
							// Answer queries again once xterm has parsed the replay.
							if (replayParsing === 0) replaying = false;
							else t.write('', () => (replaying = false));
						}
						if (msg?.t === 'exit') {
							win.prompts = [];
							app.ended(win, msg.code);
						}
						if (msg?.t === 'prompt' && typeof msg.id === 'string' && !win.prompts.some((p) => p.id === msg.id)) {
							win.prompts = [...win.prompts, { id: msg.id, kind: msg.kind, agent: msg.agent || win.info.agent, workspace: msg.workspace ?? '', subject: msg.subject ?? '' }];
						}
						if (msg?.t === 'prompt-done') win.prompts = win.prompts.filter((p) => p.id !== msg.id);
						if (msg?.t === 'prompt-error') {
							const p = win.prompts.find((x) => x.id === msg.id);
							if (p) {
								p.sending = false;
								p.error = typeof msg.error === 'string' && msg.error ? msg.error : 'The answer was not accepted';
							}
						}
					} catch {
						// Not a control message.
					}
					return;
				}
				if (first) {
					// A reattach replays recent output onto a clean screen.
					t.reset();
					first = false;
				}
				if (replaying) {
					replayParsing++;
					t.write(new Uint8Array(ev.data), () => replayParsing--);
				} else {
					t.write(new Uint8Array(ev.data));
				}
			};
			sock.onclose = () => {
				if (ws !== sock) return;
				win.connected = false;
				// The server sends them again on the next connection.
				win.prompts = [];
				if (disposed || win.info.exited) return;
				// Quick at first, so a window is back soon after a server restart; slower while it stays down.
				timer = setTimeout(
					() => {
						timer = null;
						connect();
					},
					Math.min(10000, 250 * 2 ** Math.min(retry++, 6))
				);
			};
		};
		// Hidden tabs throttle timers (up to a minute); retry at once when
		// the page shows again or the network returns.
		const reconnectNow = () => {
			if (!timer || document.visibilityState !== 'visible') return;
			clearTimeout(timer);
			timer = null;
			connect();
		};
		document.addEventListener('visibilitychange', reconnectNow);
		window.addEventListener('online', reconnectNow);

		const dataSub = t.onData((d) => {
			// The phone key bar's sticky ctrl and alt apply to on-screen keyboard keys too.
			if (!replaying) send({ t: 'i', d: viewport.phone ? withMods(d) : d });
		});

		let copyFailed = false;
		const copy = (text: string) => {
			if (!text) return;
			void copyText(text).then((ok) => {
				if (ok || copyFailed) return;
				copyFailed = true;
				app.notify('Could not copy: the browser blocked clipboard access.', true);
			});
		};
		// tmux (mouse on, set-clipboard on) and agents copy with OSC 52.
		const oscSub = t.parser.registerOscHandler(52, (data) => {
			// Replayed copies are old; they must not overwrite the clipboard.
			if (replaying) return true;
			const text = parseOsc52(data);
			if (text !== null) copy(text);
			return true;
		});
		// Copy a mouse selection when it ends, as Linux terminals do.
		let selChanged = false;
		const selSub = t.onSelectionChange(() => (selChanged = true));
		const selectEnd = () => {
			// xterm reports the selection in its own mouseup handler, after this one.
			setTimeout(() => {
				if (disposed || !selChanged) return;
				selChanged = false;
				if (t.hasSelection()) copy(t.getSelection());
			});
		};
		const selectStart = (e: MouseEvent) => {
			if (e.button !== 0) return;
			selChanged = false;
			document.addEventListener('mouseup', selectEnd, { once: true });
		};
		// Capture: runs before xterm, which may select a word on this press.
		node.addEventListener('mousedown', selectStart, true);
		// xterm sends Shift+Enter as a plain carriage return, so the agent
		// would submit. Send a line feed instead (what Ctrl+J sends): Claude
		// Code, Codex and OpenCode read it as a newline in the prompt.
		t.attachCustomKeyEventHandler((ev) => {
			// Ctrl+C copies a selection instead of sending ^C; Ctrl+Shift+C only
			// ever copies. Cmd+C on a Mac copies natively.
			if (!isMac && ev.ctrlKey && !ev.altKey && !ev.metaKey && ev.key.toLowerCase() === 'c') {
				if (!ev.shiftKey && !t.hasSelection()) return true;
				ev.preventDefault();
				if (ev.type === 'keydown' && t.hasSelection()) {
					copy(t.getSelection());
					t.clearSelection();
				}
				return false;
			}
			// Ctrl+V pastes, as Cmd+V does on a Mac: xterm would send ^V, which
			// agents read as "paste an image" from a clipboard the container lacks.
			// Leaving the key to the browser fires its paste event, which xterm reads.
			if (!isMac && ev.ctrlKey && !ev.altKey && !ev.metaKey && ev.key.toLowerCase() === 'v') return false;
			if (ev.key !== 'Enter' || !ev.shiftKey || ev.ctrlKey || ev.altKey || ev.metaKey) return true;
			ev.preventDefault();
			if (ev.type === 'keydown') send({ t: 'i', d: '\n' });
			return false;
		});
		// A hidden terminal cannot measure; it does so once it shows again.
		let fontStale = false;
		const remeasure = () => {
			if (disposed) return;
			if (!node.clientWidth) {
				fontStale = true;
				return;
			}
			fontStale = false;
			// Make xterm measure the font again once it is there.
			const family = t.options.fontFamily ?? termFont.css;
			t.options.fontFamily = 'monospace';
			t.options.fontFamily = family;
			fitSoon();
		};
		const ro = new ResizeObserver(() => {
			if (fontStale) remeasure();
			fitSoon();
		});
		ro.observe(node);
		document.fonts?.ready.then(remeasure);
		applyFont = () => {
			t.options.fontFamily = termFont.css;
			t.options.fontSize = termFont.size;
			t.options.lineHeight = termFont.lineHeight;
			t.options.fontWeight = termFont.weight;
			t.options.fontWeightBold = termFont.weightBold;
			remeasure();
			// A web or local font may arrive after the first measure.
			void termFont.load().then(remeasure);
		};
		connect();

		return () => {
			disposed = true;
			if (timer) clearTimeout(timer);
			document.removeEventListener('visibilitychange', reconnectNow);
			window.removeEventListener('online', reconnectNow);
			ro.disconnect();
			dataSub.dispose();
			oscSub.dispose();
			selSub.dispose();
			node.removeEventListener('mousedown', selectStart, true);
			document.removeEventListener('mouseup', selectEnd);
			const s = ws;
			ws = null;
			s?.close();
			win.answer = null;
			win.input = null;
			win.appCursor = () => false;
			win.prompts = [];
			t.dispose();
			term = null;
			applyFont = null;
		};
	}

	// ---------- moving and resizing ----------

	/** Ends the drag or resize under way: commit false undoes a drag (cancelled pointer, lost capture), gone when the window is going away. */
	let endTrack: ((commit: boolean, gone?: boolean) => void) | null = null;
	// A window removed mid-drag (poll, offline) must not leave a drop preview or a key listener behind.
	$effect(() => () => endTrack?.(false, true));

	function track(e: PointerEvent, kind: 'drag' | 'resize', onMove: (ev: PointerEvent) => void, onEnd?: (commit: boolean, gone: boolean) => void) {
		endTrack?.(false);
		const target = e.currentTarget as HTMLElement;
		try {
			target.setPointerCapture(e.pointerId);
		} catch {
			// Capture is optional.
		}
		busy = kind;
		const move = (ev: PointerEvent) => onMove(ev);
		const end = (commit: boolean, gone = false) => {
			if (endTrack !== end) return;
			endTrack = null;
			target.removeEventListener('pointermove', move);
			target.removeEventListener('pointerup', up);
			target.removeEventListener('pointercancel', cancel);
			target.removeEventListener('lostpointercapture', cancel);
			busy = '';
			onEnd?.(commit, gone);
			// A closed window's place is gone for good: saving would bring it back.
			if (!gone) win.save();
		};
		const up = () => end(true);
		const cancel = () => end(false);
		endTrack = end;
		target.addEventListener('pointermove', move);
		target.addEventListener('pointerup', up);
		target.addEventListener('pointercancel', cancel);
		target.addEventListener('lostpointercapture', cancel);
	}

	function startDrag(e: PointerEvent) {
		if (e.button !== 0 || (e.target as HTMLElement).closest('button, input')) return;
		e.preventDefault();
		app.focus(win);
		const sx = e.clientX;
		const sy = e.clientY;
		let ox = win.x;
		let oy = win.y;
		let moved = false;
		let cancelled = false;
		let zone: Zone | null = null;
		const desk = (e.currentTarget as HTMLElement).closest('[data-desktop]')!.getBoundingClientRect();
		// Docked windows are placed by dropping; floating ones just move.
		let docking = false;
		// A grab near the desk border is not yet a drop on that border.
		let armed = false;
		// Escape, a cancelled pointer or a lost capture: back to where it was, nothing placed.
		const abort = (gone: boolean) => {
			cancelled = true;
			zone = null;
			app.dropTab = null;
			app.clearDrop();
			if (gone || !moved) return;
			if (docking) app.retile();
			else win.place(ox, oy, win.w, win.h);
		};
		const cancel = (ev: KeyboardEvent) => {
			if (ev.key !== 'Escape' || !moved || cancelled) return;
			ev.preventDefault();
			ev.stopPropagation();
			abort(false);
		};
		window.addEventListener('keydown', cancel, true);
		track(
			e,
			'drag',
			(ev) => {
				if (cancelled) return;
				if (!moved) {
					// A click on the title bar is not a drag.
					if (Math.abs(ev.clientX - sx) + Math.abs(ev.clientY - sy) < 5) return;
					moved = true;
					if (win.max) {
						// Dragging a maximized window restores it under the pointer.
						const frac = (sx - desk.left) / desk.width;
						app.toggleMax(win, false);
						ox = sx - desk.left - win.w * frac;
						oy = 0;
					}
					docking = app.autoDock;
				}
				armed ||= Math.hypot(ev.clientX - sx, ev.clientY - sy) > 24;
				win.place(ox + ev.clientX - sx, oy + ev.clientY - sy, win.w, win.h);
				// Over a tab: the window moves there on release.
				const tabEl = document
					.elementsFromPoint(ev.clientX, ev.clientY)
					.map((el) => (el as HTMLElement).closest<HTMLElement>('[data-tab-id]'))
					.find(Boolean);
				app.dropTab = tabEl && tabEl.dataset.tabId !== win.tab ? (tabEl.dataset.tabId ?? null) : null;
				zone = null;
				if (docking && !app.dropTab) {
					const z = app.zoneAt(ev.clientX - desk.left, ev.clientY - desk.top, win.id);
					zone = z && (armed || z.kind !== 'edge') ? z : null;
				}
				app.previewDrop(win.id, zone);
			},
			(commit, gone) => {
				window.removeEventListener('keydown', cancel, true);
				if (cancelled) {
					app.clearDrop();
					return;
				}
				if (!commit) {
					abort(gone);
					return;
				}
				if (moved && app.dropTab) {
					const to = app.dropTab;
					app.dropTab = null;
					app.clearDrop();
					app.moveToTab(win, to);
					return;
				}
				if (!moved || !docking) {
					app.clearDrop();
					return;
				}
				app.land(win, zone);
			}
		);
	}

	function startResize(e: PointerEvent, dir: string) {
		if (e.button !== 0) return;
		e.preventDefault();
		e.stopPropagation();
		app.focus(win);
		const sx = e.clientX;
		const sy = e.clientY;
		// Tiles share their splits, as in a tiling window manager: the edge
		// moves, the neighbour gives way, nothing ever overlaps.
		if (tiled) {
			const h = dir.includes('e') ? 'e' : dir.includes('w') ? 'w' : null;
			const v = dir.includes('s') ? 's' : dir.includes('n') ? 'n' : null;
			const hSplit = h && app.split(win, h);
			const vSplit = v && app.split(win, v);
			if (hSplit || vSplit) {
				track(e, 'resize', (ev) => {
					if (hSplit) hSplit(ev.clientX - sx);
					if (vSplit) vSplit(ev.clientY - sy);
				});
			}
			return;
		}
		// In Free mode a window takes the size it is given.
		const o = { x: win.x, y: win.y, w: win.w, h: win.h };
		track(e, 'resize', (ev) => {
			const dx = ev.clientX - sx;
			const dy = ev.clientY - sy;
			let { x, y, w, h } = o;
			if (dir.includes('e')) w = o.w + dx;
			if (dir.includes('s')) h = o.h + dy;
			if (dir.includes('w')) {
				w = Math.max(MIN_W, o.w - dx);
				x = o.x + o.w - w;
			}
			if (dir.includes('n')) {
				h = Math.max(MIN_H, o.h - dy);
				y = o.y + o.h - h;
				if (y < 0) {
					h += y;
					y = 0;
				}
			}
			win.place(x, y, w, h);
		});
	}

	const grips: { dir: string; cls: string }[] = [
		{ dir: 'n', cls: '-top-1 inset-x-2.5 h-2 cursor-ns-resize' },
		{ dir: 's', cls: '-bottom-1 inset-x-2.5 h-2 cursor-ns-resize' },
		{ dir: 'e', cls: '-right-1 inset-y-2.5 w-2 cursor-ew-resize' },
		{ dir: 'w', cls: '-left-1 inset-y-2.5 w-2 cursor-ew-resize' },
		{ dir: 'ne', cls: '-top-1.5 -right-1.5 size-4 cursor-nesw-resize z-[6]' },
		{ dir: 'sw', cls: '-bottom-1.5 -left-1.5 size-4 cursor-nesw-resize z-[6]' },
		{ dir: 'nw', cls: '-top-1.5 -left-1.5 size-4 cursor-nwse-resize z-[6]' },
		{ dir: 'se', cls: '-right-1.5 -bottom-1.5 size-4 cursor-nwse-resize z-[6]' }
	];

	// ---------- renaming ----------

	let renaming = $state(false);
	let draft = $state('');

	function startRename() {
		draft = info.renameTo || info.name;
		renaming = true;
	}

	async function commitRename() {
		if (!renaming) return;
		const name = draft.trim();
		renaming = false;
		if (name && name !== (info.renameTo || info.name)) await app.renameWindow(win, name);
	}

	function focusSelect(node: HTMLInputElement) {
		node.focus();
		node.select();
	}

	/** Closing a window always asks: terminate the session, or only detach. */
	async function askStop() {
		if (info.exited && !win.attached) {
			// Nothing runs any more: closing just removes the window.
			app.stop(win);
			return;
		}
		if (win.ssh) {
			const choice = await app.ask('Close this window?', 'Disconnect ends the SSH connection. Hide keeps it running in the sidebar.', [
				{ label: 'Disconnect', value: 'terminate', tone: 'danger' },
				{ label: 'Hide', value: 'hide', tone: 'secondary' }
			]);
			if (choice === 'terminate') app.terminate(win);
			else if (choice === 'hide') app.hide(win);
			return;
		}
		const agent = app.agentDisplay(info.agent);
		const choice = await app.ask(
			'Close this window?',
			win.attached
				? `Terminate stops ${agent} and its container; the exitbox run in its terminal ends too. Detach closes this window and leaves the session running in its terminal.`
				: `Terminate ends ${agent} and stops its container; a named session can be resumed later from the sidebar. Detach closes this window and keeps the session running in the sidebar.`,
			[
				{ label: 'Terminate session', value: 'terminate', tone: 'danger' },
				{ label: 'Detach', value: 'detach', tone: 'secondary' }
			]
		);
		if (choice === 'terminate') app.terminate(win);
		else if (choice === 'detach') detach(win);
	}

	/** The title bar's session menu, at the pointer. */
	let menu = $state<{ x: number; y: number; anchor: HTMLElement; back: HTMLElement | null } | null>(null);

	function openMenu(e: MouseEvent) {
		// The rename field keeps the browser's own menu, for paste.
		if ((e.target as HTMLElement).closest('input')) return;
		if (win.busy) {
			e.preventDefault();
			return;
		}
		e.preventDefault();
		const el = e.currentTarget as HTMLElement;
		const at = !e.clientX && !e.clientY ? el.getBoundingClientRect() : null;
		menu = { x: at ? at.left + 40 : e.clientX, y: at ? at.bottom : e.clientY, anchor: el, back: term?.textarea ?? null };
	}
</script>

<section
	data-win-id={win.id}
	aria-label={win.title}
	onpointerdown={() => app.focus(win)}
	style:z-index={win.max ? 5000 + win.z : win.z}
	style:left={win.max ? '0px' : win.x + 'px'}
	style:top={win.max ? '0px' : win.y + 'px'}
	style:width={win.max ? '100vw' : win.w + 'px'}
	style:height={win.max ? '100dvh' : win.h + 'px'}
	class={[
		'flex flex-col border bg-surface-1',
		// Maximized covers the whole page, sidebar and bars included.
		win.max ? 'fixed rounded-none' : 'absolute rounded-xl',
		// Like an inactive-opacity setting: the window you type into stands out.
		focused ? 'border-brand shadow-float ring-1 ring-brand' : 'border-line-1 opacity-60 shadow-panel saturate-50 hover:opacity-100 hover:saturate-100',
		!busy && tiled ? 'transition-[left,top,width,height,opacity] duration-150' : 'transition-opacity duration-150',
		(win.min || win.tab !== app.activeTab) && 'hidden',
		app.shown !== win && 'max-md:hidden',
		// A phone shows the focused session edge to edge, without window chrome.
		'max-md:!absolute max-md:!inset-0 max-md:!size-full max-md:rounded-none max-md:border-0 max-md:opacity-100 max-md:shadow-none max-md:ring-0 max-md:saturate-100 max-md:transition-none'
	]}
>
	<!-- The title bar is a pointer handle; its buttons cover keyboard use. -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<header
		onpointerdown={startDrag}
		ondblclick={(e) => {
			if (!win.busy && !(e.target as HTMLElement).closest('button')) app.toggleMax(win);
		}}
		oncontextmenu={openMenu}
		class={[
			// A container: narrow tiles drop the badges first, never the window buttons.
			'@container flex h-[42px] shrink-0 touch-none items-center gap-2 overflow-hidden border-b pr-1 pl-3 max-md:hidden',
			focused ? 'border-line-2 bg-surface-3' : 'border-line-1 bg-surface-1 text-ink-2',
			win.max ? 'rounded-none' : 'rounded-t-[11px] max-md:rounded-none',
			busy === 'drag' ? 'cursor-grabbing' : 'cursor-grab'
		]}
	>
		<span class="shrink-0 font-mono text-[12px] text-ink-2">{number}</span>
		{#if renaming}
			<input
				bind:value={draft}
				{@attach focusSelect}
				aria-label="New session name"
				spellcheck="false"
				onpointerdown={(e) => e.stopPropagation()}
				onkeydown={(e) => {
					if (e.key === 'Enter') {
						e.preventDefault();
						commitRename();
					} else if (e.key === 'Escape') {
						e.preventDefault();
						renaming = false;
					}
				}}
				onblur={commitRename}
				class="h-[28px] min-w-0 flex-[0_1_240px] rounded border border-brand bg-surface-0 px-1.5 text-[16px] font-semibold outline-none"
			/>
		{:else}
			<span class="min-w-0 truncate text-[16px] font-semibold" title={info.renameTo ? `Renames to "${info.renameTo}" when it ends` : undefined}>{win.title}</span>
			{#if info.name && !(win.attached && info.exited)}
				<span class="hidden shrink-0 @xs:block"><IconButton icon="pencil" label="Rename session" small disabled={win.busy} onclick={() => startRename()} /></span>
			{/if}
		{/if}
		{#if win.ssh}
			<span class="hidden shrink-0 items-center gap-1.5 rounded border border-line-2 bg-surface-2 py-0.5 pr-2 pl-1 text-[11.5px] font-semibold whitespace-nowrap @lg:inline-flex"
				><AgentIcon name="ssh" small />SSH</span
			>
			<span class="min-w-0 flex-1 truncate font-mono text-[11px] text-ink-2">{info.ssh ? sshTarget(info.ssh) : ''}</span>
		{:else}
			<span class="hidden min-w-0 truncate rounded border border-line-2 bg-surface-2 px-2 py-0.5 text-[11.5px] font-semibold whitespace-nowrap @lg:inline">{app.agentDisplay(info.agent)}</span>
			<span class="min-w-0 flex-1 truncate font-mono text-[11px] text-ink-2" title={info.dir}>{app.tilde(info.dir)}</span>
		{/if}
		<StatusPill text={status.text} tone={status.tone} class="hidden @sm:inline" />
		<div class="ml-auto flex shrink-0 items-center gap-2">
			{#if waiting.length}
				<Chip class="border-amber bg-amber-soft text-amber" aria-label={`Show ${waiting[0].title}, waiting for your answer`} title="Another window waits for your answer" onclick={showWaiting}
					><span class="size-1.5 rounded-full bg-amber ring-2 ring-amber/40"></span>Approval waiting</Chip
				>
			{/if}
			{#if win.rebuildable}
				<IconButton icon="hammer" label="Rebuild image" disabled={win.busy} onclick={() => void confirmRebuild(win)} />
			{/if}
			<IconButton icon="settings" label="Session settings" disabled={win.busy} onclick={() => (app.sessionSettings = win)} />
			<IconButton icon="minus" label="Hide window" disabled={win.busy} onclick={() => app.hide(win)} />
			<IconButton icon={win.max ? 'restore' : 'maximize'} label={win.max ? 'Restore size' : 'Maximize'} disabled={win.busy} onclick={() => app.toggleMax(win)} />
			<IconButton icon="x" label="Close window" disabled={win.busy} onclick={askStop} />
		</div>
	</header>

	<div
		class={[
			'relative min-h-0 flex-1 overflow-hidden bg-inset pt-1.5 pr-1 pb-1 pl-2 outline-none',
			win.max ? 'rounded-none' : 'rounded-b-[11px] max-md:rounded-none',
			busy && 'pointer-events-none select-none'
		]}
		role="group"
		aria-label="Session"
		tabindex="-1"
		{@attach keyHold}
	>
		<div class="size-full" {@attach terminal}></div>
		{#if win.prompts.length && !info.exited}
			<!-- Fades the session so the card stands out; the card has the keyboard anyway. -->
			<div class="absolute inset-0 z-[5] bg-black/55 backdrop-blur-[1.5px]"></div>
			<!-- Bottom up, oldest lowest: a new card lands above, never under the pointer. -->
			<div
				class="absolute inset-x-3 bottom-3 z-[6] flex max-h-[calc(100%-24px)] flex-col-reverse gap-2 overflow-y-auto max-md:inset-x-[8px] max-md:bottom-[8px] max-md:max-h-[calc(100%-16px)]"
			>
				{#each win.prompts as p, i (p.id)}
					<PromptCard
						prompt={p}
						agent={app.agentDisplay(p.agent)}
						slot={i}
						focus={p.id === keyPrompt && focused && !win.min && !win.busy}
						onanswer={(allow, password) => win.answer?.(p.id, allow, password) ?? false}
					/>
				{/each}
			</div>
		{/if}
		{#if held && !info.exited}
			<div role="status" class="pointer-events-none absolute inset-x-3 bottom-3 z-[5] flex justify-center">
				<span class="rounded-lg border border-line-2 bg-surface-1 px-3 py-2 text-[12.5px] shadow-panel">Typing paused. Click or press a key to type here.</span>
			</div>
		{/if}
		{#if info.exited}
			<div class="absolute inset-x-3 bottom-3 z-[5]">
				<div
					role="status"
					class={[
						'flex flex-wrap items-center gap-2 rounded-lg border bg-surface-1 px-3 py-2.5 text-[12.5px] shadow-panel',
						info.exitCode === 0 ? 'border-line-2' : 'border-warn'
					]}
				>
					<span class="flex-[1_1_160px]">
						{#if win.attached}
							Detached. The session may still run in its terminal.
						{:else if win.ssh}
							{info.exitCode === 0 ? 'The connection closed.' : `The connection closed with exit code ${info.exitCode}.`}
						{:else}
							{info.exitCode === 0 ? 'The session ended.' : `The session ended with exit code ${info.exitCode}.`}
						{/if}
						{#if info.renameError}<span class="block text-warn">Rename failed: {info.renameError}</span>{/if}
					</span>
					<Button size="sm" icon="rotate" onclick={() => app.restart(win, cols, rows)}>{win.attached ? 'Attach again' : win.ssh ? 'Reconnect' : 'Start again'}</Button>
					<Button size="sm" variant="ghost" onclick={() => app.stop(win)}>Close</Button>
				</div>
			</div>
		{/if}
	</div>

	{#if win.busy}
		<!-- Covers the title bar too: nothing in the window acts until the session is gone or back. -->
		<div
			role="status"
			class={[
				'absolute inset-0 z-[8] grid place-items-center bg-black/55 backdrop-blur-[1.5px] transition-opacity duration-200 starting:opacity-0',
				win.max ? 'rounded-none' : 'rounded-xl max-md:rounded-none'
			]}
		>
			<span class="flex items-center gap-2.5 rounded-lg border border-line-2 bg-surface-1 px-4 py-2.5 text-[13.5px] font-semibold shadow-float">
				<Spinner size={16} />{win.rebuilding ? 'Rebuilding' : win.ssh ? 'Disconnecting' : 'Terminating session'}
			</span>
		</div>
	{/if}

	{#if menu && !win.busy}
		<SessionMenu
			target={{ kind: 'window', win }}
			x={menu.x}
			y={menu.y}
			anchor={menu.anchor}
			returnFocus={menu.back}
			inWindow
			onrename={info.name && !(win.attached && info.exited) ? startRename : undefined}
			onclose={() => (menu = null)}
		/>
	{/if}

	{#if !win.max}
		{#each grips.filter((g) => !tiled || splitSides.has(g.dir)) as g (g.dir)}
			<div aria-hidden="true" class={['absolute z-[4] touch-none max-md:hidden', g.cls]} onpointerdown={(e) => startResize(e, g.dir)}></div>
		{/each}
	{/if}
</section>
