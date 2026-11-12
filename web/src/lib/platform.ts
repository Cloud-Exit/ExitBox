// Key names differ on a Mac; the web UI shows the ones the user presses.

export const isMac = /Mac|iPhone|iPad/.test(navigator.platform);

const tmuxMods: Record<string, string> = { C: 'Ctrl', M: isMac ? 'Option' : 'Alt', S: 'Shift' };

/** A tmux key such as C-M-p as the keys to press, e.g. Ctrl+Alt+P. */
export function tmuxKeyLabel(key: string) {
	const parts = key.split('-');
	const last = parts.pop() ?? '';
	const mods = parts.map((m) => tmuxMods[m] ?? m);
	return [...mods, last.length === 1 ? last.toUpperCase() : last].join('+');
}
