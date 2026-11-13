// The phone key bar: terminal bytes for its keys and its sticky ctrl and alt.

export type KeyName = 'esc' | 'tab' | 'up' | 'down' | 'enter';

/** One-shot modifiers: armed on the key bar, used by the next key, then released. */
export const mods = $state({ ctrl: false, alt: false });

export function releaseMods() {
	mods.ctrl = false;
	mods.alt = false;
}

function ctrlChar(c: string) {
	const code = c.toUpperCase().charCodeAt(0);
	// @, A to Z, [ \ ] ^ _ map to 0 to 31, as a terminal sends them.
	if (code >= 64 && code <= 95) return String.fromCharCode(code - 64);
	if (c === ' ') return '\x00';
	if (c === '?') return '\x7f';
	return c;
}

/** Typed text with the armed modifiers applied to its first key; escape sequences (focus reports, arrows) pass through. */
export function withMods(d: string) {
	if (!d || (!mods.ctrl && !mods.alt) || d.startsWith('\x1b')) return d;
	const first = mods.ctrl ? ctrlChar(d[0]) : d[0];
	const out = (mods.alt ? '\x1b' : '') + first + d.slice(1);
	releaseMods();
	return out;
}

/** The bytes of a key bar key with the armed modifiers, xterm style; releases them. */
export function keyBytes(k: KeyName, appCursor: boolean) {
	let out: string;
	if (k === 'up' || k === 'down') {
		const c = k === 'up' ? 'A' : 'B';
		const m = 1 + (mods.alt ? 2 : 0) + (mods.ctrl ? 4 : 0);
		out = m > 1 ? `\x1b[1;${m}${c}` : (appCursor ? '\x1bO' : '\x1b[') + c;
	} else {
		out = (mods.alt ? '\x1b' : '') + { esc: '\x1b', tab: '\t', enter: '\r' }[k];
	}
	releaseMods();
	return out;
}
