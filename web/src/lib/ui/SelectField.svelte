<script lang="ts">
	type Option = { value: string; label: string };
	let {
		label,
		options,
		value = $bindable(),
		title = '',
		hint = '',
		error = '',
		onchange,
		wrap = 'flex-[1_1_220px]'
}: { label: string; options: Option[]; value?: string | null; title?: string; hint?: string; error?: string; onchange?: () => void; wrap?: string } = $props();
</script>

<label class={['flex min-w-0 flex-col gap-1.5', wrap]}>
	<span class="text-[11.5px] font-semibold text-ink-2 max-md:text-[13px]">{label}</span>
	<select
		bind:value
		{onchange}
		{title}
		aria-invalid={error ? true : undefined}
		class={[
			'block w-full rounded-md border bg-inset px-2.5 py-[7px] text-[12.5px] text-ink-1 outline-none hover:border-line-2 focus:border-brand max-md:h-[44px] max-md:rounded-[8px] max-md:px-[12px] max-md:text-[16px]',
			error ? 'border-warn' : 'border-line-1'
		]}
	>
		{#each options as o (o.value)}
			<option value={o.value}>{o.label}</option>
		{/each}
	</select>
	{#if error}<small role="alert" class="text-[11.5px] text-warn">{error}</small>{:else if hint}<small class="text-[11.5px] text-ink-2">{hint}</small>{/if}
</label>
