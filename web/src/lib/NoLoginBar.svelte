<script lang="ts">
	import { app } from './app.svelte';
	import Icon from './ui/Icon.svelte';
</script>

<div role="status" class="flex h-8 shrink-0 items-center gap-2 border-b border-amber/40 bg-amber-soft px-4 text-[12.5px] text-ink-1">
	<span class="shrink-0 text-amber"><Icon name="shield" /></span>
	<span class="min-w-0 truncate">
		<span class="font-semibold">No login set:</span>
		anyone {app.me?.loopback === false ? 'who can reach this server' : 'on this machine'} can run sessions as you.
	</span>
	<button type="button" onclick={() => app.enableLogin()} class="ml-auto shrink-0 rounded font-semibold text-amber underline-offset-2 hover:underline focus-visible:underline">
		Enable login
	</button>
</div>
