<script lang="ts" generics="T extends string">
	import type { Snippet } from 'svelte';

	let {
		label,
		options,
		value = $bindable(),
		lead
	}: {
		label: string;
		/** A disabled option stays visible; the page says why next to it. */
		options: { value: T; label: string; disabled?: boolean }[];
		value: T;
		/** Drawn before each option's label, e.g. an icon. */
		lead?: Snippet<[T]>;
	} = $props();

	let group = $state<HTMLDivElement>();

	// A radio group: arrow keys move the choice, Tab leaves the group.
	function onkeydown(e: KeyboardEvent) {
		const step = e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : 0;
		if (!step) return;
		e.preventDefault();
		const i = options.findIndex((o) => o.value === value);
		let next = i;
		do next = (next + step + options.length) % options.length;
		while (options[next].disabled && next !== i);
		value = options[next].value;
		group?.querySelectorAll<HTMLElement>('[role=radio]')[next]?.focus();
	}
</script>

<div bind:this={group} role="radiogroup" aria-label={label} tabindex="-1" {onkeydown} class="inline-flex w-fit gap-0.5 rounded-lg border border-line-1 bg-inset p-0.5">
	{#each options as o (o.value)}
		<button
			type="button"
			role="radio"
			aria-checked={value === o.value}
			tabindex={value === o.value ? 0 : -1}
			disabled={o.disabled}
			onclick={() => (value = o.value)}
			class={[
				'inline-flex h-8 items-center gap-2 rounded-md px-3 text-[13px] font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-40 max-md:h-[44px] max-md:px-[16px] max-md:text-[14px]',
				value === o.value ? 'bg-surface-0 text-ink-1 shadow-panel' : 'text-ink-2 enabled:hover:text-ink-1'
			]}
		>
			{@render lead?.(o.value)}{o.label}
		</button>
	{/each}
</div>
