<script lang="ts">
	import {
		FONT_DEFAULT,
		FONT_SUGGESTIONS,
		LINE_DEFAULT,
		LINE_MAX,
		LINE_MIN,
		SIZE_DEFAULT,
		SIZE_MAX,
		SIZE_MIN,
		termFont
	} from '../termFont.svelte';
	import Button from '../ui/Button.svelte';
	import TextField from '../ui/TextField.svelte';
	import PageHeader from './PageHeader.svelte';

	// Drafts, so a half-typed value is not replaced while typing.
	let family = $state(termFont.family);
	let size = $state(String(termFont.size));
	let line = $state(String(termFont.lineHeight));

	const isDefault = $derived(termFont.family === FONT_DEFAULT && termFont.size === SIZE_DEFAULT && termFont.lineHeight === LINE_DEFAULT);

	function reset() {
		termFont.reset();
		family = termFont.family;
		size = String(termFont.size);
		line = String(termFont.lineHeight);
	}
</script>

<PageHeader kind="Appearance" title="Terminal" lead="Font for session terminals in this browser. Changes apply to open windows at once." />
<div class="flex flex-wrap gap-2.5">
	<TextField
		label="Font family"
		list="term-font-suggestions"
		placeholder={FONT_DEFAULT}
		hint="Any installed font; falls back to monospace when missing."
		bind:value={family}
		oninput={() => termFont.setFamily(family)}
		onchange={() => (family = termFont.family)}
	/>
	<datalist id="term-font-suggestions">
		{#each FONT_SUGGESTIONS as f (f)}<option value={f}></option>{/each}
	</datalist>
	<TextField
		label="Font size"
		type="number"
		inputmode="numeric"
		min={SIZE_MIN}
		max={SIZE_MAX}
		step="1"
		wrap="flex-[0_1_140px]"
		bind:value={size}
		oninput={() => {
			// Out of range while typing: clamped when the field is left.
			const n = Number(size);
			if (n >= SIZE_MIN && n <= SIZE_MAX) termFont.setSize(n);
		}}
		onchange={() => {
			// An emptied field keeps the current size.
			if (String(size ?? '') !== '') termFont.setSize(Number(size));
			size = String(termFont.size);
		}}
	/>
	<TextField
		label="Line height"
		type="number"
		inputmode="decimal"
		min={LINE_MIN}
		max={LINE_MAX}
		step="0.05"
		wrap="flex-[0_1_140px]"
		bind:value={line}
		oninput={() => {
			const n = Number(line);
			if (n >= LINE_MIN && n <= LINE_MAX) termFont.setLineHeight(n);
		}}
		onchange={() => {
			if (String(line ?? '') !== '') termFont.setLineHeight(Number(line));
			line = String(termFont.lineHeight);
		}}
	/>
</div>
<div
	aria-label="Font preview"
	class="overflow-hidden rounded-lg border border-line-1 bg-inset px-3 py-2.5 whitespace-nowrap text-ink-1"
	style:font-family={termFont.css}
	style:font-size={termFont.size + 'px'}
	style:line-height={termFont.lineHeight}
>
	<span class="text-brand">~/code</span> $ git log --oneline -3 <span class="text-ink-2">0O 1lI {'{}'} [] =&gt; != ===</span>
</div>
<div>
	<Button icon="rotate" disabled={isDefault} onclick={reset}>Reset to defaults</Button>
</div>
