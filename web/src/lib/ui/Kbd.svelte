<script lang="ts">
	import type { Snippet } from 'svelte';
	let { children }: { children: Snippet } = $props();
</script>

<kbd class="rounded border border-kbd-line bg-kbd px-[5px] pt-px pb-0.5 font-mono text-[10.5px] font-medium text-ink-2 shadow-[0_1px_0_var(--x-line-2)]">{@render children()}</kbd>
