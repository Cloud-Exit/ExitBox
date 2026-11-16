import { expect, test } from 'bun:test';
import { claimsOnConnect, claimsOnFree, claimSize, fitScale, fitSize, heardSize, initialSize, mayFit, reconnectSize, releaseSize, validSize, type Size, type SizeState } from './termSize';

const wide: Size = { cols: 150, rows: 40 };
const phone: Size = { cols: 34, rows: 20 };

/** A page that claimed and sent its fit. */
function sentFit(size: Size): SizeState {
	const r = fitSize(claimSize(initialSize(), true), size);
	expect(r.send).toBe(true);
	return r.state;
}

test('a new page is no owner and fits locally without sending', () => {
	const s = initialSize();
	expect(s.owner).toBe(false);
	expect(mayFit(s)).toBe(true);
	expect(fitSize(s, wide).send).toBe(false);
});

test('an owner sends each new fit once', () => {
	let s = sentFit(wide);
	expect(s.awaiting).toBe(true);
	expect(fitSize(s, wide).send).toBe(false);
	const r = fitSize(s, phone);
	expect(r.send).toBe(true);
	s = r.state;
	expect(s.sent).toBe('34x20');
});

test('its own size echoed keeps a page owner', () => {
	const r = heardSize(sentFit(wide), wide);
	expect(r.state.owner).toBe(true);
	expect(r.state.awaiting).toBe(false);
	expect(r.state.pty).toEqual(wide);
	expect(r.resize).toBeNull();
});

test('older sizes before the echo are ignored', () => {
	const s = sentFit(wide);
	// The attach message (the size before this page claimed) arrives first.
	const r = heardSize(s, phone);
	expect(r.state).toBe(s);
	expect(r.resize).toBeNull();
	expect(heardSize(r.state, wide).state.owner).toBe(true);
});

test('another size makes a page draw at it and stop sending', () => {
	const s = heardSize(sentFit(wide), wide).state;
	const r = heardSize(s, phone);
	expect(r.state.owner).toBe(false);
	expect(r.resize).toEqual(phone);
	expect(mayFit(r.state)).toBe(false);
	expect(fitSize(r.state, { cols: 120, rows: 30 }).send).toBe(false);
});

test('a non-owner stays one when the size goes back to what it sent', () => {
	let s = heardSize(sentFit(wide), wide).state;
	s = heardSize(s, phone).state;
	const r = heardSize(s, wide);
	expect(r.state.owner).toBe(false);
	expect(r.resize).toEqual(wide);
});

test('claiming sends again, even the size sent before', () => {
	let s = heardSize(sentFit(wide), wide).state;
	s = heardSize(s, phone).state;
	s = claimSize(s);
	expect(s.owner).toBe(true);
	expect(mayFit(s)).toBe(true);
	expect(fitSize(s, wide).send).toBe(true);
});

test('an owner claiming without force sends nothing new', () => {
	const s = heardSize(sentFit(wide), wide).state;
	expect(claimSize(s)).toBe(s);
	expect(fitSize(claimSize(s), wide).send).toBe(false);
	expect(fitSize(claimSize(s, true), wide).send).toBe(true);
});

test('a hidden page that reconnects follows the size it hears', () => {
	// Sent, then the connection dropped before the echo.
	let s = reconnectSize(sentFit(wide));
	expect(s.awaiting).toBe(false);
	const r = heardSize(s, phone);
	expect(r.state.owner).toBe(false);
	expect(r.resize).toEqual(phone);
	// Its own size, still in force: it stays owner.
	s = reconnectSize(heardSize(sentFit(wide), wide).state);
	expect(heardSize(s, wide).state.owner).toBe(true);
});

test('a claim that could not measure yields to the size it hears', () => {
	const s = claimSize(initialSize(), true);
	const r = heardSize(s, phone);
	expect(r.state.owner).toBe(false);
	expect(r.resize).toEqual(phone);
});

test('sizes the server refuses are neither sent nor taken', () => {
	expect(validSize({ cols: 2, rows: 1 })).toBe(false);
	expect(validSize({ cols: 1001, rows: 30 })).toBe(false);
	expect(validSize({ cols: 80, rows: 24 })).toBe(true);
	const s = claimSize(initialSize(), true);
	expect(fitSize(s, { cols: 30, rows: 1 }).send).toBe(false);
	expect(heardSize(s, { cols: 0, rows: 0 }).state).toBe(s);
});

/** Two pages claim at once; the server applies their sizes in either order and tells both. */
for (const order of [
	['a', 'b'],
	['b', 'a']
] as const) {
	test(`two screens agree when the server takes ${order.join(' then ')}`, () => {
		const fits = { a: wide, b: phone };
		const pages: Record<'a' | 'b', { s: SizeState; xterm: Size }> = {
			a: { s: sentFit(fits.a), xterm: fits.a },
			b: { s: sentFit(fits.b), xterm: fits.b }
		};
		for (const id of ['a', 'b'] as const) {
			// Each first hears the size from before both claims.
			pages[id].s = heardSize(pages[id].s, { cols: 80, rows: 24 }).state;
			for (const from of order) {
				const r = heardSize(pages[id].s, fits[from]);
				pages[id].s = r.state;
				if (r.resize) pages[id].xterm = r.resize;
			}
		}
		const last = fits[order[1]];
		expect(pages.a.xterm).toEqual(last);
		expect(pages.b.xterm).toEqual(last);
		expect(pages[order[1]].s.owner).toBe(true);
		expect(pages[order[0]].s.owner).toBe(false);
	});
}

test('connecting while others view the session does not take the size', () => {
	expect(claimsOnConnect(true, { others: true })).toBe(false);
	// The attach message alone makes the new page draw at the session's size.
	const r = heardSize(initialSize(), wide);
	expect(r.state.owner).toBe(false);
	expect(r.resize).toEqual(wide);
});

test('connecting alone, or with no size from the server, takes the size', () => {
	expect(claimsOnConnect(true, { others: false })).toBe(true);
	expect(claimsOnConnect(true, null)).toBe(true);
});

test('a page in the background never takes the size on connect', () => {
	expect(claimsOnConnect(false, null)).toBe(false);
	expect(claimsOnConnect(false, { others: false })).toBe(false);
});

test('free is taken only by a window that does not hold the size', () => {
	const owner = heardSize(sentFit(wide), wide).state;
	expect(claimsOnFree(owner)).toBe(false);
	const other = heardSize(owner, phone).state;
	expect(claimsOnFree(other)).toBe(true);
	expect(claimsOnFree(initialSize())).toBe(true);
});

test('a released owner keeps its size and sends it again when back', () => {
	const s = releaseSize(heardSize(sentFit(wide), wide).state);
	expect(s.owner).toBe(true);
	expect(mayFit(s)).toBe(true);
	expect(fitSize(s, wide).send).toBe(true);
	// Another screen took it meanwhile: this one follows.
	expect(heardSize(s, phone).state.owner).toBe(false);
	const other = heardSize(s, phone).state;
	expect(releaseSize(other)).toBe(other);
});

test('a larger screen is scaled down to fit, never up', () => {
	expect(fitScale({ w: 400, h: 800 }, { w: 1200, h: 600 })).toBeCloseTo(1 / 3);
	expect(fitScale({ w: 1200, h: 300 }, { w: 1200, h: 600 })).toBe(0.5);
	expect(fitScale({ w: 1600, h: 900 }, { w: 300, h: 200 })).toBe(1);
	expect(fitScale({ w: 0, h: 0 }, { w: 300, h: 200 })).toBe(1);
});
