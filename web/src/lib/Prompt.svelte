<script lang="ts">
	import { app } from './app.svelte';
	import Button from './ui/Button.svelte';
	import Modal from './ui/Modal.svelte';

	const p = $derived(app.prompt);
</script>

{#if p}
	<Modal label={p.title} onclose={() => p.resolve(null)}>
		<div class="border-b border-line-1 px-5 py-4"><h2 class="text-base font-bold">{p.title}</h2></div>
		<div class="px-5 py-[18px] text-[13px] text-ink-2">{p.text}</div>
		<div class="flex flex-wrap items-center gap-2 border-t border-line-1 px-5 py-3.5">
			{#each p.choices as c (c.value)}
				<Button variant={c.tone} onclick={() => p.resolve(c.value)}>{c.label}</Button>
			{/each}
			<Button variant="ghost" onclick={() => p.resolve(null)}>Cancel</Button>
		</div>
	</Modal>
{/if}
