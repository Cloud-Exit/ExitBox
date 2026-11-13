<script lang="ts">
	let { size = 14, label = '' }: { size?: number; label?: string } = $props();
</script>

<svg
	xmlns="http://www.w3.org/2000/svg"
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	role={label ? 'img' : undefined}
	aria-label={label || undefined}
	aria-hidden={label ? undefined : 'true'}
	class="shrink-0 animate-spin"
>
	<circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="3" class="opacity-25" />
	<path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" stroke-width="3" stroke-linecap="round" />
</svg>
