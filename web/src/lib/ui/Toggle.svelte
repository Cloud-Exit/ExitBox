<script lang="ts">
	import Icon from './Icon.svelte';

	let {
		label,
		description,
		tag = '',
		checked = $bindable(false),
		onchange
	}: { label: string; description: string; tag?: string; checked?: boolean; onchange?: (v: boolean) => void } = $props();
</script>

<button
	type="button"
	aria-pressed={checked}
	onclick={() => {
		checked = !checked;
		onchange?.(checked);
	}}
	class={[
		'flex min-h-14 flex-[1_1_340px] items-start gap-3 rounded-lg border border-line-1 px-3 py-2.5 text-left',
		checked && 'bg-brand-soft'
	]}
>
	<span
		class={[
			'mt-0.5 grid size-4 shrink-0 place-items-center rounded border text-surface-0',
			checked ? 'border-brand bg-brand' : 'border-line-3 bg-inset'
		]}
	>
		{#if checked}<Icon name="check" size={12} />{/if}
	</span>
	<span class="flex min-w-0 flex-col gap-0.5">
		<span class="flex items-center gap-2 text-[13px] font-semibold">
			{label}
			{#if tag}<span class="rounded border border-line-2 bg-surface-2 px-1.5 font-mono text-[9.5px] font-bold text-ink-2 lowercase">{tag}</span>{/if}
		</span>
		<span class="text-[11.5px] text-ink-2">{description}</span>
	</span>
</button>
