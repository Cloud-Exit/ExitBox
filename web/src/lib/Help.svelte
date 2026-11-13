<script lang="ts">
	import { app } from './app.svelte';
	import { isMac, tmuxKeyLabel } from './platform';
	import IconButton from './ui/IconButton.svelte';
	import Kbd from './ui/Kbd.svelte';
	import Modal from './ui/Modal.svelte';
	import SectionLabel from './ui/SectionLabel.svelte';
	import { viewport } from './viewport.svelte';

	/** keys: alternatives, each a sequence of key presses; act: a mouse action instead. */
	type Item = { keys?: string[][]; act?: string; text: string };

	// The menu keys live in the settings; load them once if Settings was not opened yet.
	if (!app.settings) void app.loadSettings();
	const workspaceKey = $derived(tmuxKeyLabel(app.settings?.workspaceMenu || app.settings?.keybindingDefaults?.workspaceMenu || 'C-M-p'));
	const sessionKey = $derived(tmuxKeyLabel(app.settings?.sessionMenu || app.settings?.keybindingDefaults?.sessionMenu || 'C-M-s'));

	const sections = $derived<{ title: string; items: Item[] }[]>([
		{
			title: 'Windows',
			items: [
				{ act: 'Drag a title bar', text: app.autoDock ? 'Drop on a side of a window to place it there; the centre swaps' : 'Move the window' },
				...(app.autoDock ? [{ act: 'Drop at the desktop edge', text: 'Take that whole side of the tab' }] : []),
				{ act: 'Drag an edge', text: 'Resize' },
				{ act: 'Double-click a title', text: 'Maximize or restore' },
				{ act: 'Windows buttons', text: 'Main + stack, Columns and Grid tile and reset a custom tab; Free floats' },
				{ act: 'Drop on a tab', text: 'Move a window or sidebar session to that tab' }
			]
		},
		{
			title: 'Tabs',
			items: [
				{ act: 'Drag a tab', text: 'Reorder tabs' },
				{ keys: [[(isMac ? 'Option' : 'Alt') + '+Shift+Left'], ['Right']], text: 'Move the focused tab' },
				{ act: 'Double-click a tab', text: 'Rename it' }
			]
		},
		{
			title: 'Terminal',
			items: isMac
				? [
						{ keys: [['Cmd+V']], text: 'Paste' },
						{ keys: [['Option+drag']], text: 'Select while the agent uses the mouse' },
						{ act: 'Select text', text: 'Copies when you release' },
						{ keys: [['Cmd+C']], text: 'Copy the selection' },
						{ keys: [['Shift+Enter']], text: 'New line in the prompt' }
					]
				: [
						{ keys: [['Ctrl+V'], ['Shift+Insert']], text: 'Paste' },
						{ keys: [['Shift+drag']], text: 'Select while the agent uses the mouse' },
						{ act: 'Select text', text: 'Copies when you release' },
						{ keys: [['Ctrl+C']], text: 'Copy a selection, else interrupt' },
						{ keys: [['Ctrl+Shift+C']], text: 'Copy the selection' },
						{ keys: [['Shift+Enter']], text: 'New line in the prompt' }
					]
		},
		{
			title: 'Sessions',
			items: [
				{ keys: [['Ctrl+B', 'd']], text: 'Detach: the session keeps running' },
				{ keys: [[workspaceKey]], text: 'Workspace menu' },
				{ keys: [[sessionKey]], text: 'Session menu' },
				{ act: 'Cog on a window', text: 'Session settings and launch command' },
				{ act: 'Hammer on a window', text: 'Rebuild the agent image and start again' },
				{ act: 'New session > SSH', text: 'A terminal on your own machine over ssh' },
				{ act: 'Close a window', text: 'End the session or detach from it' },
				{ keys: [['?']], text: 'This help, outside a terminal' }
			]
		}
	]);

	// A phone has no windows to drag, no mouse and no ? key: its own taps instead.
	const phoneSections = $derived<{ title: string; items: Item[] }[]>([
		{
			title: 'Phone',
			items: [
				{ act: 'List button', text: 'Sessions, folders and settings' },
				{ act: 'Tap a tab', text: 'Show the session focused there' },
				{ act: 'More button',text: 'Session actions: rename, folder, tab, settings, rebuild, detach' },
				{ act: 'Long press a row', text: 'Its actions, as the More button' }
			]
		},
		{
			title: 'Keys',
			items: [
				{ act: 'Tap the terminal', text: 'Type with the on-screen keyboard' },
				{ act: 'ctrl, alt', text: 'Apply to the next key, then release' },
				{ act: 'Send', text: 'Types the line, then Enter' },
				{ act: 'Send, empty', text: 'Just Enter' }
			]
		},
		{
			title: 'Sessions',
			items: [
				{ keys: [['Ctrl+B', 'd']], text: 'Detach: the session keeps running' },
				{ keys: [[workspaceKey]], text: 'Workspace menu' },
				{ keys: [[sessionKey]], text: 'Session menu' },
				{ act: 'New session > SSH', text: 'A terminal on your own machine over ssh' }
			]
		}
	]);

	const close = () => (app.helpOpen = false);
</script>

<Modal label="Help" onclose={close} wide>
	<div class="flex items-center gap-3 border-b border-line-1 px-5 py-4 max-md:min-h-[52px] max-md:py-0 max-md:pr-[4px] max-md:pl-[16px]">
		<h2 class="flex-1 text-xl font-bold max-md:text-[15px]">Help</h2>
		<IconButton icon="x" label="Close" onclick={close} />
	</div>
	<div class="grid min-h-0 grid-cols-2 gap-x-8 gap-y-5 overflow-auto px-5 py-[18px] max-md:grid-cols-1 max-md:px-[12px] max-md:py-[16px]">
		{#each viewport.phone ? phoneSections : sections as s (s.title)}
			<section class="flex flex-col gap-2">
				<SectionLabel>{s.title}</SectionLabel>
				<dl class="grid grid-cols-[minmax(0,auto)_1fr] items-baseline gap-x-4 gap-y-1.5 text-[12.5px] max-md:gap-y-[10px] max-md:text-[14px]">
					{#each s.items as it (it.text)}
						<dt class="whitespace-nowrap text-ink-1">
							{#if it.keys}
								{#each it.keys as alt, i (i)}
									{#if i}<span class="mx-1 text-ink-3">or</span>{/if}
									{#each alt as k, j (j)}{#if j}{' '}{/if}<Kbd>{k}</Kbd>{/each}
								{/each}
							{:else}
								<span class="font-semibold">{it.act}</span>
							{/if}
						</dt>
						<dd class="text-ink-2">{it.text}</dd>
					{/each}
				</dl>
			</section>
		{/each}
	</div>
	{#if app.settingsError && !app.settings}
		<div class="border-t border-line-1 px-5 py-2.5 text-[11.5px] text-ink-2">Menu keys show the defaults: {app.settingsError}</div>
	{/if}
</Modal>
