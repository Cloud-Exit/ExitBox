<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	type Props = HTMLInputAttributes & { label: string; hint?: string; value?: string | null; mono?: boolean; wrap?: string };
	let { label, hint = '', value = $bindable(), mono = false, wrap = 'flex-[1_1_260px]', class: cls, ...rest }: Props = $props();
</script>

<label class={['flex min-w-0 flex-col gap-1.5', wrap]}>
	<span class="text-[11.5px] font-semibold text-ink-2 max-md:text-[13px]">{label}</span>
	<input
		bind:value
		autocomplete="off"
		spellcheck="false"
		class={[
			'block w-full rounded-md border border-line-1 bg-inset px-2.5 py-[7px] text-[12.5px] text-ink-1 outline-none placeholder:text-ink-3 hover:border-line-2 focus:border-brand focus:bg-surface-0',
			// 16px: iOS zooms into smaller fields.
			'max-md:h-[44px] max-md:rounded-[8px] max-md:px-[12px] max-md:text-[16px]',
			mono && 'font-mono',
			cls
		]}
		{...rest}
	/>
	{#if hint}<small class="text-[11.5px] text-ink-2">{hint}</small>{/if}
</label>
