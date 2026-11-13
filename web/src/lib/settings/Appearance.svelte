<script lang="ts">
	import {
		FONT_DEFAULT,
		LINE_DEFAULT,
		LINE_MAX,
		LINE_MIN,
		SIZE_DEFAULT,
		SIZE_MAX,
		SIZE_MIN,
		TERM_FONTS,
		UI_FONT_DEFAULT,
		UI_FONTS,
		WEIGHT_DEFAULT,
		WEIGHTS,
		termFont,
		weightsOf,
		uiFont
	} from '../termFont.svelte';
	import Button from '../ui/Button.svelte';
	import SectionLabel from '../ui/SectionLabel.svelte';
	import Segmented from '../ui/Segmented.svelte';
	import SelectField from '../ui/SelectField.svelte';
	import TextField from '../ui/TextField.svelte';
	import PageHeader from './PageHeader.svelte';

	// Drafts, so a half-typed value is not replaced while typing.
	let size = $state(String(termFont.size));
	let line = $state(String(termFont.lineHeight));

	/** Every weight, the ones the family has no face for disabled. */
	const weightOptions = (family: string) => WEIGHTS.map((w) => ({ value: String(w.value), label: w.label, disabled: !weightsOf(family).includes(w.value) }));

	/** Why a weight is off, in a word or two. */
	function weightHint(family: string) {
		const missing = WEIGHTS.filter((w) => !weightsOf(family).includes(w.value)).map((w) => w.label);
		return missing.length ? `${family} has no ${missing.join(' or ')}.` : '';
	}

	const isDefault = $derived(
		termFont.family === FONT_DEFAULT &&
			termFont.weight === WEIGHT_DEFAULT &&
			termFont.size === SIZE_DEFAULT &&
			termFont.lineHeight === LINE_DEFAULT &&
			uiFont.family === UI_FONT_DEFAULT &&
			uiFont.weight === WEIGHT_DEFAULT
	);

	function reset() {
		termFont.reset();
		uiFont.reset();
		size = String(termFont.size);
		line = String(termFont.lineHeight);
	}
</script>

{#snippet weight(label: string, family: string, get: () => number, set: (v: number) => void, note = '')}
	{@const hint = [weightHint(family), note].filter(Boolean).join(' ')}
	<div class="flex flex-col gap-1.5">
		<span class="text-[11.5px] font-semibold text-ink-2 max-md:text-[13px]">Weight</span>
		<Segmented {label} options={weightOptions(family)} bind:value={() => String(get()), (v) => set(Number(v))} />
		{#if hint}<small class="text-[11.5px] text-ink-2">{hint}</small>{/if}
	</div>
{/snippet}

<PageHeader kind="Appearance" title="Appearance" lead="Fonts in this browser. Changes apply to open windows at once." />

<SectionLabel>Terminal font</SectionLabel>
<div class="flex flex-wrap items-start gap-2.5">
	<SelectField
		label="Font family"
		options={TERM_FONTS.map((f) => ({ value: f, label: f }))}
		hint="Where Consolas is not installed, Atkinson Hyperlegible Mono shows."
		bind:value={() => termFont.family, (v) => termFont.setFamily(v ?? '')}
	/>
	{@render weight(
		'Terminal font weight',
		termFont.family,
		() => termFont.weight,
		(v) => termFont.setWeight(v),
		termFont.weight === 700 ? 'Bold text then looks like normal text.' : ''
	)}
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
	aria-label="Terminal font preview"
	class="overflow-hidden rounded-lg border border-line-1 bg-inset px-3 py-2.5 whitespace-nowrap text-ink-1"
	style:font-family={termFont.css}
	style:font-size={termFont.size + 'px'}
	style:line-height={termFont.lineHeight}
	style:font-weight={termFont.weight}
>
	<span class="text-brand" style:font-weight={termFont.weightBold}>~/code</span> $ git log --oneline -3 <span class="text-ink-2">0O 1lI {'{}'} [] =&gt; != ===</span>
</div>

<SectionLabel>Interface font</SectionLabel>
<div class="flex flex-wrap items-start gap-2.5">
	<SelectField
		label="Font family"
		options={UI_FONTS.map((f) => ({ value: f, label: f }))}
		hint="Arial shows only where it is installed."
		bind:value={() => uiFont.family, (v) => uiFont.setFamily(v ?? '')}
	/>
	{@render weight('Interface font weight', uiFont.family, () => uiFont.weight, (v) => uiFont.setWeight(v))}
</div>
<div aria-label="Interface font preview" class="overflow-hidden rounded-lg border border-line-1 bg-surface-1 px-3 py-2.5 text-[13px] whitespace-nowrap text-ink-1 max-md:text-[14px]">
	<span class="font-bold">Session settings</span> <span class="text-ink-2">The quick brown fox jumps over the lazy dog. 0O 1lI</span>
</div>

<div>
	<Button icon="rotate" disabled={isDefault} onclick={reset}>Reset to defaults</Button>
</div>
