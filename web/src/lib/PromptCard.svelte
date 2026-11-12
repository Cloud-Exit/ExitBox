<script lang="ts">
	import type { HostPrompt } from './app.svelte';
	import AgentIcon from './AgentIcon.svelte';
	import Button from './ui/Button.svelte';

	let {
		prompt,
		agent,
		focus = false,
		onanswer
	}: { prompt: HostPrompt; agent: string; focus?: boolean; onanswer: (allow: boolean, password?: string) => void } = $props();

	let password = $state('');
	const isPassword = $derived(prompt.kind === 'vault-password');
	const name = $derived(agent || 'The agent');

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
			default:
				return { cmd: 'exitbox-vault', lead: 'wants to unlock', subject: prompt.workspace ? `${prompt.workspace} vault` : 'vault', note: 'Kept in memory until this session ends.', yes: 'Unlock', no: 'Cancel' };
		}
	});

	function deny() {
		password = '';
		onanswer(false);
	}

	function submit() {
		if (isPassword) {
			if (!password) return;
			const pw = password;
			password = '';
			onanswer(true, pw);
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

	/** Takes the keyboard when its window is the focused one. */
	function grab(node: HTMLElement) {
		if (focus) node.focus();
	}
</script>

<!-- Not a form: a password form makes browsers offer to save the password. -->
<div
	role="group"
	data-prompt={prompt.kind}
	aria-label={`${text.cmd}: ${name} ${text.lead} ${text.subject}`}
	{@attach escapeDenies}
	class="flex flex-wrap items-center gap-x-4 gap-y-2.5 rounded-lg border border-amber bg-surface-1 px-3.5 py-3 shadow-float"
>
	<div class="flex min-w-0 flex-[1_1_220px] flex-col gap-1">
		<span class="flex min-w-0 items-center gap-1.5 text-[12.5px]">
			<span class="font-mono font-bold text-amber">{text.cmd}</span>
			<span class="text-ink-3">·</span>
			<AgentIcon name={prompt.agent} small />
			<span class="truncate text-ink-2"><span class="font-semibold text-ink-1">{name}</span> {text.lead}</span>
		</span>
		<span class="truncate font-mono text-[14px] font-bold text-ink-1" title={text.subject}>{text.subject}</span>
		<span class="text-[11.5px] text-ink-2">{text.note}</span>
	</div>
	{#if isPassword}
		<input
			{@attach grab}
			type="password"
			bind:value={password}
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
			class="h-8 min-w-0 flex-[1_1_160px] rounded-md border border-line-2 bg-inset px-2.5 text-[12.5px] text-ink-1 outline-none placeholder:text-ink-3 focus:border-brand"
		/>
	{/if}
	<div class="ml-auto flex shrink-0 items-center gap-1.5">
		{#if isPassword}
			<Button variant="primary" size="sm" disabled={!password} onclick={submit}>{text.yes}</Button>
		{:else}
			<Button variant="primary" size="sm" onclick={submit} {@attach grab}>{text.yes}</Button>
		{/if}
		<Button variant="ghost" size="sm" onclick={deny}>{text.no}</Button>
	</div>
</div>
