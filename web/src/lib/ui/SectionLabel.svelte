<script lang="ts">
	import type { Snippet } from 'svelte';
	let { children }: { children: Snippet } = $props();
</script>

<span class="text-[10.5px] font-semibold tracking-[0.06em] text-ink-2 uppercase">{@render children()}</span>
