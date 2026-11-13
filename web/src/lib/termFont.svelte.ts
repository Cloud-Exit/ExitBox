import { local, setLocal } from './api';

// Per browser, like the theme: installed fonts differ per machine.
export const FONT_DEFAULT = 'Consolas';
/** Shipped with the page, so it is there when Consolas is not installed. */
const FONT_BUNDLED = 'Atkinson Hyperlegible Mono';
export const SIZE_DEFAULT = 13;
export const LINE_DEFAULT = 1.15;
export const SIZE_MIN = 8;
export const SIZE_MAX = 32;
export const LINE_MIN = 1;
export const LINE_MAX = 2;

/** Terminal fonts; Consolas only where it is installed. */
export const TERM_FONTS = [FONT_DEFAULT, FONT_BUNDLED, 'Roboto Mono'] as const;
export const UI_FONT_DEFAULT = 'Atkinson Hyperlegible Next';
/** Interface fonts; Arial only where it is installed. */
export const UI_FONTS = [UI_FONT_DEFAULT, 'Roboto', 'Arial'] as const;

export type Weight = 400 | 500 | 700;
export const WEIGHTS: { value: Weight; label: string }[] = [
	{ value: 400, label: 'Regular' },
	{ value: 500, label: 'Medium' },
	{ value: 700, label: 'Bold' }
];
export const WEIGHT_DEFAULT: Weight = 400;

/** The weights each family has a face for; any other weight draws with the nearest face and looks the same. */
const FAMILY_WEIGHTS: Record<string, Weight[]> = {
	Consolas: [400, 700],
	[FONT_BUNDLED]: [400, 700],
	'Roboto Mono': [400, 500, 700],
	// Its Medium is the SemiBold face.
	'Atkinson Hyperlegible Next': [400, 500, 700],
	Roboto: [400, 500, 700],
	Arial: [400, 700]
};

export const weightsOf = (family: string): Weight[] => FAMILY_WEIGHTS[family] ?? [400, 700];

/** The weight itself when the family has it, else the nearest it has (the lighter on a tie). */
export function fitWeight(w: Weight, family: string): Weight {
	const have = weightsOf(family);
	if (have.includes(w)) return w;
	return have.reduce((best, v) => (Math.abs(v - w) < Math.abs(best - w) ? v : best));
}

// The bundled font comes first: a missing font then still looks as intended, and stays monospaced.
const MONO_FALLBACK = [FONT_BUNDLED, 'ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'];
const SANS_FALLBACK = [UI_FONT_DEFAULT, 'system-ui', 'sans-serif'];
const GENERIC = new Set(['monospace', 'ui-monospace', 'serif', 'sans-serif', 'system-ui', 'cursive', 'fantasy']);

/** A CSS font list for the chosen family, always ending in the fallback chain. */
export function fontStack(family: string, fallback = MONO_FALLBACK): string {
	const names = family
		.split(',')
		.map((n) => n.trim().replace(/^['"]|['"]$/g, '').trim())
		.filter(Boolean);
	const seen = new Set(names.map((n) => n.toLowerCase()));
	const all = [...names, ...fallback.filter((n) => !seen.has(n.toLowerCase()))];
	return all.map((n) => (GENERIC.has(n.toLowerCase()) || /^[A-Za-z][\w-]*$/.test(n) ? n : `'${n.replace(/'/g, '')}'`)).join(', ');
}

/** The saved family when it is one of the options (any case), else the default: older pages saved free text. */
function pick<T extends string>(saved: unknown, options: readonly T[]): T {
	const s = typeof saved === 'string' ? saved.trim().toLowerCase() : '';
	return options.find((o) => o.toLowerCase() === s) ?? options[0];
}

const pickWeight = (v: unknown): Weight => WEIGHTS.find((w) => w.value === v)?.value ?? WEIGHT_DEFAULT;

const clamp = (v: number, lo: number, hi: number, def: number) => (Number.isFinite(v) ? Math.min(hi, Math.max(lo, v)) : def);
export const clampSize = (v: number) => Math.round(clamp(v, SIZE_MIN, SIZE_MAX, SIZE_DEFAULT));
export const clampLine = (v: number) => Math.round(clamp(v, LINE_MIN, LINE_MAX, LINE_DEFAULT) * 100) / 100;

class TermFont {
	family = $state(pick(local<string>('termFont'), TERM_FONTS));
	weight = $state(fitWeight(pickWeight(local<number>('termFontWeight')), this.family));
	size = $state(clampSize(local<number>('termFontSize') ?? SIZE_DEFAULT));
	lineHeight = $state(clampLine(local<number>('termLineHeight') ?? LINE_DEFAULT));
	css = $derived(fontStack(this.family));
	/** No family here has a face above 700: with the base weight Bold, bold text looks like normal text. */
	readonly weightBold = 700;

	constructor() {
		// Drop a saved family that is not an option any more.
		const saved = local<string>('termFont');
		if (saved && saved !== this.family) setLocal('termFont', this.family === FONT_DEFAULT ? null : this.family);
	}

	setFamily(v: string) {
		this.family = pick(v, TERM_FONTS);
		setLocal('termFont', this.family === FONT_DEFAULT ? null : this.family);
		if (!weightsOf(this.family).includes(this.weight)) this.setWeight(fitWeight(this.weight, this.family));
	}

	setWeight(v: number) {
		this.weight = fitWeight(pickWeight(v), this.family);
		setLocal('termFontWeight', this.weight === WEIGHT_DEFAULT ? null : this.weight);
	}

	setSize(v: number) {
		this.size = clampSize(v);
		setLocal('termFontSize', this.size === SIZE_DEFAULT ? null : this.size);
	}

	setLineHeight(v: number) {
		this.lineHeight = clampLine(v);
		setLocal('termLineHeight', this.lineHeight === LINE_DEFAULT ? null : this.lineHeight);
	}

	/** Loads the faces a terminal draws with: xterm measures the cell size from them. */
	async load() {
		try {
			await Promise.all([
				document.fonts.load(`${this.weight} ${this.size}px ${this.css}`),
				document.fonts.load(`${this.weightBold} ${this.size}px ${this.css}`)
			]);
		} catch {
			// The fallback font works too.
		}
	}

	reset() {
		this.setFamily(FONT_DEFAULT);
		this.setWeight(WEIGHT_DEFAULT);
		this.setSize(SIZE_DEFAULT);
		this.setLineHeight(LINE_DEFAULT);
	}
}

export const termFont = new TermFont();

/** Weights of font-normal, -medium, -semibold and -bold per base weight; headings stay as heavy as the font allows. */
const UI_STEPS: Record<Weight, [number, number, number, number]> = {
	400: [400, 500, 600, 700],
	500: [500, 600, 700, 800],
	700: [700, 700, 800, 800]
};

class UiFont {
	family = $state(pick(local<string>('uiFont'), UI_FONTS));
	weight = $state(fitWeight(pickWeight(local<number>('uiFontWeight')), this.family));
	css = $derived(fontStack(this.family, SANS_FALLBACK));

	constructor() {
		const saved = local<string>('uiFont');
		if (saved && saved !== this.family) setLocal('uiFont', this.family === UI_FONT_DEFAULT ? null : this.family);
		this.apply();
	}

	setFamily(v: string) {
		this.family = pick(v, UI_FONTS);
		setLocal('uiFont', this.family === UI_FONT_DEFAULT ? null : this.family);
		if (!weightsOf(this.family).includes(this.weight)) this.setWeight(fitWeight(this.weight, this.family));
		else this.apply();
	}

	setWeight(v: number) {
		this.weight = fitWeight(pickWeight(v), this.family);
		setLocal('uiFontWeight', this.weight === WEIGHT_DEFAULT ? null : this.weight);
		this.apply();
	}

	/** Sets the theme's font variables on the root element; the defaults leave app.css in charge. */
	private apply() {
		if (typeof document === 'undefined') return;
		const st = document.documentElement.style;
		if (this.family === UI_FONT_DEFAULT) st.removeProperty('--font-sans');
		else st.setProperty('--font-sans', this.css);
		const names = ['normal', 'medium', 'semibold', 'bold'];
		UI_STEPS[this.weight].forEach((w, i) => {
			if (this.weight === WEIGHT_DEFAULT) st.removeProperty('--font-weight-' + names[i]);
			else st.setProperty('--font-weight-' + names[i], String(w));
		});
	}

	reset() {
		this.setFamily(UI_FONT_DEFAULT);
		this.setWeight(WEIGHT_DEFAULT);
	}
}

export const uiFont = new UiFont();
