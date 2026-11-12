<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import Icon from './Icon.svelte';
	import type { IconName } from './icons';

	type Props = HTMLButtonAttributes & { pressed?: boolean; icon?: IconName; children: Snippet };
	let { pressed, icon, children, class: cls, ...rest }: Props = $props();
</script>

<button
	type="button"
	aria-pressed={pressed === undefined ? undefined : pressed}
	class={[
		'inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-xs font-semibold whitespace-nowrap transition-colors disabled:cursor-not-allowed disabled:opacity-55',
		pressed ? 'border-line-2 bg-brand-soft text-ink-1' : 'border-line-1 text-ink-2 enabled:hover:bg-surface-2 enabled:hover:text-ink-1',
		cls
	]}
	{...rest}
>
	{#if icon}<Icon name={icon} size={12} />{/if}
	{@render children()}
</button>
