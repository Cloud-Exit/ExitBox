<script lang="ts">
	import type { HostPrompt } from './app.svelte';
	import AgentIcon from './AgentIcon.svelte';
	import Button from './ui/Button.svelte';
	import Spinner from './ui/Spinner.svelte';

	let {
		prompt,
		agent,
		focus = false,
		slot = 0,
		onanswer
	}: {
		prompt: HostPrompt;
		agent: string;
		focus?: boolean;
		/** Its place in the stack; a new place means it moved under the pointer. */
		slot?: number;
		onanswer: (allow: boolean, password?: string) => boolean;
	} = $props();

	/** How long the approving button stays off after the card appears, moves or takes the keyboard. */
	const SETTLE_MS = 600;

	let password = $state('');
	let settling = $state(true);
	let settleTimer: ReturnType<typeof setTimeout> | undefined;

	/** A click or key meant for what was there before must not approve this card. */
	function settle() {
		settling = true;
		clearTimeout(settleTimer);
		settleTimer = setTimeout(() => (settling = false), SETTLE_MS);
	}

	$effect(() => {
		void slot;
		settle();
		return () => clearTimeout(settleTimer);
	});
	const isPassword = $derived(prompt.kind === 'vault-password');
	const name = $derived(agent || 'The agent');
	const sending = $derived(!!prompt.sending);

	const text = $derived.by(() => {
		const ws = prompt.workspace ? `the ${prompt.workspace} vault` : 'the vault';
		switch (prompt.kind) {
			case 'allow':
				return { cmd: 'exitbox-allow', lead: 'wants', subject: prompt.subject, note: 'Allowed until this session ends.', yes: 'Allow for session', no: 'Deny' };
			case 'vault-read':
				return { cmd: 'exitbox-vault get', lead: 'wants to read', subject: prompt.subject, note: `From ${ws}. Later reads of this key skip this prompt.`, yes: 'Allow', no: 'Deny' };
			case 'vault-write':
				return { cmd: 'exitbox-vault set', lead: 'wants to write', subject: prompt.subject, note: `Saves this key to ${ws}.`, yes: 'Allow write', no: 'Deny' };
			case 'vault-list':
				return { cmd: 'exitbox-vault list', lead: 'wants to list', subject: 'vault key names', note: 'Names only, never values.', yes: 'Allow', no: 'Deny' };
			case 'vault-password':
				return { cmd: 'exitbox-vault', lead: 'wants to unlock', subject: prompt.workspace ? `${prompt.workspace} vault` : 'vault', note: 'Kept in memory until this session ends.', yes: 'Unlock', no: 'Cancel' };
			default:
				// A kind this page does not know can only be denied.
				return { cmd: 'exitbox', lead: 'asks for', subject: prompt.subject || prompt.kind, note: 'This page does not know this request.', yes: '', no: 'Deny' };
		}
	});
	const known = $derived(text.yes !== '');

	let card: HTMLElement | undefined;

	/** Answered here: the field is done with, so the window may give the terminal the keyboard. */
	function leaveField() {
		if (card && document.activeElement instanceof HTMLInputElement && card.contains(document.activeElement)) card.focus({ preventScroll: true });
	}

	function deny() {
		if (sending) return;
		password = '';
		if (onanswer(false)) leaveField();
	}

	function submit() {
		if (sending || !known || settling) return;
		if (isPassword) {
			if (!password) return;
			// Kept when it could not be sent, so it need not be typed again.
			if (onanswer(true, password)) {
				password = '';
				leaveField();
			}
		} else {
			onanswer(true);
		}
	}

	/** Escape anywhere in the card denies. */
	function escapeDenies(node: HTMLElement) {
		const onKey = (e: KeyboardEvent) => {
			if (e.key !== 'Escape') return;
			e.preventDefault();
			e.stopPropagation();
			deny();
		};
		node.addEventListener('keydown', onKey);
		return () => node.removeEventListener('keydown', onKey);
	}

	/** Takes the keyboard for its focused window; never onto an approving button, never from a field or a dialog. */
	function grab(node: HTMLElement) {
		if (!focus) return;
		// After the click that focused the window has moved focus where it goes.
		const frame = requestAnimationFrame(() => {
			const at = document.activeElement;
			if (!node.isConnected || (at && node.closest('[role=group]')?.contains(at))) return;
			if (at instanceof HTMLElement && at.closest('input, textarea, select, [contenteditable]') && !at.closest('.xterm')) return;
			if (document.querySelector('[role=dialog]')) return;
			node.focus();
			settle();
		});
		return () => cancelAnimationFrame(frame);
	}
</script>

<!-- Not a form: a password form makes browsers offer to save the password. -->
<div
	role="group"
	tabindex="-1"
	bind:this={card}
	data-prompt={prompt.kind}
	aria-label={`${text.cmd}: ${name} ${text.lead} ${text.subject}`}
	aria-busy={sending}
	{@attach escapeDenies}
	{@attach !isPassword && grab}
	class={[
		'flex flex-wrap items-center gap-x-6 gap-y-3.5 rounded-xl border bg-surface-1 px-6 py-5 shadow-float focus:outline-2 focus:outline-offset-2 focus:outline-amber',
		// Stacked on a phone: text, then full-width buttons.
		'max-md:flex-col max-md:flex-nowrap max-md:items-stretch max-md:gap-[12px] max-md:px-[16px] max-md:py-[16px]',
		known ? 'border-amber' : 'border-line-2'
	]}
>
	<div class="flex min-w-0 flex-[1_1_260px] flex-col gap-1.5 max-md:flex-none">
		<span class="flex min-w-0 items-center gap-2 text-[15px] max-md:flex-wrap max-md:gap-y-1 max-md:text-[14px]">
			<span class={['font-mono font-bold max-md:whitespace-nowrap', known ? 'text-amber' : 'text-ink-2']}>{text.cmd}</span>
			<span class="text-ink-3">·</span>
			<AgentIcon name={prompt.agent} />
			<span class="truncate text-ink-2 max-md:whitespace-normal"><span class="font-semibold text-ink-1">{name}</span> {text.lead}</span>
		</span>
		<!-- Wraps anywhere: the tail of a long domain is the part that matters. -->
		<span class="max-h-24 overflow-y-auto font-mono text-[18px] font-bold break-all text-ink-1 max-md:text-[16px]">{text.subject}</span>
		<span class="text-[13.5px] text-ink-2">{text.note}</span>
		{#if prompt.error}<span role="alert" class="text-[13.5px] font-semibold text-warn">{prompt.error}</span>{/if}
	</div>
	{#if isPassword}
		<input
			{@attach grab}
			type="password"
			bind:value={password}
			disabled={sending}
			aria-label="Vault password"
			placeholder="Vault password"
			autocomplete="off"
			spellcheck="false"
			onkeydown={(e) => {
				if (e.key === 'Enter') {
					e.preventDefault();
					submit();
				}
			}}
			class="h-10 min-w-0 flex-[1_1_200px] rounded-md border border-line-2 bg-inset px-3 text-[14px] text-ink-1 outline-none placeholder:text-ink-3 focus:border-brand disabled:opacity-50 max-md:h-[44px] max-md:flex-none max-md:text-[16px]"
		/>
	{/if}
	<!-- Deny first in the tab order, approve first on screen: Tab then Enter must never approve. -->
	<div class="ml-auto flex shrink-0 items-center gap-2 max-md:ml-0 max-md:flex-col max-md:items-stretch">
		{#if sending}<span class="text-ink-2 max-md:self-center"><Spinner size={16} label="Sending your answer" /></span>{/if}
		<Button variant="ghost" size="lg" class="order-2 max-md:w-full max-md:justify-center max-md:border-line-2" disabled={sending} onclick={deny}>{text.no}</Button>
		{#if known}
			<Button variant="primary" size="lg" class="order-1 max-md:w-full max-md:justify-center" disabled={sending || settling || (isPassword && !password)} onclick={submit}
				>{text.yes}</Button
			>
		{/if}
	</div>
</div>
