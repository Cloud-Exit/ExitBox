import { describe, expect, test } from 'bun:test';
import { applyZone, DEFAULTS, edgeAt, fromRects, insertAt, insertAuto, insertRoot, layout, leaves, MAX_DEPTH, MAX_LEAVES, MIN_H, MIN_W, moveEdge, prune, remove, rename, resizer, sanitize, swap, zoneAt, type Node, type Rect, type Split } from './tiling';

const desk = { w: 1600, h: 900 };
const G = DEFAULTS.gap;

function overlaps(a: Rect, b: Rect) {
	return a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
}

/** No two tiles overlap and together with the gaps they cover the desk. */
function checkTiles(rects: Map<string, Rect>) {
	const list = [...rects.values()];
	for (let i = 0; i < list.length; i++) {
		const r = list[i];
		expect(r.x).toBeGreaterThanOrEqual(G);
		expect(r.y).toBeGreaterThanOrEqual(G);
		expect(r.x + r.w).toBeLessThanOrEqual(desk.w - G);
		expect(r.y + r.h).toBeLessThanOrEqual(desk.h - G);
		for (let j = i + 1; j < list.length; j++) expect(overlaps(r, list[j])).toBe(false);
	}
	// Every point of the inner area is in a tile or within a gap of one.
	for (let x = G; x < desk.w - G; x += 37) {
		for (let y = G; y < desk.h - G; y += 29) {
			const near = list.some((r) => x >= r.x - G && x <= r.x + r.w + G && y >= r.y - G && y <= r.y + r.h + G);
			expect(near).toBe(true);
		}
	}
}

const row = (...ids: string[]): Node => ({ dir: 'row', kids: ids.map((id) => ({ id })), sizes: ids.map(() => 1) });

describe('layout', () => {
	test('one leaf fills the desk inside the gap', () => {
		expect(layout({ id: 'a' }, desk).get('a')).toEqual({ x: G, y: G, w: desk.w - 2 * G, h: desk.h - 2 * G });
	});

	test('nested splits never overlap and fill the desk', () => {
		const t: Node = {
			dir: 'column',
			kids: [row('a', 'b', 'c'), { dir: 'row', kids: [{ id: 'd' }, { dir: 'column', kids: [{ id: 'e' }, { id: 'f' }], sizes: [2, 1] }], sizes: [1, 3] }],
			sizes: [1, 1]
		};
		const r = layout(t, desk);
		expect([...r.keys()].sort()).toEqual(['a', 'b', 'c', 'd', 'e', 'f']);
		checkTiles(r);
	});

	test('a tiny weight still gets the minimum size', () => {
		const t: Node = { dir: 'row', kids: [{ id: 'a' }, { id: 'b' }], sizes: [100, 1] };
		expect(layout(t, desk).get('b')!.w).toBeGreaterThanOrEqual(MIN_W);
	});
});

describe('fromRects', () => {
	test('reproduces a grid with a short last row', () => {
		const t: Node = { dir: 'column', kids: [row('a', 'b', 'c'), row('d', 'e')], sizes: [3, 2] };
		const before = layout(t, desk);
		const built = fromRects(before)!;
		expect(layout(built, desk)).toEqual(before);
	});

	test('reproduces main + stack', () => {
		const t: Node = { dir: 'row', kids: [{ id: 'm' }, { dir: 'column', kids: [{ id: 'a' }, { id: 'b' }, { id: 'c' }], sizes: [1, 2, 1] }], sizes: [56, 44] };
		const before = layout(t, desk);
		expect(layout(fromRects(before)!, desk)).toEqual(before);
	});

	test('reproduces float rectangles within a pixel', () => {
		const rects = new Map<string, Rect>([
			['a', { x: 8, y: 8, w: 523.33, h: 884 }],
			['b', { x: 539.33, y: 8, w: 523.33, h: 884 }],
			['c', { x: 1070.67, y: 8, w: 521.33, h: 884 }]
		]);
		const out = layout(fromRects(rects)!, desk);
		for (const [id, r] of rects) {
			const o = out.get(id)!;
			expect(Math.abs(o.x - r.x)).toBeLessThanOrEqual(1);
			expect(Math.abs(o.w - r.w)).toBeLessThanOrEqual(1);
		}
	});
});

describe('insert', () => {
	const two = row('a', 'b');
	const base = layout(two, desk);

	test('right of a leaf halves it', () => {
		const r = layout(insertAt(two, 'n', 'a', 'right'), desk);
		const a = r.get('a')!;
		const n = r.get('n')!;
		expect(n.x).toBeGreaterThan(a.x);
		expect(n.y).toBe(a.y);
		expect(a.x + a.w + G + n.w).toBeCloseTo(base.get('a')!.x + base.get('a')!.w, -1);
		expect(r.get('b')).toEqual(base.get('b')!);
		checkTiles(r);
	});

	test('left of a leaf', () => {
		const r = layout(insertAt(two, 'n', 'b', 'left'), desk);
		expect(r.get('n')!.x).toBeLessThan(r.get('b')!.x);
		expect(r.get('n')!.x).toBeGreaterThan(r.get('a')!.x);
		expect(r.get('a')).toEqual(base.get('a')!);
		checkTiles(r);
	});

	test('below a leaf', () => {
		const t = insertAt(two, 'n', 'a', 'bottom');
		const r = layout(t, desk);
		expect(r.get('n')!.x).toBe(r.get('a')!.x);
		expect(r.get('n')!.y).toBeGreaterThan(r.get('a')!.y);
		expect(Math.abs(r.get('n')!.h - r.get('a')!.h)).toBeLessThanOrEqual(1);
		expect(r.get('b')).toEqual(base.get('b')!);
		checkTiles(r);
	});

	test('above a leaf', () => {
		const r = layout(insertAt(two, 'n', 'b', 'top'), desk);
		expect(r.get('n')!.y).toBeLessThan(r.get('b')!.y);
		expect(r.get('n')!.x).toBe(r.get('b')!.x);
		checkTiles(r);
	});

	test('along a whole side of the desk', () => {
		const grid: Node = { dir: 'column', kids: [row('a', 'b'), row('c', 'd')], sizes: [1, 1] };
		const left = layout(insertRoot(grid, 'n', 'left'), desk).get('n')!;
		expect(left).toEqual({ x: G, y: G, w: left.w, h: desk.h - 2 * G });
		expect(Math.abs(left.w - (desk.w - 3 * G) / 2)).toBeLessThanOrEqual(1);
		const bottom = layout(insertRoot(grid, 'n', 'bottom'), desk).get('n')!;
		expect(bottom.w).toBe(desk.w - 2 * G);
		expect(bottom.y + bottom.h).toBe(desk.h - G);
		// A third equal column when the root already splits that way.
		const third = layout(insertRoot(two, 'n', 'right'), desk);
		expect(Math.abs(third.get('n')!.w - third.get('a')!.w)).toBeLessThanOrEqual(1);
		checkTiles(third);
	});

	test('auto placement splits the given window along its longer side, else the largest', () => {
		const t = insertAuto(two, 'n', 'b', desk);
		const r = layout(t, desk);
		// b is taller than wide, so it splits top and bottom.
		expect(r.get('n')!.x).toBe(r.get('b')!.x);
		expect(r.get('n')!.y).toBeGreaterThan(r.get('b')!.y);
		const big: Node = { dir: 'row', kids: [{ id: 'a' }, { id: 'b' }], sizes: [1, 3] };
		expect(leaves(insertAuto(big, 'n', null, desk))).toEqual(['a', 'b', 'n']);
		expect(insertAuto(null, 'n', null, desk)).toEqual({ id: 'n' });
	});
});

describe('remove, swap, prune', () => {
	test('remove collapses a single-kid split', () => {
		const t: Node = { dir: 'row', kids: [{ id: 'a' }, { dir: 'column', kids: [{ id: 'b' }, { dir: 'row', kids: [{ id: 'c' }, { id: 'd' }], sizes: [1, 1] }], sizes: [1, 1] }], sizes: [1, 1] };
		const r = remove(t, 'b')!;
		// The column held b and a row: the row takes its place.
		expect(r).toEqual({ dir: 'row', kids: [{ id: 'a' }, { dir: 'row', kids: [{ id: 'c' }, { id: 'd' }], sizes: [1, 1] }], sizes: [1, 1] });
		expect(remove(remove(remove(r, 'a'), 'c'), 'd')).toBeNull();
		expect(remove({ id: 'a' }, 'a')).toBeNull();
		checkTiles(layout(r, desk));
	});

	test('swap trades two leaves', () => {
		const t = row('a', 'b', 'c');
		const before = layout(t, desk);
		const after = layout(swap(t, 'a', 'c'), desk);
		expect(after.get('a')).toEqual(before.get('c')!);
		expect(after.get('c')).toEqual(before.get('a')!);
	});

	test('prune drops unknown ids', () => {
		const t: Node = { dir: 'column', kids: [row('a', 'gone'), row('c', 'old')], sizes: [1, 1] };
		const p = prune(t, (id) => id === 'a' || id === 'c');
		expect(p).toEqual({ dir: 'column', kids: [{ id: 'a' }, { id: 'c' }], sizes: [1, 1] });
		expect(prune(t, () => false)).toBeNull();
	});

	test('sanitize rejects junk and duplicate ids', () => {
		expect(sanitize(null)).toBeNull();
		expect(sanitize({ dir: 'diag', kids: [], sizes: [] })).toBeNull();
		expect(sanitize({ dir: 'row', kids: [{ id: 'a' }, { id: 'a' }, { id: 'b' }], sizes: [1, 1, -3] })).toEqual({ dir: 'row', kids: [{ id: 'a' }, { id: 'b' }], sizes: [1, 1] });
		expect(sanitize({ dir: 'row', kids: [{ id: 'a' }], sizes: [1] })).toEqual({ id: 'a' });
	});
});

describe('applyZone', () => {
	const t: Node = { dir: 'column', kids: [row('a', 'b'), row('c', 'd')], sizes: [1, 1] };

	test('moving a window to the right of another', () => {
		const n = applyZone(t, 'a', { kind: 'side', target: 'd', side: 'right' }, desk);
		const r = layout(n, desk);
		expect(r.get('a')!.x).toBeGreaterThan(r.get('d')!.x);
		expect(r.get('a')!.y).toBe(r.get('d')!.y);
		// b alone takes the top row.
		expect(r.get('b')!.w).toBe(desk.w - 2 * G);
		checkTiles(r);
	});

	test('centre swaps', () => {
		const before = layout(t, desk);
		const after = layout(applyZone(t, 'a', { kind: 'swap', target: 'd' }, desk), desk);
		expect(after.get('a')).toEqual(before.get('d')!);
	});

	test('a newcomer dropped in the centre takes the first half', () => {
		const r = layout(applyZone(t, 'n', { kind: 'swap', target: 'd' }, desk), desk);
		expect(r.get('n')!.x).toBeLessThan(r.get('d')!.x);
		checkTiles(r);
	});

	test('desk edge', () => {
		const r = layout(applyZone(t, 'd', { kind: 'edge', side: 'left' }, desk), desk);
		expect(r.get('d')!.x).toBe(G);
		expect(r.get('d')!.h).toBe(desk.h - 2 * G);
		checkTiles(r);
	});
});

describe('resizer', () => {
	const t: Node = { dir: 'row', kids: [{ id: 'a' }, { dir: 'column', kids: [{ id: 'b' }, { id: 'c' }], sizes: [1, 1] }], sizes: [1, 1] };

	test('moves the nearest split of that orientation with a neighbour', () => {
		const base = layout(t, desk);
		// c's west edge borders a through the root row.
		const r = layout(resizer(t, 'c', 'w', desk)!(-100), desk);
		expect(r.get('c')!.w).toBe(base.get('c')!.w + 100);
		expect(r.get('b')!.w).toBe(r.get('c')!.w);
		expect(r.get('a')!.w).toBe(base.get('a')!.w - 100);
		checkTiles(r);
		// b's south edge moves the column split.
		const s = layout(resizer(t, 'b', 's', desk)!(50), desk);
		expect(s.get('b')!.h).toBe(base.get('b')!.h + 50);
	});

	test('no neighbour, no resize', () => {
		expect(resizer(t, 'a', 'w', desk)).toBeNull();
		expect(resizer(t, 'a', 'n', desk)).toBeNull();
		expect(resizer(t, 'c', 's', desk)).toBeNull();
		expect(resizer(t, 'missing', 'e', desk)).toBeNull();
	});

	test('minimum sizes hold', () => {
		const grow = layout(resizer(t, 'a', 'e', desk)!(5000), desk);
		expect(grow.get('b')!.w).toBe(MIN_W);
		checkTiles(grow);
		const shrink = layout(resizer(t, 'a', 'e', desk)!(-5000), desk);
		expect(shrink.get('a')!.w).toBe(MIN_W);
		const tall = layout(resizer(t, 'b', 's', desk)!(5000), desk);
		expect(tall.get('c')!.h).toBe(MIN_H);
	});
});

describe('zoneAt', () => {
	const rects = layout(row('a', 'b'), desk);
	const a = rects.get('a')!;
	const at = (fx: number, fy: number) => ({ x: a.x + a.w * fx, y: a.y + a.h * fy });

	test('bands, centre and corners', () => {
		expect(zoneAt(at(0.9, 0.5), desk, rects, 'b')).toEqual({ kind: 'side', target: 'a', side: 'right' });
		expect(zoneAt(at(0.1, 0.5), desk, rects, 'b')).toEqual({ kind: 'side', target: 'a', side: 'left' });
		expect(zoneAt(at(0.5, 0.15), desk, rects, 'b')).toEqual({ kind: 'side', target: 'a', side: 'top' });
		expect(zoneAt(at(0.5, 0.85), desk, rects, 'b')).toEqual({ kind: 'side', target: 'a', side: 'bottom' });
		expect(zoneAt(at(0.5, 0.5), desk, rects, 'b')).toEqual({ kind: 'swap', target: 'a' });
		// Bottom right corner, nearer the bottom edge.
		expect(zoneAt(at(0.8, 0.95), desk, rects, 'b')).toEqual({ kind: 'side', target: 'a', side: 'bottom' });
	});

	test('desk edges win and the dragged window never targets itself', () => {
		expect(zoneAt({ x: 10, y: 400 }, desk, rects, 'b')).toEqual({ kind: 'edge', side: 'left' });
		expect(zoneAt({ x: 800 + 400, y: desk.h - 5 }, desk, rects, 'a')).toEqual({ kind: 'edge', side: 'bottom' });
		expect(zoneAt({ x: 1200, y: 400 }, desk, rects, 'b')).toBeNull();
		expect(zoneAt({ x: 300, y: 400 }, desk, new Map([['a', a]]), 'a')).toBeNull();
	});
});

/** Every tile is at least the minimum size. */
function checkMin(rects: Map<string, Rect>) {
	for (const [id, r] of rects) {
		expect([id, r.w >= MIN_W, r.h >= MIN_H]).toEqual([id, true, true]);
	}
}

const sizesOf = (n: Node) => (n as Split).sizes;

describe('layout with hidden leaves', () => {
	const t: Node = { dir: 'column', kids: [row('a', 'b'), row('c', 'd', 'e')], sizes: [1, 2] };

	test('lays out as if the hidden leaves were pruned', () => {
		for (const hide of [['b'], ['a', 'b'], ['d'], ['a', 'c', 'e']]) {
			const shown = (id: string) => !hide.includes(id);
			expect(layout(t, desk, DEFAULTS, shown)).toEqual(layout(prune(t, shown), desk));
		}
		expect(layout(t, desk, DEFAULTS, () => false).size).toBe(0);
		expect(layout({ id: 'a' }, desk, DEFAULTS, () => false).size).toBe(0);
	});

	test('a hidden window comes back to its own slot', () => {
		const before = layout(t, desk);
		const hidden = layout(t, desk, DEFAULTS, (id) => id !== 'd');
		expect(hidden.has('d')).toBe(false);
		expect(layout(t, desk, DEFAULTS, () => true)).toEqual(before);
	});

	test('a split across a hidden leaf trades with the next shown one and keeps the hidden share', () => {
		const three = row('a', 'b', 'c');
		const shown = (id: string) => id !== 'b';
		const base = layout(three, desk, DEFAULTS, shown);
		const moved = resizer(three, 'a', 'e', desk, DEFAULTS, shown)!(100);
		const r = layout(moved, desk, DEFAULTS, shown);
		expect(r.get('a')!.w).toBe(base.get('a')!.w + 100);
		expect(r.get('c')!.w).toBe(base.get('c')!.w - 100);
		const back = layout(moved, desk).get('b')!;
		expect(Math.abs(back.w - (desk.w - 4 * G) / 3)).toBeLessThanOrEqual(1);
	});
});

describe('alloc', () => {
	test('a desk smaller than the minimums shares by weight and stays inside', () => {
		const small = { w: 500, h: 300 };
		const r = layout(row('a', 'b', 'c'), small);
		for (const t of r.values()) {
			expect(t.x + t.w).toBeLessThanOrEqual(small.w - G);
			expect(t.w).toBeLessThan(MIN_W);
		}
		expect(Math.abs(r.get('a')!.w - r.get('c')!.w)).toBeLessThanOrEqual(1);
	});
});

describe('resizer, more', () => {
	const t: Node = { dir: 'row', kids: [{ id: 'a' }, { dir: 'column', kids: [{ id: 'b' }, { id: 'c' }], sizes: [1, 1] }], sizes: [1, 1] };

	test('the neighbour stops at its minimum and the weights stay positive', () => {
		const n = resizer(t, 'a', 'e', desk)!(5000);
		const [own, nb] = sizesOf(n);
		expect(nb).toBeGreaterThan(0);
		const room = desk.w - 3 * G;
		expect(nb / own).toBeCloseTo(MIN_W / (room - MIN_W), 6);
		const r = layout(n, desk);
		expect(r.get('b')!.w).toBe(MIN_W);
		expect(r.get('a')!.w).toBe(room - MIN_W);
	});

	test('a pair too small for both minimums does not move', () => {
		const small = { w: 400, h: 300 };
		const two = row('a', 'b');
		expect(resizer(two, 'a', 'e', small)!(0)).toEqual(two);
		expect(resizer(two, 'a', 'e', small)!(100)).toEqual(two);
	});

	test('the nearest split of that orientation moves, not an outer one', () => {
		const nested: Node = { dir: 'row', kids: [row('a', 'b'), { id: 'c' }], sizes: [2, 1] };
		const base = layout(nested, desk);
		const r = layout(resizer(nested, 'a', 'e', desk)!(100), desk);
		expect(r.get('a')!.w).toBe(base.get('a')!.w + 100);
		expect(r.get('b')!.w).toBe(base.get('b')!.w - 100);
		expect(r.get('c')).toEqual(base.get('c')!);
	});

	test('a sibling held at its minimum does not skew the trade', () => {
		const t3: Node = { dir: 'row', kids: [{ id: 'a' }, { id: 'b' }, { id: 'c' }], sizes: [10, 1, 10] };
		const base = layout(t3, desk);
		expect(base.get('a')!.w).toBe(644);
		expect(base.get('b')!.w).toBe(MIN_W);
		expect(layout(resizer(t3, 'a', 'e', desk)!(0), desk)).toEqual(base);
		// b is at its minimum: a cannot grow, but it shrinks pixel for pixel.
		expect(layout(resizer(t3, 'a', 'e', desk)!(50), desk)).toEqual(base);
		const r = layout(resizer(t3, 'a', 'e', desk)!(-50), desk);
		expect(r.get('a')!.w).toBe(594);
		expect(r.get('b')!.w).toBe(MIN_W + 50);
		expect(r.get('c')).toEqual(base.get('c')!);
	});

	test('a corner moves both splits of the current tree, also after the tree changed mid-drag', () => {
		const base = layout(t, desk);
		const ew = edgeAt(t, 'b', 'w', desk)!;
		const es = edgeAt(t, 'b', 's', desk)!;
		expect(ew).toBe(base.get('b')!.x);
		expect(es).toBe(base.get('b')!.y + base.get('b')!.h);
		// One pointer move: both axes applied in turn, each to the tree as it is now.
		let cur = moveEdge(t, 'b', 'w', ew - 100, desk)!;
		cur = moveEdge(cur, 'b', 's', es + 50, desk)!;
		let r = layout(cur, desk);
		expect(r.get('b')!.x).toBe(base.get('b')!.x - 100);
		expect(r.get('b')!.h).toBe(base.get('b')!.h + 50);
		// A poll adds a window mid-drag: the next move keeps it.
		cur = insertAt(cur, 'n', 'a', 'bottom');
		cur = moveEdge(cur, 'b', 'w', ew - 150, desk)!;
		cur = moveEdge(cur, 'b', 's', es + 60, desk)!;
		r = layout(cur, desk);
		expect(leaves(cur).sort()).toEqual(['a', 'b', 'c', 'n']);
		expect(r.get('b')!.x).toBe(base.get('b')!.x - 150);
		expect(r.get('b')!.h).toBe(base.get('b')!.h + 60);
		checkTiles(r);
		// The window that went away mid-drag: nothing to move, no tree to write.
		expect(moveEdge(cur, 'gone', 'w', 100, desk)).toBeNull();
	});
});

describe('insertAuto, minimum sizes', () => {
	test('12 windows opened one after another all keep the minimum size', () => {
		for (const nearLast of [true, false]) {
			let t: Node | null = null;
			for (let i = 0; i < 12; i++) {
				t = insertAuto(t, 'w' + i, nearLast && i ? 'w' + (i - 1) : null, desk);
				const r = layout(t, desk);
				expect(r.size).toBe(i + 1);
				checkMin(r);
				checkTiles(r);
			}
		}
	});

	test('near splits the other way when its longer side cannot halve', () => {
		const r = layout(insertAuto({ id: 'a' }, 'n', 'a', { w: 516, h: 416 }), { w: 516, h: 416 });
		expect(r.get('n')!.x).toBe(r.get('a')!.x);
		expect(r.get('n')!.y).toBeGreaterThan(r.get('a')!.y);
	});

	test('near too small either way: the largest tile that splits takes it', () => {
		const low = { w: 1600, h: 300 };
		const t: Node = { dir: 'row', kids: [{ id: 'a' }, { id: 'b' }, { id: 'c' }], sizes: [1, 10, 4] };
		const n = insertAuto(t, 'n', 'a', low);
		const r = layout(n, low);
		expect(r.get('n')!.x).toBeGreaterThan(r.get('b')!.x);
		expect(r.get('n')!.x).toBeLessThan(r.get('c')!.x);
		checkMin(r);
	});

	test('no tile splits: the largest takes it', () => {
		const tiny = { w: 400, h: 300 };
		const t: Node = { dir: 'row', kids: [{ id: 'a' }, { id: 'b' }], sizes: [1, 3] };
		expect(leaves(insertAuto(t, 'n', 'a', tiny))).toEqual(['a', 'b', 'n']);
	});
});

describe('insertRoot', () => {
	test('a new strip takes the mean weight of the others', () => {
		const t: Node = { dir: 'row', kids: [{ id: 'a' }, { id: 'b' }], sizes: [1, 3] };
		expect(sizesOf(insertRoot(t, 'n', 'right'))).toEqual([1, 3, 2]);
		expect(sizesOf(insertRoot(t, 'n', 'left'))).toEqual([2, 1, 3]);
	});
});

describe('applyZone, more', () => {
	test('a missing target puts the window along the right of the desk', () => {
		const t = row('a', 'b');
		expect(leaves(applyZone(t, 'n', { kind: 'side', target: 'x', side: 'left' }, desk))).toEqual(['a', 'b', 'n']);
		expect(leaves(applyZone(t, 'n', { kind: 'swap', target: 'x' }, desk))).toEqual(['a', 'b', 'n']);
		expect(leaves(applyZone(t, 'a', { kind: 'side', target: 'a', side: 'left' }, desk))).toEqual(['b', 'a']);
	});
});

describe('zoneAt, more', () => {
	const rects = layout(row('a', 'b', 'c'), desk);
	const b = rects.get('b')!;
	const c = rects.get('c')!;

	test('outside the desk there is no zone', () => {
		for (const p of [
			{ x: -5, y: 400 },
			{ x: 800, y: -3 },
			{ x: desk.w + 10, y: 400 },
			{ x: 800, y: desk.h + 1 }
		]) {
			expect(zoneAt(p, desk, rects, 'a')).toBeNull();
			expect(zoneAt(p, desk, rects, null)).toBeNull();
		}
	});

	test('in a gap the nearest tile counts', () => {
		expect(zoneAt({ x: c.x - 2, y: 450 }, desk, rects, null)).toEqual({ kind: 'side', target: 'c', side: 'left' });
		expect(zoneAt({ x: b.x + b.w + 2, y: 450 }, desk, rects, null)).toEqual({ kind: 'side', target: 'b', side: 'right' });
		expect(zoneAt({ x: b.x - 2, y: 450 }, desk, rects, 'a')).toEqual({ kind: 'side', target: 'b', side: 'left' });
	});
});

describe('fromRects, more', () => {
	test('tiles touching within a pixel still split', () => {
		const rects = new Map<string, Rect>([
			['a', { x: 8, y: 8, w: 1000.5, h: 884 }],
			['b', { x: 1008, y: 8, w: 584, h: 884 }]
		]);
		const n = fromRects(rects)! as Split;
		expect(n.dir).toBe('row');
		expect(leaves(n)).toEqual(['a', 'b']);
		expect(n.sizes).toEqual([1000.5, 584]);
	});

	test('a grid becomes rows of columns', () => {
		const grid: Node = { dir: 'column', kids: [row('a', 'b'), row('c', 'd')], sizes: [1, 1] };
		const n = fromRects(layout(grid, desk)) as Split;
		expect(n.dir).toBe('column');
		expect(n.kids.map((k) => [(k as Split).dir, leaves(k)])).toEqual([
			['row', ['a', 'b']],
			['row', ['c', 'd']]
		]);
	});
});

describe('rename', () => {
	test('renames a leaf', () => {
		expect(rename(row('a', 'b'), 'a', 'z')).toEqual(row('z', 'b'));
		expect(rename(row('a', 'b'), 'x', 'z')).toEqual(row('a', 'b'));
	});

	test('a leaf already named to gives way, so ids stay unique', () => {
		const n = rename(row('a', 'b', 'c'), 'a', 'c');
		expect(leaves(n)).toEqual(['c', 'b']);
		expect(rename(row('a', 'b'), 'a', 'a')).toEqual(row('a', 'b'));
	});
});

describe('sanitize and normalize', () => {
	test('repairs weights', () => {
		expect(sanitize({ dir: 'row', kids: [{ id: 'a' }, { id: 'b' }, { id: 'c' }], sizes: [0, 'x', null] })).toEqual(row('a', 'b', 'c'));
		const n = remove({ dir: 'row', kids: [{ id: 'a' }, { id: 'b' }, { id: 'c' }], sizes: [1, NaN, -2] }, 'a');
		expect(n).toEqual(row('b', 'c'));
	});

	test('huge weights stay finite', () => {
		const n = sanitize({ dir: 'row', kids: [{ id: 'a' }, { id: 'b' }], sizes: [1e308, 1e308] })!;
		const r = layout(n, desk);
		expect(r.get('a')!.w).toBe(r.get('b')!.w);
		for (const t of r.values()) for (const v of [t.x, t.y, t.w, t.h]) expect(Number.isFinite(v)).toBe(true);
	});

	test('kids and sizes must match', () => {
		expect(sanitize({ dir: 'row', kids: [{ id: 'a' }, { id: 'b' }], sizes: [1] })).toBeNull();
	});

	test('rejects trees too deep or too big, without throwing', () => {
		const chain = (depth: number) => {
			let n: unknown = { id: 'leaf' };
			for (let k = 0; k < depth; k++) n = { dir: k % 2 ? 'row' : 'column', kids: [{ id: 'x' + k }, n], sizes: [1, 1] };
			return n;
		};
		expect(sanitize(chain(MAX_DEPTH))).not.toBeNull();
		expect(sanitize(chain(MAX_DEPTH + 1))).toBeNull();
		expect(() => sanitize(chain(9000))).not.toThrow();
		expect(sanitize(chain(9000))).toBeNull();
		const wide = (n: number) => ({ dir: 'row', kids: Array.from({ length: n }, (_, i) => ({ id: 'w' + i })), sizes: Array.from({ length: n }, () => 1) });
		expect(sanitize(wide(MAX_LEAVES))).not.toBeNull();
		expect(sanitize(wide(MAX_LEAVES + 1))).toBeNull();
		const half = Math.ceil((MAX_LEAVES + 1) / 2);
		const rows = (n: number) => ({ dir: 'column', kids: [0, 1].map((k) => ({ dir: 'row', kids: Array.from({ length: n }, (_, i) => ({ id: k + '.' + i })), sizes: Array.from({ length: n }, () => 1) })), sizes: [1, 1] });
		expect(sanitize(rows(MAX_LEAVES / 2))).not.toBeNull();
		expect(sanitize(rows(half))).toBeNull();
	});
});
