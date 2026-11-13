<script lang="ts">
	import { untrack, type Snippet } from 'svelte';
	import { viewport } from '../viewport.svelte';

	let {
		label,
		onclose,
		wide = false,
		sheet = false,
		children
	}: {
		label: string;
		onclose: () => void;
		wide?: boolean;
		/** On a phone: a sheet at the bottom instead of a full-screen page. */
		sheet?: boolean;
		children: Snippet;
	} = $props();

	// Focus the first control when the dialog opens.
	function autofocus(node: HTMLElement) {
		const prev = document.activeElement as HTMLElement | null;
		// A selector list matches in document order, so the marked control is looked up on its own.
		const first = node.querySelector<HTMLElement>('[data-autofocus]') ?? node.querySelector<HTMLElement>('input, button.primary-action, button');
		// A focused field on a phone opens the on-screen keyboard; untracked, so a viewport flip never reruns this mid-typing.
		if (untrack(() => viewport.phone) && first?.matches('input, textarea, select')) node.focus();
		else first?.focus();
		return () => prev?.focus?.();
	}

	const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

	// Tab and Shift+Tab cycle inside the dialog, so focus never reaches the page behind it.
	function trapTab(e: KeyboardEvent) {
		if (e.key !== 'Tab') return;
		const node = e.currentTarget as HTMLElement;
		const list = [...node.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => el.getClientRects().length > 0);
		if (!list.length) {
			e.preventDefault();
			node.focus();
			return;
		}
		const first = list[0];
		const last = list[list.length - 1];
		const at = document.activeElement;
		if (e.shiftKey && (at === first || at === node || !node.contains(at))) {
			e.preventDefault();
			last.focus();
		} else if (!e.shiftKey && (at === last || !node.contains(at))) {
			e.preventDefault();
			first.focus();
		}
	}
</script>

<div
	class={['fixed inset-0 z-[10000] grid place-items-center bg-black/50 p-4', sheet ? 'max-md:items-end max-md:p-0' : 'max-md:place-items-stretch max-md:p-0']}
	role="presentation"
	onpointerdown={(e) => {
		if (!viewport.phone && e.target === e.currentTarget) onclose();
	}}
	onclick={(e) => {
		// On a phone the whole tap ends on the backdrop: closing at pointerdown would let the click act on the page below.
		if (viewport.phone && e.target === e.currentTarget) onclose();
	}}
	onkeydown={(e) => {
		if (e.key === 'Escape') {
			e.stopPropagation();
			onclose();
		}
	}}
>
	<div
		role="dialog"
		aria-modal="true"
		aria-label={label}
		tabindex="-1"
		{@attach autofocus}
		onkeydown={trapTab}
		style:height={viewport.phone && !sheet && viewport.height ? viewport.height + 'px' : undefined}
		class={[
			'flex max-h-[calc(100vh-32px)] w-full flex-col overflow-hidden rounded-xl border border-line-1 bg-surface-1 shadow-float',
			wide ? 'max-w-[880px]' : 'max-w-[460px]',
			'max-md:max-w-none max-md:outline-none',
			sheet
				? 'max-md:max-h-[90dvh] max-md:rounded-b-none max-md:border-x-0 max-md:border-b-0 max-md:pb-[env(safe-area-inset-bottom)]'
				: 'max-md:h-dvh max-md:max-h-none max-md:rounded-none max-md:border-0 max-md:pt-[env(safe-area-inset-top)] max-md:pb-[env(safe-area-inset-bottom)] max-md:shadow-none'
		]}
	>
		{@render children()}
	</div>
</div>
