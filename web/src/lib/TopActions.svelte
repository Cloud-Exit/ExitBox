<script lang="ts">
	import { app } from './app.svelte';
	import Button from './ui/Button.svelte';
	import IconButton from './ui/IconButton.svelte';

	const user = $derived(app.data?.user || app.me?.user || '');
</script>

<div class="flex shrink-0 items-center gap-1">
	<IconButton icon="help" label="Help and keys (?)" onclick={() => (app.helpOpen = true)} />
	<IconButton icon={app.theme === 'dark' ? 'sun' : 'moon'} label={app.theme === 'dark' ? 'Switch to light' : 'Switch to dark'} onclick={() => app.toggleTheme()} />
	{#if app.data?.authEnabled}
		<span class="mx-1 h-5 w-px bg-line-1"></span>
		{#if user}<span class="max-w-[140px] truncate font-mono text-[11.5px] text-ink-2" title={'Signed in as ' + user}>{user}</span>{/if}
		<Button size="sm" variant="ghost" icon="logout" title="Sign out. Your sessions keep running." onclick={() => app.signOut()}>Sign out</Button>
	{/if}
</div>
