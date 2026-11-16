// Touch scrolling for a terminal: xterm 6 scrolls on wheel events only.
import type { Terminal } from '@xterm/xterm';

/** Finger travel before a touch counts as a scroll, so taps and sideways swipes pass. */
const SLOP = 8;

/** Whole lines in a pixel distance, and the pixels left over. */
export function takeLines(px: number, cell: number): { lines: number; rest: number } {
	if (!(cell > 0)) return { lines: 0, rest: 0 };
	const lines = Math.trunc(px / cell);
	return { lines, rest: px - lines * cell };
}

/** A vertical drag scrolls the session; a flick keeps going and slows down. */
export function touchScroll(node: HTMLElement, t: Terminal) {
	let last: { x: number; y: number; t: number } | null = null;
	let start: { x: number; y: number } | null = null;
	let scrolling = false;
	let rest = 0;
	let speed = 0;
	let frame = 0;

	const cell = () => (t.element?.querySelector('.xterm-screen')?.clientHeight ?? 0) / t.rows;

	/** Moves by whole lines; the app gets wheel reports when it owns the mouse or the alternate screen. */
	function move(px: number, x: number, y: number) {
		const got = takeLines(rest + px, cell());
		rest = got.rest;
		if (!got.lines) return;
		if (t.modes.mouseTrackingMode === 'none' && t.buffer.active.type === 'normal') {
			t.scrollLines(-got.lines);
			return;
		}
		const target = t.element?.querySelector('.xterm-screen') ?? t.element;
		for (let i = 0; i < Math.abs(got.lines); i++) {
			target?.dispatchEvent(new WheelEvent('wheel', { deltaY: got.lines > 0 ? -1 : 1, deltaMode: WheelEvent.DOM_DELTA_LINE, clientX: x, clientY: y, bubbles: true, cancelable: true }));
		}
	}

	function stop() {
		if (frame) cancelAnimationFrame(frame);
		frame = 0;
		speed = 0;
	}

	function down(e: TouchEvent) {
		stop();
		const p = e.touches[0];
		scrolling = false;
		rest = 0;
		start = e.touches.length === 1 ? { x: p.clientX, y: p.clientY } : null;
		last = start ? { ...start, t: e.timeStamp } : null;
	}

	function drag(e: TouchEvent) {
		const p = e.touches[0];
		if (!start || !last || e.touches.length !== 1) return;
		if (!scrolling) {
			const dx = Math.abs(p.clientX - start.x);
			const dy = Math.abs(p.clientY - start.y);
			if (dy < SLOP || dy < dx) return;
			scrolling = true;
		}
		// The page must not scroll or pull to refresh under the session.
		if (e.cancelable) e.preventDefault();
		const dt = Math.max(1, e.timeStamp - last.t);
		const dy = p.clientY - last.y;
		speed = 0.6 * speed + 0.4 * (dy / dt);
		last = { x: p.clientX, y: p.clientY, t: e.timeStamp };
		move(dy, p.clientX, p.clientY);
	}

	function up(e: TouchEvent) {
		const at = last;
		const was = scrolling;
		start = null;
		last = null;
		scrolling = false;
		// A finger that rested before lifting does not fling.
		if (!was || !at || e.touches.length || e.timeStamp - at.t > 80 || Math.abs(speed) < 0.3) return;
		let prev = performance.now();
		const glide = (now: number) => {
			const dt = Math.min(50, now - prev);
			prev = now;
			move(speed * dt, at.x, at.y);
			speed *= Math.pow(0.996, dt);
			frame = Math.abs(speed) > 0.05 ? requestAnimationFrame(glide) : 0;
		};
		frame = requestAnimationFrame(glide);
	}

	node.addEventListener('touchstart', down, { passive: true });
	node.addEventListener('touchmove', drag, { passive: false });
	node.addEventListener('touchend', up, { passive: true });
	node.addEventListener('touchcancel', up, { passive: true });
	return () => {
		stop();
		node.removeEventListener('touchstart', down);
		node.removeEventListener('touchmove', drag);
		node.removeEventListener('touchend', up);
		node.removeEventListener('touchcancel', up);
	};
}
