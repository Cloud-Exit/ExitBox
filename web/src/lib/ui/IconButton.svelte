<script lang="ts">
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import Icon from './Icon.svelte';
	import type { IconName } from './icons';

	type Props = HTMLButtonAttributes & { icon: IconName; label: string; small?: boolean };
	let { icon, label, small = false, class: cls, ...rest }: Props = $props();
</script>

<button
	type="button"
	aria-label={label}
	title={label}
	class={[
		'grid shrink-0 place-items-center rounded-md text-ink-3 transition-colors hover:bg-surface-2 hover:text-ink-1',
		small ? 'size-6' : 'size-7',
		cls
	]}
	{...rest}
>
	<Icon name={icon} size={small ? 12 : 14} />
</button>
