// The phone layout: narrow, or a touch screen too short for windows (a phone in landscape). Same query as the max-md: variant in app.css.
const PHONE = '(width < 48rem), (pointer: coarse) and (width < 60rem) and (height < 500px)';

class Viewport {
	/** Phone layout: one session fills the screen, sheets replace dialogs. */
	phone = $state(false);
	/** The visual viewport height while an on-screen keyboard shrinks it (iOS keeps innerHeight), else 0. */
	height = $state(0);

	constructor() {
		if (typeof window === 'undefined') return;
		const mq = matchMedia(PHONE);
		this.phone = mq.matches;
		mq.addEventListener('change', (e) => (this.phone = e.matches));
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
}

export const viewport = new Viewport();
