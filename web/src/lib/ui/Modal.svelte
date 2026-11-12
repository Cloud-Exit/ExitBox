<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		label,
		onclose,
		wide = false,
		children
	}: { label: string; onclose: () => void; wide?: boolean; children: Snippet } = $props();

	// Focus the first control when the dialog opens.
	function autofocus(node: HTMLElement) {
		const prev = document.activeElement as HTMLElement | null;
		const first = node.querySelector<HTMLElement>('[data-autofocus], input, button.primary-action, button');
		first?.focus();
		return () => prev?.focus?.();
	}
</script>

<div
	class="fixed inset-0 z-[10000] grid place-items-center bg-black/50 p-4"
	role="presentation"
	onpointerdown={(e) => {
		if (e.target === e.currentTarget) onclose();
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
		class={[
			'flex max-h-[calc(100vh-32px)] w-full flex-col overflow-hidden rounded-xl border border-line-1 bg-surface-1 shadow-float',
			wide ? 'max-w-[880px]' : 'max-w-[460px]'
		]}
	>
		{@render children()}
	</div>
</div>
