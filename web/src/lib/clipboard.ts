/** Largest text an OSC 52 copy may put on the clipboard. */
export const OSC52_MAX_BYTES = 1 << 20;

/**
 * The text of an OSC 52 "set clipboard" payload (`<targets>;<base64>`),
 * or null for anything else. Queries (`?`) are null: answering them would
 * hand the browser's clipboard to the container.
 */
export function parseOsc52(data: string): string | null {
	const sep = data.indexOf(';');
	if (sep < 0) return null;
	const b64 = data.slice(sep + 1).replace(/\s+/g, '');
	if (!b64 || b64 === '?') return null;
	if (Math.floor((b64.length * 3) / 4) > OSC52_MAX_BYTES + 2) return null;
	let bin: string;
	try {
		bin = atob(b64);
	} catch {
		return null;
	}
	if (bin.length > OSC52_MAX_BYTES) return null;
	const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
	return new TextDecoder().decode(bytes);
}

/** Copies through a hidden textarea; works outside secure contexts. */
function execCopy(text: string): boolean {
	const prev = document.activeElement as HTMLElement | null;
	const ta = document.createElement('textarea');
	ta.value = text;
	ta.setAttribute('readonly', '');
	ta.className = 'pointer-events-none fixed top-0 left-0 opacity-0';
	document.body.appendChild(ta);
	ta.select();
	let ok = false;
	try {
		ok = document.execCommand('copy');
	} catch {
		ok = false;
	}
	ta.remove();
	prev?.focus({ preventScroll: true });
	return ok;
}

/** Puts text on the system clipboard; false when the browser refuses. */
export async function copyText(text: string): Promise<boolean> {
	// navigator.clipboard is missing on plain http to a LAN address.
	if (navigator.clipboard?.writeText) {
		try {
			await navigator.clipboard.writeText(text);
			return true;
		} catch {
			// Fall through: no focus or permission.
		}
	}
	return execCopy(text);
}
