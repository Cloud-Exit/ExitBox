// The phone layout: narrow, or a touch screen too short for windows (a phone in landscape). Same query as the max-md: variant in app.css.
const PHONE = '(width < 48rem), (pointer: coarse) and (width < 60rem) and (height < 500px)';

const FOCUS_KEY = 'exitbox.phoneFocus';
/** Fields that type into the session. */
const TYPING = 'form[data-keys] input, section[data-win-id] textarea';

class Viewport {
	/** Phone layout: one session fills the screen, sheets replace dialogs. */
	phone = $state(false);
	/** The visual viewport height while an on-screen keyboard shrinks it (iOS keeps innerHeight), else 0. */
	height = $state(0);
	/** Phone focus: only the session, its keys and the text field show. */
	focus = $state(false);
	/** A touch screen types into the session, so its keyboard is up. */
	keyboard = $state(false);

	/** Header, tabs and chips are out of the way: by the focus button, or while the keyboard is up. */
	get bare() {
		return this.focus || this.keyboard;
	}

	constructor() {
		if (typeof window === 'undefined') return;
		try {
			this.focus = localStorage.getItem(FOCUS_KEY) === '1';
		} catch {
			// Private windows refuse storage; focus then starts off.
		}
		const mq = matchMedia(PHONE);
		this.phone = mq.matches;
		mq.addEventListener('change', (e) => (this.phone = e.matches));
		let blur: ReturnType<typeof setTimeout> | null = null;
		const touch = matchMedia('(pointer: coarse)');
		addEventListener('focusin', (e) => {
			if (!(e.target instanceof Element) || !e.target.matches(TYPING)) return;
			if (blur) clearTimeout(blur);
			this.keyboard = touch.matches;
		});
		addEventListener('focusout', () => {
			if (blur) clearTimeout(blur);
			// A session switch moves the focus to the next field; the header must not flash in between.
			blur = setTimeout(() => (this.keyboard = false), 250);
		});
		const vv = window.visualViewport;
		if (!vv) return;
		const update = () => {
			// Pinch zoom shrinks the visual viewport too; only an unzoomed one is a keyboard.
			const unzoomed = Math.abs(vv.scale - 1) < 0.01;
			this.height = unzoomed && vv.height < innerHeight - 1 ? Math.round(vv.height) : 0;
			// iOS scrolls the page to show a focused field; the layout already moved above the keyboard.
			if (unzoomed && this.phone && scrollY) scrollTo(0, 0);
		};
		vv.addEventListener('resize', update);
		vv.addEventListener('scroll', update);
		update();
	}

	toggleFocus() {
		this.focus = !this.focus;
		try {
			localStorage.setItem(FOCUS_KEY, this.focus ? '1' : '0');
		} catch {
			// Not remembered, still switched.
		}
	}
}

export const viewport = new Viewport();
