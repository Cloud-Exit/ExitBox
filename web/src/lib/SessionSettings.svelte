<script lang="ts">
	import { runCommand, shellQuote, sshCommand } from './api';
	import type { State } from './api';
	import { app } from './app.svelte';
	import type { Win } from './app.svelte';
	import { copyText } from './clipboard';
	import NewSession from './NewSession.svelte';
	import Button from './ui/Button.svelte';
	import IconButton from './ui/IconButton.svelte';
	import Modal from './ui/Modal.svelte';
	import SectionLabel from './ui/SectionLabel.svelte';

	let { data, win }: { data: State; win: Win } = $props();

	const info = $derived(win.info);
	// Mirrors sessionCommand in cmd/web.go.
	const command = $derived(
		info.kind === 'ssh'
			? sshCommand(info.args)
			: info.kind === 'attach'
			? [data.runtime || 'podman', 'exec', '-it', '-e', 'TERM=xterm-256color', info.container || '<container>', 'tmux', 'attach-session', '-t', 'exitbox-' + info.agent]
					.map(shellQuote)
					.join(' ')
			: runCommand(info.dir, info.args)
	);
	const rows = $derived(
		[
			['Agent', info.agent ? app.agentDisplay(info.agent) : ''],
			['Workspace', info.workspace],
			['Directory', app.tilde(info.dir)],
			['Container', info.container ?? ''],
			['Session name', info.name]
		].filter(([, v]) => v)
	);

	const close = () => (app.sessionSettings = null);

	async function copy() {
		if (await copyText(command)) app.notify('Command copied');
		else app.notify('The browser did not allow copying', true);
	}
</script>

<!-- An SSH session edits its options in the New session dialog too. -->
{#if (info.kind === 'run' && info.request) || (info.kind === 'ssh' && info.ssh)}
	<NewSession {data} edit={win} />
{:else}
	<Modal label="Session settings" onclose={close}>
		<div class="flex items-center gap-3 border-b border-line-1 px-5 py-4 max-md:min-h-[52px] max-md:py-0 max-md:pr-[4px] max-md:pl-[16px]">
			<div class="flex flex-1 flex-col gap-0.5">
				<span class="inline-flex items-center gap-1.5 font-mono text-[10.5px] tracking-[0.08em] text-ink-2 uppercase max-md:hidden">
					<span class="size-1.5 rounded-full bg-brand"></span>{info.kind === 'attach' ? 'Started in a terminal' : 'Session'}
				</span>
				<h2 class="text-xl font-bold max-md:text-[15px]">Session settings</h2>
			</div>
			<IconButton icon="x" label="Close" onclick={close} />
		</div>
		<div class="flex flex-col gap-[18px] overflow-auto px-5 py-[18px] max-md:flex-1 max-md:px-[12px] max-md:py-[16px]">
			<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-[13px]">
				{#each rows as [k, v] (k)}
					<dt class="text-ink-2">{k}</dt>
					<dd class={['min-w-0 break-all', (k === 'Directory' || k === 'Container') && 'font-mono text-xs']}>{v}</dd>
				{/each}
			</dl>
			<div class="flex flex-col gap-2">
				<div class="flex items-center gap-2">
					<SectionLabel>{info.kind === 'attach' ? 'Attached with' : 'Runs as'}</SectionLabel>
					<IconButton icon="copy" label="Copy command" small onclick={copy} />
				</div>
				<div class="rounded-lg border border-line-1 bg-inset px-3 py-2.5 font-mono text-xs break-all whitespace-pre-wrap">{command}</div>
				{#if info.kind === 'run'}
					<small class="text-[11.5px] text-ink-2">Started before settings were kept, so they cannot be changed here.</small>
				{/if}
			</div>
		</div>
		<div class="flex items-center gap-2 border-t border-line-1 px-5 py-3.5 max-md:px-[12px] max-md:py-[8px]">
			<Button variant="ghost" class="max-md:w-full max-md:justify-center max-md:border-line-2" onclick={close}>Close</Button>
		</div>
	</Modal>
{/if}
