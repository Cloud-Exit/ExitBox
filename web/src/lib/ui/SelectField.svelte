<script lang="ts">
	type Option = { value: string; label: string };
	let {
		label,
		options,
		value = $bindable(),
		title = '',
		onchange,
		wrap = 'flex-[1_1_220px]'
	}: { label: string; options: Option[]; value?: string | null; title?: string; onchange?: () => void; wrap?: string } = $props();
</script>

<label class={['flex min-w-0 flex-col gap-1.5', wrap]}>
	<span class="text-[11.5px] font-semibold text-ink-2">{label}</span>
	<select
		bind:value
		{onchange}
		{title}
		class="block w-full rounded-md border border-line-1 bg-inset px-2.5 py-[7px] text-[12.5px] text-ink-1 outline-none hover:border-line-2 focus:border-brand"
	>
		{#each options as o (o.value)}
			<option value={o.value}>{o.label}</option>
		{/each}
	</select>
</label>
