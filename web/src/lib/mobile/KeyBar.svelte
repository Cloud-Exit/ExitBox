<script lang="ts">
	import type { Win } from '../app.svelte';
	import Icon from '../ui/Icon.svelte';
	import type { IconName } from '../ui/icons';
	import { keyBytes, mods, withMods, type KeyName } from './keys.svelte';

	let { win }: { win: Win } = $props();

	let text = $state('');
	/** The field's text before the last edit, to find what a key added while a modifier is armed. */
	let prev = '';
	const off = $derived(win.busy || win.info.exited || !win.connected);

	// Arrows and Enter as icons: the mono font has no arrow glyphs.
	const keys: { key: KeyName | 'ctrl' | 'alt'; label: string; icon?: IconName; name: string }[] = [
		{ key: 'esc', label: 'esc', name: 'Escape' },
		{ key: 'tab', label: 'tab', name: 'Tab' },
		{ key: 'ctrl', label: 'ctrl', name: 'Control, for the next key' },
		{ key: 'alt', label: 'alt', name: 'Alt, for the next key' },
		{ key: 'up', label: '', icon: 'arrowUp', name: 'Up arrow' },
		{ key: 'down', label: '', icon: 'arrowDown', name: 'Down arrow' },
		{ key: 'enter', label: '', icon: 'enter', name: 'Enter' }
	];

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
		const line = text;
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

	/** Keeps the focus (and the on-screen keyboard) where it is when a key is tapped. */
	const keepFocus = (e: MouseEvent) => e.preventDefault();
</script>

<!-- Hidden while a card waits: on a short phone with the keyboard open the card needs the room. -->
<form
	onsubmit={submit}
	aria-label="Keys for the session"
	class={['flex shrink-0 flex-col gap-[6px] border-t border-line-1 bg-surface-1 p-[6px]', win.prompts.length && !win.info.exited && 'hidden']}
>
	<div class="flex gap-[4px]">
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
					'grid h-[44px] min-w-0 flex-1 place-items-center rounded-[6px] border font-mono text-[12px] select-none disabled:opacity-50',
					on ? 'border-brand bg-brand-soft text-ink-1' : 'border-line-2 bg-surface-2 text-ink-1 active:bg-surface-3'
				]}>{#if k.icon}<Icon name={k.icon} />{:else}{k.label}{/if}</button
			>
		{/each}
	</div>
	<div class="flex items-center gap-[6px]">
		<input
			type="text"
			bind:value={text}
			{onbeforeinput}
			{oninput}
			disabled={off}
			aria-label="Text to send"
			placeholder={'Type to ' + win.title}
			enterkeyhint="send"
			autocomplete="off"
			autocapitalize="off"
			spellcheck="false"
			class="h-[44px] min-w-0 flex-1 rounded-[8px] border border-line-2 bg-inset px-[12px] font-mono text-[16px] text-ink-1 outline-none placeholder:text-ink-3 focus:border-brand disabled:opacity-50"
		/>
		<button
			type="submit"
			aria-label={text ? 'Send the line and Enter' : 'Send Enter'}
			disabled={off}
			onmousedown={keepFocus}
			class="grid size-[44px] shrink-0 place-items-center rounded-[8px] bg-ink-1 text-surface-0 disabled:opacity-50"><Icon name="arrowUp" size={18} /></button
		>
	</div>
</form>
