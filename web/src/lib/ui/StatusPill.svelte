<script lang="ts">
	let { text, tone, class: cls = '' }: { text: string; tone: 'run' | 'wait' | 'off' | 'bad'; class?: string } = $props();
</script>

<span
	class={[
		'shrink-0 rounded px-[7px] py-0.5 text-[10.5px] font-semibold tracking-[0.03em] whitespace-nowrap uppercase',
		tone === 'run' && 'bg-brand-soft text-brand',
		tone === 'wait' && 'bg-amber-soft text-amber',
		tone === 'off' && 'bg-surface-2 text-ink-2',
		tone === 'bad' && 'bg-warn-soft text-warn',
		cls
	]}>{text}</span
>
