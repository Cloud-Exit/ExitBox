<script lang="ts">
	import { app, type Win } from '../app.svelte';
	import { viewport } from '../viewport.svelte';
	import Icon from '../ui/Icon.svelte';
	import Button from '../ui/Button.svelte';
	import type { CodexQuestionAction } from '../codexQuestions';
	import type { IconName } from '../ui/icons';
	import { keyBytes, mods, withMods, type KeyName } from './keys.svelte';

	let { win }: { win: Win } = $props();

	let text = $state('');
	/** The field's text before the last edit, to find what a key added while a modifier is armed. */
	let prev = '';
	/** In focus the header's mark is gone: the focus button carries it. */
	const otherWaits = $derived(viewport.bare && app.windows.some((w) => w !== win && w.prompts.length));
	const codex = $derived(win.info.agent === 'codex');
	const question = $derived(codex && !win.busy && win.connected && !win.info.exited ? win.codexQuestion : null);
	const off = $derived(win.busy || win.info.exited || !win.connected);

	// Arrows as icons: the mono font has no arrow glyphs. Enter is the send button with an empty field.
	const keys: { key: KeyName | 'ctrl' | 'alt'; label: string; icon?: IconName; name: string }[] = [
		{ key: 'esc', label: 'esc', name: 'Escape' },
		{ key: 'up', label: '', icon: 'arrowUp', name: 'Up arrow' },
		{ key: 'down', label: '', icon: 'arrowDown', name: 'Down arrow' },
		{ key: 'tab', label: 'tab', name: 'Tab' },
		{ key: 'ctrl', label: 'ctrl', name: 'Control, for the next key' },
		{ key: 'alt', label: 'alt', name: 'Alt, for the next key' }
	];

	function answer(action: CodexQuestionAction) {
		if (question) win.answerCodex?.(question, action);
	}

	function focusEditor() { win.focusTerminal?.(); }

	/** A session switch with a phone keyboard open keeps the actual Codex editor focused. */
	function keepTerminalKeyboard() { if (codex && viewport.keyboard) focusEditor(); }

	function press(key: KeyName | 'ctrl' | 'alt') {
		if (key === 'ctrl' || key === 'alt') mods[key] = !mods[key];
		else win.input?.(keyBytes(key, win.appCursor()));
	}

	/** What an edit inserted: the part between the unchanged start and end. */
	function inserted(before: string, after: string) {
		let i = 0;
		while (i < before.length && i < after.length && before[i] === after[i]) i++;
		let j = 0;
		while (j < before.length - i && j < after.length - i && before[before.length - 1 - j] === after[after.length - 1 - j]) j++;
		return after.length > before.length ? after.slice(i, after.length - j) : '';
	}

	/** With ctrl or alt armed, the next typed key goes straight to the session and never into the field. */
	function onbeforeinput(e: InputEvent) {
		if (!(mods.ctrl || mods.alt) || !e.data || !e.cancelable) return;
		if (e.inputType !== 'insertText' && e.inputType !== 'insertCompositionText') return;
		e.preventDefault();
		// A composition carries the whole word so far; the key just typed is its end.
		win.input?.(withMods(e.inputType === 'insertText' ? e.data : ([...e.data].at(-1) ?? '')));
	}

	function oninput() {
		// Fallback where beforeinput cannot be cancelled (IME composition): take the key back out of the field.
		const added = mods.ctrl || mods.alt ? inserted(prev, text) : '';
		if (added) {
			text = prev;
			win.input?.(withMods(added));
		}
		prev = text;
	}

	function submit(e: SubmitEvent) {
		e.preventDefault();
		if (off || !win.input) return;
		const line = codex ? '' : text;
		if (!line) {
			win.input(keyBytes('enter', win.appCursor()));
			return;
		}
		if (!win.input(line)) return;
		text = '';
		prev = '';
		const enter = keyBytes('enter', win.appCursor());
		// Enter in its own write: agents read text and Enter in one read as a paste, not a submit.
		setTimeout(() => win.input?.(enter), 40);
	}

	/** A session switch with the keyboard up keeps it up: the new bar takes the focus. */
	function keepKeyboard(node: HTMLInputElement) {
		if (viewport.keyboard && !node.disabled) node.focus();
	}

	/** Keeps the focus (and the on-screen keyboard) where it is when a key is tapped. */
	const keepFocus = (e: MouseEvent) => e.preventDefault();
</script>

{#if question}
	<section aria-label="Codex question" class="max-h-[35dvh] shrink-0 overflow-y-auto border-t border-line-1 bg-surface-1 p-2">
		{#if question.kind === 'queued'}
			<Button class="w-full justify-center" onclick={() => answer({kind:'open'})} disabled={off}>Answer {question.title}</Button>
		{:else}
			<p class="mb-1 text-[12px] text-ink-2">{question.title}</p>
			<p class="mb-2 text-[14px] text-ink-1">{question.question}</p>
			{#if question.kind === 'options'}
				<div class="flex flex-col gap-1.5">
					{#each question.options as option (option.digit)}
						<Button class="h-auto! min-h-[44px] w-full justify-start py-2 text-left whitespace-normal!" onclick={() => answer({kind:'choice',digit:option.digit})} disabled={off}>{option.digit}. {option.label}</Button>
					{/each}
				</div>
				{#if question.canAddNotes}<Button class="mt-2" onclick={() => answer({kind:'notes'})} disabled={off}>Add notes to selected option</Button>{/if}
			{:else}
				<div class="flex flex-wrap gap-2">
					<Button onclick={focusEditor} disabled={off}>{question.notes ? 'Edit notes' : 'Type your answer'}</Button>
					<Button variant="primary" onclick={() => answer({kind:'submit'})} disabled={off}>{question.submitAll ? 'Submit all answers' : 'Submit answer'}</Button>
				</div>
			{/if}
		{/if}
	</section>
{/if}

<!-- Hidden while a card waits: on a short phone with the keyboard open the card needs the room. -->
<form
	data-keys
	{@attach keepTerminalKeyboard}
	onsubmit={submit}
	aria-label="Keys for the session"
	class={['flex shrink-0 items-center gap-[6px] border-t border-line-1 bg-surface-1 p-[6px]', win.prompts.length && !win.info.exited && 'hidden']}
>
	<button
		type="button"
		aria-label={viewport.bare ? (otherWaits ? 'Show the header and tabs; another session waits for your answer' : 'Show the header and tabs') : 'Focus: hide the header and tabs'}
		aria-pressed={viewport.bare}
		onmousedown={keepFocus}
		onclick={() => viewport.toggleFocus()}
		class={[
			'relative grid h-[44px] w-[40px] shrink-0 place-items-center rounded-[8px] border select-none',
			viewport.bare ? 'border-brand bg-brand-soft text-ink-1' : 'border-line-2 bg-surface-2 text-ink-1 active:bg-surface-3'
		]}
	>
		<Icon name={viewport.bare ? 'restore' : 'maximize'} size={16} />
		{#if otherWaits}<span class="absolute top-[6px] right-[6px] size-[8px] rounded-full bg-amber ring-2 ring-amber/40"></span>{/if}
	</button>
	<!-- One line with the field; the keys past the edge slide in. A line being typed takes their place. -->
	<div role="group" aria-label="Keys" class={[codex ? 'flex-1 gap-[4px] overflow-x-auto [scrollbar-width:none]' : 'max-w-[33%] shrink-0 gap-[4px] overflow-x-auto [scrollbar-width:none]', text && !codex ? 'hidden' : 'flex']}>
		{#each keys as k (k.key)}
			{@const sticky = k.key === 'ctrl' || k.key === 'alt'}
			{@const on = (k.key === 'ctrl' && mods.ctrl) || (k.key === 'alt' && mods.alt)}
			<button
				type="button"
				aria-label={k.name}
				aria-pressed={sticky ? on : undefined}
				disabled={off}
				onmousedown={keepFocus}
				onclick={() => press(k.key)}
				class={[
					'grid h-[44px] w-[38px] shrink-0 place-items-center rounded-[6px] border font-mono text-[12px] select-none disabled:opacity-50',
					on ? 'border-brand bg-brand-soft text-ink-1' : 'border-line-2 bg-surface-2 text-ink-1 active:bg-surface-3'
				]}>{#if k.icon}<Icon name={k.icon} />{:else}{k.label}{/if}</button
			>
		{/each}
	</div>
	{#if codex}
		<Button icon="terminal" onclick={focusEditor} disabled={off} aria-label="Open keyboard for Codex">Type</Button>
	{:else}
	<input
		type="text"
		bind:value={text}
		{@attach keepKeyboard}
		{onbeforeinput}
		{oninput}
		disabled={off}
		aria-label={'Text to send to ' + win.title}
		placeholder="Type"
		enterkeyhint="send"
		autocomplete="off"
		autocapitalize="off"
		spellcheck="false"
		class="h-[44px] min-w-0 flex-1 rounded-[8px] border border-line-2 bg-inset px-[10px] font-mono text-[16px] text-ink-1 outline-none placeholder:text-ink-3 focus:border-brand disabled:opacity-50"
	/>
	{/if}
	<button
		type="submit"
		aria-label={!codex && text ? 'Send the line and Enter' : 'Send Enter'}
		disabled={off}
		onmousedown={keepFocus}
		class="grid h-[44px] w-[40px] shrink-0 place-items-center rounded-[8px] bg-ink-1 text-surface-0 disabled:opacity-50"><Icon name="enter" size={18} /></button
	>
</form>
