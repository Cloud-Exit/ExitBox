<script lang="ts">
	import { app } from './app.svelte';
</script>

<div class="flex min-h-[26px] shrink-0 items-center justify-end border-t border-line-1 px-4 py-1 text-[11.5px] text-ink-2">
	<span class="font-mono">exitbox web · {app.data?.listen}</span>
</div>
