<script lang="ts">
	let { kind, title, lead, source = '' }: { kind: string; title: string; lead: string; source?: string } = $props();
</script>

<div class="flex flex-col gap-1">
	<span class="inline-flex items-center gap-1.5 font-mono text-[10.5px] tracking-[0.08em] text-ink-2 uppercase">
		<span class="size-1.5 rounded-full bg-brand"></span>{kind}
	</span>
	<h1 class="text-[26px] leading-tight font-bold max-md:text-[22px]">{title}</h1>
	<span class="max-w-[640px] text-[13px] text-ink-2 max-md:text-[14px]">{lead}</span>
</div>
{#if source}<div class="font-mono text-[11.5px] text-ink-2">{source}</div>{/if}
