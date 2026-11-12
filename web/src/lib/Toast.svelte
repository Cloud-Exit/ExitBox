<script lang="ts">
	import { app } from './app.svelte';
</script>

{#if app.toast}
	<div role="status" class="fixed right-4 bottom-11 z-[20000] max-w-[min(520px,calc(100vw-32px))]">
		<div class={['rounded-lg border bg-surface-1 px-3 py-2.5 text-[12.5px] shadow-float', app.toast.bad ? 'border-warn' : 'border-accent']}>{app.toast.text}</div>
	</div>
{/if}
