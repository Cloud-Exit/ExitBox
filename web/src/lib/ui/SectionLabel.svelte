<script lang="ts">
	import type { Snippet } from 'svelte';
	/** sub: a label inside a section, smaller and in sentence case. */
	let { children, sub = false }: { children: Snippet; sub?: boolean } = $props();
</script>

<span class={sub ? 'text-[10.5px] font-medium text-ink-2 max-md:text-[12px]' : 'text-[10.5px] font-semibold tracking-[0.06em] text-ink-2 uppercase'}>{@render children()}</span>
