<script lang="ts">
	import { app } from '../app.svelte';
	import Button from '../ui/Button.svelte';
	import Icon from '../ui/Icon.svelte';

	let {
		items = $bindable(),
		placeholder,
		valid,
		label
	}: { items: string[]; placeholder: string; valid: (v: string) => boolean; label: string } = $props();

	let draft = $state('');

	function add() {
		const vals = draft.split(/[\s,]+/).filter(Boolean);
		const bad = vals.find((v) => !valid(v));
		if (bad) {
			app.notify('Not valid: ' + bad, true);
			return;
		}
		items = [...items, ...vals.filter((v) => !items.includes(v))];
		draft = '';
	}
</script>

<div class="flex flex-[1_1_100%] flex-col gap-2">
	<div class="flex flex-wrap gap-1.5">
		{#each items as v (v)}
			<span class="inline-flex items-center gap-1 rounded border border-line-2 bg-surface-2 py-[3px] pr-1 pl-2 font-mono text-[11.5px]">
				{v}
				<button type="button" aria-label={'Remove ' + v} title="Remove" class="grid place-items-center text-ink-3 hover:text-warn" onclick={() => (items = items.filter((x) => x !== v))}>
					<Icon name="x" size={11} />
				</button>
			</span>
		{/each}
	</div>
	<div class="flex max-w-[460px] gap-2">
		<input
			bind:value={draft}
			aria-label={label}
			{placeholder}
			spellcheck="false"
			onkeydown={(e) => {
				if (e.key === 'Enter') {
					e.preventDefault();
					add();
				}
			}}
			class="block w-full min-w-0 rounded-md border border-line-1 bg-inset px-2.5 py-[7px] font-mono text-[12.5px] outline-none placeholder:text-ink-3 hover:border-line-2 focus:border-brand"
		/>
		<Button icon="plus" onclick={add}>Add</Button>
	</div>
</div>
