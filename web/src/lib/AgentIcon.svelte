<script lang="ts">
	import { agentMark } from './agents';

	let { name, small = false, muted = false }: { name: string; small?: boolean; muted?: boolean } = $props();
	const mark = $derived(agentMark(name));
</script>

<span aria-hidden="true" class={['grid shrink-0 place-items-center rounded-[5px] text-white', small ? 'size-4' : 'size-[18px]', mark.tile, muted && 'grayscale']}>
	{#if mark.glyph}
		<svg
			xmlns="http://www.w3.org/2000/svg"
			width={small ? 10 : 12}
			height={small ? 10 : 12}
			viewBox="0 0 24 24"
			fill="none"
			stroke="currentColor"
			stroke-width="2.5"
			stroke-linecap="round"
			stroke-linejoin="round"
		>
			{@html mark.glyph}
		</svg>
	{:else}
		<span class="text-[10px] leading-none font-bold uppercase">{name.slice(0, 1)}</span>
	{/if}
</span>
