<script module lang="ts">
	/** A session's state in a list: running here, running elsewhere, ended lately, ended long ago, or asking. */
	export type SessionState = 'active' | 'detached' | 'ended' | 'inactive' | 'waiting';

	export const STATE_LABEL: Record<SessionState, string> = {
		active: 'Active',
		detached: 'Detached',
		ended: 'Recently ended',
		inactive: 'Inactive',
		waiting: 'Waiting for approval'
	};
</script>

<script lang="ts">
	let { state, size = 12 }: { state: SessionState; size?: number } = $props();
</script>

<span
	role="img"
	aria-label={STATE_LABEL[state]}
	title={STATE_LABEL[state]}
	class={[
		'grid shrink-0 place-items-center',
		state === 'active' && 'text-st-active',
		state === 'detached' && 'text-st-detached',
		state === 'ended' && 'text-st-ended',
		state === 'inactive' && 'text-st-inactive',
		state === 'waiting' && 'rounded-full bg-amber-soft text-amber ring-2 ring-amber/40'
	]}
>
	<svg xmlns="http://www.w3.org/2000/svg" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
		<polyline points="8 5 16 12 8 19" />
	</svg>
</span>
