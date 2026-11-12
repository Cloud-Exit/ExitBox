import { local, setLocal } from './api';

// Per browser, like the theme: installed fonts differ per machine.
export const FONT_DEFAULT = 'Atkinson Hyperlegible Mono';
export const SIZE_DEFAULT = 13;
export const LINE_DEFAULT = 1.15;
export const SIZE_MIN = 8;
export const SIZE_MAX = 32;
export const LINE_MIN = 1;
export const LINE_MAX = 2;

/** Suggestions; any installed font name works. */
export const FONT_SUGGESTIONS = [
	FONT_DEFAULT,
	'JetBrains Mono',
	'Fira Code',
	'Cascadia Code',
	'SF Mono',
	'Menlo',
	'Consolas',
	'DejaVu Sans Mono',
	'monospace'
];

const FALLBACK = ['ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'];
const GENERIC = new Set(['monospace', 'ui-monospace', 'serif', 'sans-serif', 'system-ui', 'cursive', 'fantasy']);

/** A CSS font list for the chosen family, always ending in the fallback chain. */
export function fontStack(family: string): string {
	const names = family
		.split(',')
		.map((n) => n.trim().replace(/^['"]|['"]$/g, '').trim())
		.filter(Boolean);
	const seen = new Set(names.map((n) => n.toLowerCase()));
	const all = [...names, ...FALLBACK.filter((n) => !seen.has(n.toLowerCase()))];
	return all.map((n) => (GENERIC.has(n.toLowerCase()) || /^[A-Za-z][\w-]*$/.test(n) ? n : `'${n.replace(/'/g, '')}'`)).join(', ');
}

const clamp = (v: number, lo: number, hi: number, def: number) => (Number.isFinite(v) ? Math.min(hi, Math.max(lo, v)) : def);
export const clampSize = (v: number) => Math.round(clamp(v, SIZE_MIN, SIZE_MAX, SIZE_DEFAULT));
export const clampLine = (v: number) => Math.round(clamp(v, LINE_MIN, LINE_MAX, LINE_DEFAULT) * 100) / 100;

class TermFont {
	family = $state(local<string>('termFont')?.trim() || FONT_DEFAULT);
	size = $state(clampSize(local<number>('termFontSize') ?? SIZE_DEFAULT));
	lineHeight = $state(clampLine(local<number>('termLineHeight') ?? LINE_DEFAULT));
	css = $derived(fontStack(this.family));

	setFamily(v: string) {
		this.family = v.trim() || FONT_DEFAULT;
		setLocal('termFont', this.family === FONT_DEFAULT ? null : this.family);
	}

	setSize(v: number) {
		this.size = clampSize(v);
		setLocal('termFontSize', this.size === SIZE_DEFAULT ? null : this.size);
	}

	setLineHeight(v: number) {
		this.lineHeight = clampLine(v);
		setLocal('termLineHeight', this.lineHeight === LINE_DEFAULT ? null : this.lineHeight);
	}

	reset() {
		this.setFamily(FONT_DEFAULT);
		this.setSize(SIZE_DEFAULT);
		this.setLineHeight(LINE_DEFAULT);
	}
}

export const termFont = new TermFont();
