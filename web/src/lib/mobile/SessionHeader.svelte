<script lang="ts">
	import { app, sessionStatus } from '../app.svelte';
	import { sshTarget } from '../api';
	import SessionMenu from '../SessionMenu.svelte';
	import Brand from '../ui/Brand.svelte';
	import Icon from '../ui/Icon.svelte';
	import StatusPill from '../ui/StatusPill.svelte';

	const win = $derived(app.shown);
	const number = $derived(win ? app.windows.indexOf(win) + 1 : 0);
	const status = $derived(win ? sessionStatus(win) : null);
	/** Another session waits for an answer: the list button says so. */
	const waiting = $derived(app.windows.some((w) => w !== win && w.prompts.length));
	const canRename = $derived(!!win && !!win.info.name && !(win.attached && win.info.exited));

	let menu = $state<{ x: number; y: number; anchor: HTMLElement } | null>(null);
	let renaming = $state(false);
	let draft = $state('');

	function startRename() {
		if (!win) return;
		draft = win.info.renameTo || win.info.name;
		renaming = true;
	}

	async function commitRename() {
		if (!renaming || !win) return;
		const name = draft.trim();
		renaming = false;
		if (name && name !== (win.info.renameTo || win.info.name)) await app.renameWindow(win, name);
	}

	function focusSelect(node: HTMLInputElement) {
		node.focus();
		node.select();
	}

	// A rename belongs to the session it started on.
	$effect(() => {
		void win;
		renaming = false;
		menu = null;
	});
</script>

<header class="flex min-h-[52px] shrink-0 items-center gap-[4px] border-b border-line-1 bg-surface-1 pr-[8px] pl-[4px]">
	<button
		type="button"
		aria-label={waiting ? 'Open sessions; another session waits for your answer' : 'Open sessions'}
		onclick={() => (app.sessionsOpen = true)}
		class="relative grid size-[44px] shrink-0 place-items-center rounded-[6px] text-ink-1"
	>
		<Icon name="list" size={18} />
		{#if waiting}<span class="absolute top-[10px] right-[10px] size-[8px] rounded-full bg-amber ring-2 ring-amber/40"></span>{/if}
	</button>
	{#if win && status}
		{#if renaming}
			<input
				bind:value={draft}
				{@attach focusSelect}
				aria-label="New session name"
				spellcheck="false"
				autocapitalize="off"
				enterkeyhint="done"
				onkeydown={(e) => {
					if (e.key === 'Enter') {
						e.preventDefault();
						commitRename();
					} else if (e.key === 'Escape') {
						e.preventDefault();
						renaming = false;
					}
				}}
				onblur={commitRename}
				class="h-[40px] min-w-0 flex-1 rounded-[6px] border border-brand bg-surface-0 px-[8px] text-[16px] font-semibold outline-none"
			/>
		{:else}
			<span class="flex min-w-0 flex-1 flex-col">
				<span class="flex min-w-0 items-center gap-[8px]">
					<span class="shrink-0 font-mono text-[12px] text-ink-2">{number}</span>
					<span class="truncate text-[15px] font-bold" title={win.info.renameTo ? `Renames to "${win.info.renameTo}" when it ends` : undefined}>{win.title}</span>
				</span>
				<span class="truncate font-mono text-[12px] text-ink-2">
					{win.ssh ? 'SSH · ' + (win.info.ssh ? sshTarget(win.info.ssh) : '') : app.agentDisplay(win.info.agent) + ' · ' + app.tilde(win.info.dir)}
				</span>
			</span>
		{/if}
		<StatusPill text={status.text} tone={status.tone} />
		<button
			type="button"
			aria-label="Session actions"
			aria-haspopup="menu"
			disabled={win.busy}
			onclick={(e) => {
				const r = e.currentTarget.getBoundingClientRect();
				menu = { x: r.left, y: r.bottom, anchor: e.currentTarget };
			}}
			class="grid size-[44px] shrink-0 place-items-center rounded-[6px] text-ink-1 disabled:opacity-50"
		>
			<Icon name="more" size={18} />
		</button>
	{:else}
		<span class="flex min-w-0 flex-1 items-center pl-[4px]"><Brand /></span>
	{/if}
</header>

<!-- Hidden while a card waits here, like the key bar: the card needs the room. -->
<div class={['flex shrink-0 border-b border-line-1 bg-surface-1', win && win.prompts.length && !win.info.exited && 'hidden']}>
	<div role="tablist" aria-label="Tabs" class="flex min-w-0 flex-1 items-stretch gap-[2px] overflow-x-auto px-[4px] [scrollbar-width:none]">
		{#each app.tabs as t (t.id)}
			{@const active = app.activeTab === t.id}
			{@const list = app.tabWindows(t.id)}
			<button
				type="button"
				role="tab"
				aria-selected={active}
				onclick={() => app.setTab(t.id)}
				class={[
					'inline-flex min-h-[44px] shrink-0 items-center gap-[6px] border-b-2 px-[12px] text-[13px] font-semibold whitespace-nowrap',
					active ? 'border-brand text-ink-1' : 'border-transparent text-ink-2'
				]}
			>
				{t.name}
				{#if list.some((w) => w.prompts.length)}
					<span class="size-[6px] rounded-full bg-amber ring-2 ring-amber/40"></span><span class="sr-only">, waiting for your answer</span>
				{/if}
				<span class="font-mono text-[12px] font-normal text-ink-2">{list.length}</span>
			</button>
		{/each}
	</div>
	<button type="button" aria-label="New session" onclick={() => (app.newSessionOpen = true)} class="grid size-[44px] shrink-0 place-items-center text-ink-1">
		<Icon name="plus" size={18} />
	</button>
</div>

{#if menu && win && !win.busy}
	<SessionMenu target={{ kind: 'window', win }} x={menu.x} y={menu.y} anchor={menu.anchor} returnFocus={menu.anchor} inWindow onrename={canRename ? startRename : undefined} onclose={() => (menu = null)} />
{/if}
