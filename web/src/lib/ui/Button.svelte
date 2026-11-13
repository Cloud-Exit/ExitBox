<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import Icon from './Icon.svelte';
	import type { IconName } from './icons';

	type Props = HTMLButtonAttributes & {
		variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
		size?: 'lg' | 'md' | 'sm';
		icon?: IconName;
		children: Snippet;
	};
	let { variant = 'secondary', size = 'md', icon, children, type = 'button', class: cls, ...rest }: Props = $props();
</script>

<button
	{type}
	class={[
		'inline-flex items-center gap-[7px] rounded-md border border-transparent font-[650] whitespace-nowrap transition-colors select-none disabled:cursor-not-allowed disabled:opacity-50',
		size === 'lg' ? 'h-10 px-5 text-[14px]' : size === 'md' ? 'h-8 px-3.5 text-[12.5px]' : 'h-7 px-2.5 text-[12.5px]',
		// Touch targets on a phone.
		'max-md:h-[44px] max-md:text-[14px]',
		variant === 'primary' && 'bg-btn text-btn-ink enabled:hover:bg-btn-hover',
		variant === 'secondary' && 'border-line-2 bg-surface-2 text-ink-1 enabled:hover:bg-surface-3',
		variant === 'ghost' && 'text-ink-2 enabled:hover:bg-surface-2 enabled:hover:text-ink-1',
		variant === 'danger' && 'bg-warn text-white enabled:hover:brightness-110',
		cls
	]}
	{...rest}
>
	{#if icon}<Icon name={icon} size={size === 'lg' ? 16 : size === 'md' ? 14 : 12} />{/if}
	{@render children()}
</button>
