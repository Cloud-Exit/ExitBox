<script lang="ts">
	import { app } from './app.svelte';
	import Button from './ui/Button.svelte';
	import Modal from './ui/Modal.svelte';

	const p = $derived(app.prompt);
</script>

{#if p}
	{#key p}
		<Modal label={p.title} onclose={() => p.resolve(null)} sheet>
			<div class="border-b border-line-1 px-5 py-4 max-md:px-[16px]"><h2 class="text-base font-bold">{p.title}</h2></div>
			<div class="px-5 py-[18px] text-[13px] text-ink-2 max-md:px-[16px] max-md:text-[14px]">{p.text}</div>
			<div class="flex flex-wrap items-center gap-2 border-t border-line-1 px-5 py-3.5 max-md:flex-col max-md:items-stretch max-md:px-[16px]">
				{#each p.choices as c (c.value)}
					<Button variant={c.tone} class="max-md:w-full max-md:justify-center" onclick={() => p.resolve(c.value)}>{c.label}</Button>
				{/each}
				<!-- Cancel takes the focus: a stray Enter must never pick a destructive choice. -->
				<Button variant="ghost" data-autofocus class="max-md:w-full max-md:justify-center max-md:border-line-2" onclick={() => p.resolve(null)}>Cancel</Button>
			</div>
		</Modal>
	{/key}
{/if}
