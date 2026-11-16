// Tells an on-screen keyboard from a focused field: hiding the keyboard does not blur the field.

/** Less height than this below the tallest seen is a keyboard, not a browser bar. */
const KEYBOARD_MIN = 120;

export type Full = { w: number; h: number };

/** The tallest viewport seen at this width; a new width (rotation) starts over. */
export function tallest(prev: Full | null, w: number, h: number): Full {
	return prev && prev.w === w ? { w, h: Math.max(prev.h, h) } : { w, h };
}

export function keyboardUp(h: number, full: Full | null): boolean {
	return !!full && h < full.h - KEYBOARD_MIN;
}
