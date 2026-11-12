<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import Icon from './Icon.svelte';
	import type { IconName } from './icons';

	type Props = HTMLButtonAttributes & { pressed?: boolean; icon?: IconName; size?: 'sm' | 'md'; children: Snippet };
	let { pressed, icon, size = 'sm', children, class: cls, ...rest }: Props = $props();
</script>

<button
	type="button"
	aria-pressed={pressed === undefined ? undefined : pressed}
	class={[
		'inline-flex items-center rounded-md border font-semibold whitespace-nowrap transition-colors disabled:cursor-not-allowed disabled:opacity-55',
		size === 'md' ? 'h-9 gap-2 pr-3 pl-2 text-[13px]' : 'h-7 gap-1.5 px-2.5 text-xs',
		pressed
			? ['bg-brand-soft text-ink-1', size === 'md' ? 'border-brand' : 'border-line-2']
			: 'border-line-1 text-ink-2 enabled:hover:bg-surface-2 enabled:hover:text-ink-1',
		cls
	]}
	{...rest}
>
	{#if icon}<Icon name={icon} size={12} />{/if}
	{@render children()}
</button>
