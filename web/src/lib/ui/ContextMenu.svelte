<script module lang="ts">
	import type { IconName } from './icons';

	export type MenuItem =
		| { type?: 'item'; label: string; icon?: IconName; onselect: () => void; disabled?: boolean; danger?: boolean; checked?: boolean }
		| { type: 'separator' }
		/** Expands in place, so it works the same with touch, mouse and keys. */
		| { type: 'group'; label: string; icon?: IconName; items: MenuItem[] };
</script>

<script lang="ts">
	import { tick } from 'svelte';
	import { viewport } from '../viewport.svelte';
	import Icon from './Icon.svelte';

	let {
		items,
		x,
		y,
		label,
		anchor = null,
		returnFocus = null,
		keyboard = false,
		expand = null,
		onclose
	}: {
		items: MenuItem[];
		/** Where it opens, in client pixels; it moves to stay in the viewport. */
		x: number;
		y: number;
		label: string;
		/** What it belongs to: scrolling a box around it closes the menu. */
		anchor?: HTMLElement | null;
		returnFocus?: HTMLElement | null;
		/** Opened from the keyboard: the first item takes focus. */
		keyboard?: boolean;
		/** A group shown expanded from the start. */
		expand?: string | null;
		onclose: () => void;
	} = $props();

	// svelte-ignore state_referenced_locally
	let expanded = $state<string | null>(expand);
	let closed = false;
	/** A press began on the phone's backdrop: the click that ends the opening long press must not close the menu. */
	let scrimDown = false;

	function close(refocus = true) {
		if (closed) return;
		closed = true;
		// Read before onclose: the parent's props may be gone after it.
		const back = refocus ? returnFocus : null;
		onclose();
		back?.focus({ preventScroll: true });
	}

	function activate(it: { onselect: () => void }) {
		// Focus goes back first, so an action that focuses something (a rename field, a dialog) keeps it.
		close();
		it.onselect();
	}

	const focusables = (node: HTMLElement) => [...node.querySelectorAll<HTMLElement>('[data-mi]:not([disabled])')];

	/** Lives on document.body: a filtered or transformed ancestor would offset a fixed box. */
	function mount(wrap: HTMLElement) {
		// The empty wrapper stays where Svelte put it, so Svelte can still remove the block.
		document.body.appendChild(wrap);
		const node = wrap.querySelector<HTMLElement>('[role=menu]')!;
		const fit = () => {
			const r = node.getBoundingClientRect();
			node.style.left = Math.max(6, Math.min(x, innerWidth - r.width - 6)) + 'px';
			node.style.top = Math.max(6, Math.min(y, innerHeight - r.height - 6)) + 'px';
		};
		fit();
		const ro = new ResizeObserver(fit);
		ro.observe(node);
		if (keyboard) focusables(node)[0]?.focus({ preventScroll: true });
		else node.focus({ preventScroll: true });

		const outside = (e: Event) => {
			// The phone's backdrop closes on its own click, so the tap cannot land on what is under it.
			if (!wrap.contains(e.target as Node)) close(false);
		};
		const scrolled = (e: Event) => {
			const t = e.target;
			// Only scrolling that moves the anchor; a terminal printing elsewhere does not.
			if (t === document || (t instanceof Node && !node.contains(t) && (!anchor || t.contains(anchor)))) close(false);
		};
		const escape = (e: KeyboardEvent) => {
			if (e.key !== 'Escape') return;
			e.preventDefault();
			e.stopPropagation();
			close();
		};
		const lost = () => close(false);
		// A phone's on-screen keyboard closing resizes the window; only a new width moves the menu's anchor.
		const width = innerWidth;
		const resized = () => {
			if (!viewport.phone || innerWidth !== width) close(false);
		};
		document.addEventListener('pointerdown', outside, true);
		document.addEventListener('scroll', scrolled, true);
		document.addEventListener('keydown', escape, true);
		window.addEventListener('blur', lost);
		window.addEventListener('resize', resized);
		return () => {
			wrap.remove();
			ro.disconnect();
			document.removeEventListener('pointerdown', outside, true);
			document.removeEventListener('scroll', scrolled, true);
			document.removeEventListener('keydown', escape, true);
			window.removeEventListener('blur', lost);
			window.removeEventListener('resize', resized);
		};
	}

	async function toggle(group: string, focusFirst: boolean) {
		expanded = expanded === group ? null : group;
		if (!focusFirst || expanded !== group) return;
		await tick();
		document.querySelector<HTMLElement>(`[data-mi][data-in="${CSS.escape(group)}"]:not([disabled])`)?.focus();
	}

	function onkeydown(e: KeyboardEvent) {
		const node = e.currentTarget as HTMLElement;
		const els = focusables(node);
		const cur = document.activeElement as HTMLElement | null;
		const i = cur ? els.indexOf(cur) : -1;
		const group = cur?.dataset.group;
		const inside = cur?.dataset.in;
		if (e.key === 'ArrowDown') els[(i + 1) % els.length]?.focus();
		else if (e.key === 'ArrowUp') els[(i - 1 + els.length) % els.length]?.focus();
		else if (e.key === 'Home') els[0]?.focus();
		else if (e.key === 'End') els.at(-1)?.focus();
		else if (e.key === 'ArrowRight' && group) {
			if (expanded === group) els[i + 1]?.focus();
			else void toggle(group, true);
		} else if (e.key === 'ArrowLeft' && (inside || (group && expanded === group))) {
			const g = inside ?? group!;
			expanded = null;
			node.querySelector<HTMLElement>(`[data-group="${CSS.escape(g)}"]`)?.focus();
		} else if (e.key === 'Tab') close();
		else return;
		e.preventDefault();
		e.stopPropagation();
	}
</script>

{#snippet list(entries: MenuItem[], parent: string)}
	{#each entries as it, i (i)}
		{#if it.type === 'separator'}
			<div role="separator" class="mx-1 my-1 h-px bg-line-1"></div>
		{:else if it.type === 'group'}
			{@const open = expanded === it.label}
			<button
				type="button"
				role="menuitem"
				aria-haspopup="true"
				aria-expanded={open}
				data-mi
				data-group={it.label}
				onclick={() => toggle(it.label, false)}
				class="flex h-8 w-full shrink-0 items-center gap-2.5 rounded-md px-2 text-left text-[13px] outline-none hover:bg-surface-2 focus:bg-surface-2 max-md:h-[44px] max-md:text-[14px]"
			>
				{#if it.icon}<span class="text-ink-2"><Icon name={it.icon} size={13} /></span>{/if}
				<span class="flex-1 truncate">{it.label}</span>
				<span class="text-ink-3"><Icon name={open ? 'chevronDown' : 'chevronRight'} size={12} /></span>
			</button>
			{#if open}
				<div role="group" aria-label={it.label} class="ml-3 flex flex-col border-l border-line-1 pl-1">
					{@render list(it.items, it.label)}
				</div>
			{/if}
		{:else}
			<button
				type="button"
				role={it.checked === undefined ? 'menuitem' : 'menuitemradio'}
				aria-checked={it.checked}
				disabled={it.disabled}
				data-mi
				data-in={parent || undefined}
				onclick={() => activate(it)}
				class={[
					'flex h-8 w-full shrink-0 items-center gap-2.5 rounded-md px-2 text-left text-[13px] outline-none disabled:cursor-not-allowed disabled:opacity-50 max-md:h-[44px] max-md:text-[14px]',
					it.danger ? 'text-warn enabled:hover:bg-warn-soft focus:bg-warn-soft' : 'enabled:hover:bg-surface-2 focus:bg-surface-2'
				]}
			>
				{#if it.icon}<span class={it.danger ? '' : 'text-ink-2'}><Icon name={it.icon} size={13} /></span>{/if}
				<span class="flex-1 truncate">{it.label}</span>
				{#if it.checked}<span class="text-brand"><Icon name="check" size={12} /></span>{/if}
			</button>
		{/if}
	{/each}
{/snippet}

<div class="hidden">
	<div {@attach mount} class="contents">
		{#if viewport.phone}
			<!-- A phone shows the menu as a sheet; a tap on the dimmed page closes it without acting there. -->
			<div
				aria-hidden="true"
				class="fixed inset-0 z-[9998] bg-black/50"
				onpointerdown={() => (scrimDown = true)}
				onclick={() => {
					if (scrimDown) close(false);
				}}
			></div>
		{/if}
		<div
			role="menu"
			aria-label={label}
			tabindex="-1"
			{onkeydown}
			oncontextmenu={(e) => e.preventDefault()}
			class="fixed top-0 left-0 z-[9999] flex max-h-[calc(100dvh-12px)] w-60 flex-col overflow-auto rounded-lg border border-line-2 bg-surface-1 p-1 text-ink-1 shadow-float outline-none max-md:!top-auto max-md:!bottom-0 max-md:!left-0 max-md:max-h-[80dvh] max-md:w-full max-md:rounded-b-none max-md:border-x-0 max-md:border-b-0 max-md:p-[8px] max-md:pb-[calc(env(safe-area-inset-bottom)+8px)]"
		>
			{#if viewport.phone}<div class="truncate px-2 pt-1 pb-2 text-[13px] font-semibold text-ink-2">{label}</div>{/if}
			{@render list(items, '')}
			{#if viewport.phone}
				<!-- The scrim is hidden from assistive tech: this is the way out it can reach. -->
				<div role="separator" class="mx-1 my-1 h-px bg-line-1"></div>
				<button
					type="button"
					role="menuitem"
					data-mi
					onclick={() => close()}
					class="flex h-[44px] w-full shrink-0 items-center justify-center rounded-md px-2 text-[14px] font-semibold text-ink-2 outline-none focus:bg-surface-2 enabled:hover:bg-surface-2">Cancel</button
				>
			{/if}
		</div>
	</div>
</div>
