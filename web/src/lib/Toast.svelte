<script lang="ts">
	import { app } from './app.svelte';
</script>

{#if app.toast}
	<!-- At the top on a phone: the key bar and the keyboard take the bottom. -->
	<div role="status" class="fixed right-4 bottom-11 z-[20000] max-w-[min(520px,calc(100vw-32px))] max-md:inset-x-[12px] max-md:top-[calc(env(safe-area-inset-top)+8px)] max-md:pointer-events-none max-md:bottom-auto max-md:max-w-none">
		<div class={['rounded-lg border bg-surface-1 px-3 py-2.5 text-[12.5px] shadow-float max-md:text-[14px]', app.toast.bad ? 'border-warn' : 'border-accent']}>{app.toast.text}</div>
	</div>
{/if}
