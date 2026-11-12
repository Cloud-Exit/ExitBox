<script lang="ts">
	import { untrack } from 'svelte';
	import { Terminal } from '@xterm/xterm';
	import { FitAddon } from '@xterm/addon-fit';
	import { app, MIN_H, MIN_W, type Win } from './app.svelte';
	import Button from './ui/Button.svelte';
	import IconButton from './ui/IconButton.svelte';

	let { win, number }: { win: Win; number: number } = $props();

	let busy = $state<'' | 'drag' | 'resize'>('');
	let term: Terminal | null = null;
	let cols = 120;
	let rows = 36;

	const focused = $derived(app.focusedId === win.id);
	/** Tiled by the dock layout: resizing moves splits, dragging reorders. */
	const tiled = $derived(app.autoDock && !win.max);
	/** The edges of a tile that border a neighbour; only they resize. */
	const splitSides = $derived.by(() => {
		if (!tiled) return new Set<string>();
		// Recompute when the tiles move.
		void [win.x, win.y, win.w, win.h, app.windows.length, app.dockLayout];
		const sides = (['n', 's', 'e', 'w'] as const).filter((d) => app.split(win, d) !== null);
		const set = new Set<string>(sides);
		for (const c of ['ne', 'nw', 'se', 'sw']) if (set.has(c[0]) && set.has(c[1])) set.add(c);
		return set;
	});
	const info = $derived(win.info);
	const status = $derived.by(() => {
		if (info.exited && win.attached) return { text: 'detached', tone: 'off' };
		if (info.exited) return info.exitCode === 0 ? { text: 'ended', tone: 'off' } : { text: 'exit ' + info.exitCode, tone: 'bad' };
		if (!win.connected) return { text: 'connecting', tone: 'wait' };
		return win.attached ? { text: 'terminal', tone: 'run' } : { text: 'running', tone: 'run' };
	});

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

	// Focus the terminal when this window becomes the focused one.
	$effect(() => {
		if (focused && !win.min) term?.focus();
	});

	/**
	 * Mounts xterm and keeps a WebSocket to the session. Output arrives as
	 * binary frames; keys and resizes go out as JSON text frames.
	 */
	function terminal(node: HTMLElement) {
		// Runs once per window: nothing read here may re-run the attachment.
		return untrack(() => mountTerminal(node));
	}

	function mountTerminal(node: HTMLElement) {
		const t = new Terminal({
			fontFamily: "'Atkinson Hyperlegible Mono', ui-monospace, SFMono-Regular, Menlo, monospace",
			fontSize: 13,
			lineHeight: 1.15,
			cursorBlink: true,
			scrollback: 5000,
			theme: termTheme()
		});
		const fit = new FitAddon();
		t.loadAddon(fit);
		t.open(node);
		term = t;

		let ws: WebSocket | null = null;
		let disposed = false;
		let retry = 0;
		let lastSize = '';
		let timer: ReturnType<typeof setTimeout> | null = null;

		const send = (msg: object) => {
			if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg));
		};

		const fitNow = () => {
			if (disposed || win.min || app.view !== 'sessions' || !node.clientWidth) return;
			try {
				fit.fit();
			} catch {
				return;
			}
			cols = t.cols;
			rows = t.rows;
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
						if (msg?.t === 'exit') app.ended(win, msg.code);
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
				t.write(new Uint8Array(ev.data));
			};
			sock.onclose = () => {
				if (ws !== sock) return;
				win.connected = false;
				if (disposed || win.info.exited) return;
				timer = setTimeout(connect, Math.min(5000, 500 * 2 ** retry++));
			};
		};

		const dataSub = t.onData((d) => send({ t: 'i', d }));
		// xterm sends Shift+Enter as a plain carriage return, so the agent
		// would submit. Send a line feed instead (what Ctrl+J sends): Claude
		// Code, Codex and OpenCode read it as a newline in the prompt.
		t.attachCustomKeyEventHandler((ev) => {
			if (ev.key !== 'Enter' || !ev.shiftKey || ev.ctrlKey || ev.altKey || ev.metaKey) return true;
			ev.preventDefault();
			if (ev.type === 'keydown') send({ t: 'i', d: '\n' });
			return false;
		});
		const ro = new ResizeObserver(fitSoon);
		ro.observe(node);
		const fontsDone = () => {
			// Make xterm measure the font again once it is there.
			t.options.fontFamily = 'monospace';
			t.options.fontFamily = "'Atkinson Hyperlegible Mono', ui-monospace, SFMono-Regular, Menlo, monospace";
			fitSoon();
		};
		document.fonts?.ready.then(fontsDone);
		connect();

		return () => {
			disposed = true;
			if (timer) clearTimeout(timer);
			ro.disconnect();
			dataSub.dispose();
			const s = ws;
			ws = null;
			s?.close();
			t.dispose();
			term = null;
		};
	}

	// ---------- moving and resizing ----------

	function track(e: PointerEvent, kind: 'drag' | 'resize', onMove: (ev: PointerEvent) => void, onEnd?: () => void) {
		const target = e.currentTarget as HTMLElement;
		try {
			target.setPointerCapture(e.pointerId);
		} catch {
			// Capture is optional.
		}
		busy = kind;
		const move = (ev: PointerEvent) => onMove(ev);
		const up = () => {
			target.removeEventListener('pointermove', move);
			target.removeEventListener('pointerup', up);
			target.removeEventListener('pointercancel', up);
			busy = '';
			onEnd?.();
			win.save();
		};
		target.addEventListener('pointermove', move);
		target.addEventListener('pointerup', up);
		target.addEventListener('pointercancel', up);
	}

	/**
	 * The slot a dragged tiled window lands in: the tile under the pointer,
	 * or the nearest one when the pointer is in a gap.
	 * Its own slot means it stays put.
	 */
	function slotAt(x: number, y: number, desk: DOMRect) {
		const px = x - desk.left;
		const py = y - desk.top;
		let best: (typeof app.windows)[number] | null = null;
		let bestD = Infinity;
		for (const w of app.windows) {
			if (w.min || w.max) continue;
			// The dragged window's slot is where it was before the drag.
			const r = w === win ? home : { x: w.x, y: w.y, w: w.w, h: w.h };
			const inside = px >= r.x && px <= r.x + r.w && py >= r.y && py <= r.y + r.h;
			const d = inside ? -1 : Math.hypot(px - (r.x + r.w / 2), py - (r.y + r.h / 2));
			if (d < bestD) {
				bestD = d;
				best = w;
			}
		}
		return best;
	}
	let home = { x: 0, y: 0, w: 0, h: 0 };

	function startDrag(e: PointerEvent) {
		if (e.button !== 0 || (e.target as HTMLElement).closest('button, input')) return;
		e.preventDefault();
		app.focus(win);
		const sx = e.clientX;
		const sy = e.clientY;
		let ox = win.x;
		let oy = win.y;
		let moved = false;
		const desk = (e.currentTarget as HTMLElement).closest('[data-desktop]')!.getBoundingClientRect();
		// Docked windows are rearranged by dragging; floating ones just move.
		let docking = false;
		track(
			e,
			'drag',
			(ev) => {
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
					home = { x: win.x, y: win.y, w: win.w, h: win.h };
				}
				win.place(ox + ev.clientX - sx, oy + ev.clientY - sy, win.w, win.h);
				// Over a tab: the window moves there on release.
				const tabEl = document
					.elementsFromPoint(ev.clientX, ev.clientY)
					.map((el) => (el as HTMLElement).closest<HTMLElement>('[data-tab-id]'))
					.find(Boolean);
				app.dropTab = tabEl && tabEl.dataset.tabId !== win.tab ? (tabEl.dataset.tabId ?? null) : null;
				if (app.dropTab) {
					app.dropTarget = null;
					return;
				}
				if (docking) {
					const slot = slotAt(ev.clientX, ev.clientY, desk);
					app.dropTarget = slot && slot !== win ? slot.id : null;
				}
			},
			() => {
				if (moved && app.dropTab) {
					const to = app.dropTab;
					app.dropTab = null;
					app.dropTarget = null;
					app.moveToTab(win, to);
					return;
				}
				if (!moved || !docking) return;
				const target = app.windows.find((w) => w.id === app.dropTarget);
				app.dropTarget = null;
				if (target) app.swapDock(win, target);
				else app.retile();
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

	async function askStop() {
		if (win.attached) {
			// Detaching leaves the session running in its terminal.
			app.stop(win);
			return;
		}
		if (info.exited) {
			app.stop(win);
			return;
		}
		const choice = await app.ask(
			'Stop this session?',
			`Stopping ends ${app.agentDisplay(info.agent)} and its container. Named sessions can be resumed later from the sidebar. Hiding keeps it running.`,
			[
				{ label: 'Stop session', value: 'stop', tone: 'danger' },
				{ label: 'Hide window', value: 'hide', tone: 'secondary' }
			]
		);
		if (choice === 'stop') app.stop(win);
		else if (choice === 'hide') app.hide(win);
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
		app.dropTarget === win.id && 'outline-2 outline-offset-2 outline-accent',
		!busy && tiled ? 'transition-[left,top,width,height,opacity] duration-150' : 'transition-opacity duration-150',
		(win.min || win.tab !== app.activeTab) && 'hidden',
		!focused && 'max-md:hidden',
		'max-md:!inset-0 max-md:!size-full max-md:rounded-none'
	]}
>
	<!-- The title bar is a pointer handle; its buttons cover keyboard use. -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<header
		onpointerdown={startDrag}
		ondblclick={(e) => {
			if (!(e.target as HTMLElement).closest('button')) app.toggleMax(win);
		}}
		class={[
			'flex h-9 shrink-0 touch-none items-center gap-2 overflow-hidden border-b pr-1 pl-3',
			focused ? 'border-line-2 bg-surface-3' : 'border-line-1 bg-surface-1 text-ink-2',
			win.max ? 'rounded-none' : 'rounded-t-[11px] max-md:rounded-none',
			busy === 'drag' ? 'cursor-grabbing' : 'cursor-grab'
		]}
	>
		<span class="font-mono text-[10.5px] text-ink-2">{number}</span>
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
				class="h-6 min-w-0 flex-[0_1_220px] rounded border border-brand bg-surface-0 px-1.5 text-[13px] outline-none"
			/>
		{:else}
			<span class="min-w-0 truncate text-[13px] font-semibold" title={info.renameTo ? `Renames to "${info.renameTo}" when it ends` : undefined}>{win.title}</span>
			{#if !win.attached && info.name}
				<IconButton icon="pencil" label="Rename session" small onclick={() => startRename()} />
			{/if}
		{/if}
		<span class="shrink-0 rounded border border-line-2 bg-surface-2 px-2 py-0.5 text-[11.5px] font-semibold whitespace-nowrap">{app.agentDisplay(info.agent)}</span>
		<span class="min-w-0 flex-1 truncate font-mono text-[11px] text-ink-2" title={info.dir}>{app.tilde(info.dir)}</span>
		<span
			class={[
				'rounded px-[7px] py-0.5 text-[10.5px] font-semibold tracking-[0.03em] whitespace-nowrap uppercase',
				status.tone === 'run' && 'bg-brand-soft text-brand',
				status.tone === 'wait' && 'bg-amber-soft text-amber',
				status.tone === 'off' && 'bg-surface-2 text-ink-2',
				status.tone === 'bad' && 'bg-warn-soft text-warn'
			]}>{status.text}</span
		>
		<IconButton icon="minus" label="Hide window" onclick={() => app.hide(win)} />
		<IconButton icon={win.max ? 'restore' : 'maximize'} label={win.max ? 'Restore size' : 'Maximize'} onclick={() => app.toggleMax(win)} />
		<IconButton icon="x" label={win.attached ? 'Detach (keeps running in its terminal)' : 'Stop session'} onclick={askStop} />
	</header>

	<div
		class={[
			'relative min-h-0 flex-1 overflow-hidden bg-inset pt-1.5 pr-1 pb-1 pl-2',
			win.max ? 'rounded-none' : 'rounded-b-[11px] max-md:rounded-none',
			busy && 'pointer-events-none select-none'
		]}
	>
		<div class="size-full" {@attach terminal}></div>
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
						{:else}
							{info.exitCode === 0 ? 'The session ended.' : `The session ended with exit code ${info.exitCode}.`}
						{/if}
						{#if info.renameError}<span class="block text-warn">Rename failed: {info.renameError}</span>{/if}
					</span>
					<Button size="sm" icon="rotate" onclick={() => app.restart(win, cols, rows)}>{win.attached ? 'Attach again' : 'Start again'}</Button>
					<Button size="sm" variant="ghost" onclick={() => app.stop(win)}>Close</Button>
				</div>
			</div>
		{/if}
	</div>

	{#if !win.max}
		{#each grips.filter((g) => !tiled || splitSides.has(g.dir)) as g (g.dir)}
			<div aria-hidden="true" class={['absolute z-[4] touch-none max-md:hidden', g.cls]} onpointerdown={(e) => startResize(e, g.dir)}></div>
		{/each}
	{/if}
</section>
