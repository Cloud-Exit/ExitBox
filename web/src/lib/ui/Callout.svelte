<script lang="ts">
	import type { Snippet } from 'svelte';
	let { tone = 'info', children }: { tone?: 'info' | 'bad' | 'ok'; children: Snippet } = $props();
</script>

<div
	role={tone === 'bad' ? 'alert' : 'status'}
	class={[
		'flex items-start gap-2 rounded-lg border px-3 py-2.5 text-[12.5px]',
		tone === 'info' && 'border-line-2 bg-surface-2',
		tone === 'bad' && 'border-warn bg-warn-soft',
		tone === 'ok' && 'border-accent bg-accent-soft'
	]}
>
	{@render children()}
</div>
