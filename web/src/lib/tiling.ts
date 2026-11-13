// A per-tab split tree, as in i3 or Hyprland: pure layout logic, no Svelte.

export const MIN_W = 280;
export const MIN_H = 160;
export const GAP = 8;

export interface Rect {
	x: number;
	y: number;
	w: number;
	h: number;
}

export interface Size {
	w: number;
	h: number;
}

export type Dir = 'row' | 'column';
export type Side = 'left' | 'right' | 'top' | 'bottom';
export type Edge = 'n' | 's' | 'e' | 'w';

export interface Leaf {
	id: string;
}

/** row lays its kids out left to right, column top to bottom; sizes are relative weights. */
export interface Split {
	dir: Dir;
	kids: Node[];
	sizes: number[];
}

export type Node = Leaf | Split;

/** Where a dragged window lands: on a side of a tile, in its place, or along a whole side of the desk. */
export type Zone = { kind: 'side'; target: string; side: Side } | { kind: 'swap'; target: string } | { kind: 'edge'; side: Side };

export interface Opts {
	gap: number;
	minW: number;
	minH: number;
}

export const DEFAULTS: Opts = { gap: GAP, minW: MIN_W, minH: MIN_H };

/** Which leaves a layout shows; a leaf it rejects keeps its place in the tree but takes no room. */
export type Shown = (id: string) => boolean;
const all: Shown = () => true;

/** Bounds on a stored tree: deeper or bigger ones are rejected as junk. */
export const MAX_DEPTH = 32;
export const MAX_LEAVES = 256;
const MIN_WEIGHT = 1e-4;
const MAX_WEIGHT = 1e6;

export const isLeaf = (n: Node): n is Leaf => 'id' in n;

/** A usable split weight: junk becomes 1, the rest stays in a range whose sums stay finite. */
function weight(v: unknown): number {
	const s = Number(v);
	return Number.isFinite(s) && s > 0 ? Math.min(Math.max(s, MIN_WEIGHT), MAX_WEIGHT) : 1;
}

const sideDir = (s: Side): Dir => (s === 'left' || s === 'right' ? 'row' : 'column');
const before = (s: Side) => s === 'left' || s === 'top';
const sum = (a: number[]) => a.reduce((x, y) => x + y, 0);

export function clone(n: Node): Node {
	return isLeaf(n) ? { id: n.id } : { dir: n.dir, kids: n.kids.map(clone), sizes: [...n.sizes] };
}

export function leaves(n: Node | null): string[] {
	if (!n) return [];
	return isLeaf(n) ? [n.id] : n.kids.flatMap(leaves);
}

export function has(n: Node | null, id: string) {
	return leaves(n).includes(id);
}

/** Collapses splits left with one kid; same-direction nesting stays, so a split never moves its neighbours. */
function normalize(n: Node): Node {
	if (isLeaf(n)) return n;
	const kids = n.kids.map(normalize);
	if (kids.length === 1) return kids[0];
	return { dir: n.dir, kids, sizes: n.sizes.map(weight) };
}

/** Checks a tree loaded from storage; null when it is not a tree or is too deep or big. Duplicate ids keep their first leaf. */
export function sanitize(raw: unknown): Node | null {
	const seen = new Set<string>();
	let bad = false;
	const walk = (v: unknown, depth: number): Node | null => {
		if (bad || !v || typeof v !== 'object') return null;
		if (depth > MAX_DEPTH) {
			bad = true;
			return null;
		}
		const o = v as Record<string, unknown>;
		if (typeof o.id === 'string') {
			if (!o.id || seen.has(o.id)) return null;
			seen.add(o.id);
			if (seen.size > MAX_LEAVES) bad = true;
			return { id: o.id };
		}
		if ((o.dir !== 'row' && o.dir !== 'column') || !Array.isArray(o.kids) || !Array.isArray(o.sizes) || o.kids.length !== o.sizes.length) return null;
		if (o.kids.length > MAX_LEAVES) {
			bad = true;
			return null;
		}
		const kids: Node[] = [];
		const sizes: number[] = [];
		for (let i = 0; i < o.kids.length; i++) {
			const n = walk(o.kids[i], depth + 1);
			if (n) {
				kids.push(n);
				// normalize repairs the weight.
				sizes.push(Number(o.sizes[i]));
			}
		}
		if (!kids.length) return null;
		return { dir: o.dir, kids, sizes };
	};
	const n = walk(raw, 0);
	return n && !bad ? normalize(n) : null;
}

/** Drops a leaf; a split left with one kid gives way to it. */
export function remove(n: Node | null, id: string): Node | null {
	if (!n) return null;
	const walk = (m: Node): Node | null => {
		if (isLeaf(m)) return m.id === id ? null : m;
		const kids: Node[] = [];
		const sizes: number[] = [];
		m.kids.forEach((k, i) => {
			const r = walk(k);
			if (r) {
				kids.push(r);
				sizes.push(m.sizes[i]);
			}
		});
		if (!kids.length) return null;
		return { dir: m.dir, kids, sizes };
	};
	const r = walk(n);
	return r && normalize(r);
}

/** Keeps only the leaves keep accepts. */
export function prune(n: Node | null, keep: (id: string) => boolean): Node | null {
	let t = n;
	for (const id of leaves(n)) if (!keep(id)) t = remove(t, id);
	return t;
}

/** Gives leaf from the id to; a leaf already called to gives way, so ids stay unique. */
export function rename(n: Node, from: string, to: string): Node {
	if (from === to || !has(n, from)) return n;
	const walk = (m: Node): Node => (isLeaf(m) ? (m.id === from ? { id: to } : m) : { dir: m.dir, kids: m.kids.map(walk), sizes: [...m.sizes] });
	return walk(has(n, to) ? remove(n, to)! : n);
}

export function swap(n: Node, a: string, b: string): Node {
	if (isLeaf(n)) return n.id === a ? { id: b } : n.id === b ? { id: a } : n;
	return { dir: n.dir, kids: n.kids.map((k) => swap(k, a, b)), sizes: [...n.sizes] };
}

/** Puts id on a side of the target leaf; the two share the target's space half and half. */
export function insertAt(n: Node | null, id: string, target: string, side: Side): Node {
	if (!n) return { id };
	const dir = sideDir(side);
	const walk = (m: Node): Node => {
		if (isLeaf(m)) return m.id !== target ? m : { dir, kids: before(side) ? [{ id }, m] : [m, { id }], sizes: [1, 1] };
		return { dir: m.dir, kids: m.kids.map(walk), sizes: [...m.sizes] };
	};
	return walk(n);
}

/** Puts id along a whole side of the desk; it takes the share one more equal strip would. */
export function insertRoot(n: Node | null, id: string, side: Side): Node {
	if (!n) return { id };
	const dir = sideDir(side);
	if (!isLeaf(n) && n.dir === dir) {
		const share = sum(n.sizes) / n.kids.length;
		return before(side) ? { dir, kids: [{ id }, ...n.kids], sizes: [share, ...n.sizes] } : { dir, kids: [...n.kids, { id }], sizes: [...n.sizes, share] };
	}
	return { dir, kids: before(side) ? [{ id }, n] : [n, { id }], sizes: [1, 1] };
}

/** Whether a subtree has a shown leaf. */
function visible(n: Node, shown: Shown): boolean {
	return isLeaf(n) ? shown(n.id) : n.kids.some((k) => visible(k, shown));
}

/** The smallest the shown part of a subtree can be along one axis. */
function minSize(n: Node, axis: 'w' | 'h', o: Opts, shown: Shown): number {
	if (isLeaf(n)) return axis === 'w' ? o.minW : o.minH;
	const ms = n.kids.filter((k) => visible(k, shown)).map((k) => minSize(k, axis, o, shown));
	if (!ms.length) return 0;
	const along = (n.dir === 'row') === (axis === 'w');
	return along ? sum(ms) + o.gap * (ms.length - 1) : Math.max(...ms);
}

/** Shares room by weight, giving each part at least its minimum while the room allows. */
function alloc(room: number, weights: number[], mins: number[]): number[] {
	const total = sum(weights);
	const prop = weights.map((w) => (room * w) / total);
	if (sum(mins) > room) return prop;
	const fixed = weights.map(() => false);
	for (;;) {
		const free = room - sum(mins.filter((_, i) => fixed[i]));
		const wsum = sum(weights.filter((_, i) => !fixed[i]));
		const sizes = weights.map((w, i) => (fixed[i] ? mins[i] : (free * w) / wsum));
		const low = sizes.findIndex((s, i) => !fixed[i] && s < mins[i] - 1e-9);
		if (low < 0) return sizes;
		fixed[low] = true;
	}
}

/** Visits every shown node with its area, shown kids split along the node's direction with gaps between. */
function walkAreas(n: Node, area: Rect, o: Opts, shown: Shown, visit: (n: Node, r: Rect) => void) {
	if (!visible(n, shown)) return;
	visit(n, area);
	if (isLeaf(n)) return;
	const kids = n.kids.map((k, i) => ({ k, w: n.sizes[i] })).filter(({ k }) => visible(k, shown));
	const row = n.dir === 'row';
	const len = row ? area.w : area.h;
	const room = len - o.gap * (kids.length - 1);
	const parts = alloc(
		room,
		kids.map(({ w }) => w),
		kids.map(({ k }) => minSize(k, row ? 'w' : 'h', o, shown))
	);
	let at = row ? area.x : area.y;
	kids.forEach(({ k }, i) => {
		const r = row ? { x: at, y: area.y, w: parts[i], h: area.h } : { x: area.x, y: at, w: area.w, h: parts[i] };
		walkAreas(k, r, o, shown, visit);
		at += parts[i] + o.gap;
	});
}

const rootArea = (desk: Size, o: Opts): Rect => ({ x: o.gap, y: o.gap, w: desk.w - 2 * o.gap, h: desk.h - 2 * o.gap });

/** The unrounded area of every shown node. */
function nodeAreas(n: Node, desk: Size, o: Opts, shown: Shown): Map<Node, Rect> {
	const out = new Map<Node, Rect>();
	walkAreas(n, rootArea(desk, o), o, shown, (m, r) => out.set(m, r));
	return out;
}

/** The unrounded area of every shown leaf, by id. */
function leafAreas(n: Node, desk: Size, o: Opts, shown: Shown): Map<string, Rect> {
	const out = new Map<string, Rect>();
	walkAreas(n, rootArea(desk, o), o, shown, (m, r) => {
		if (isLeaf(m)) out.set(m.id, r);
	});
	return out;
}

/**
 * The rectangle of every shown leaf, edges rounded to whole pixels so
 * neighbours never overlap. Hidden leaves give their room to the rest, as if
 * pruned, and get it back when shown again.
 */
export function layout(n: Node | null, desk: Size, o: Opts = DEFAULTS, shown: Shown = all): Map<string, Rect> {
	const out = new Map<string, Rect>();
	if (!n) return out;
	for (const [id, r] of leafAreas(n, desk, o, shown)) {
		const x = Math.round(r.x);
		const y = Math.round(r.y);
		out.set(id, { x, y, w: Math.max(1, Math.round(r.x + r.w) - x), h: Math.max(1, Math.round(r.y + r.h) - y) });
	}
	return out;
}

/** Builds a tree whose layout reproduces tiles already on screen, by cutting along the gaps. */
export function fromRects(rects: Map<string, Rect>): Node | null {
	const build = (ids: string[]): Node | null => {
		if (!ids.length) return null;
		if (ids.length === 1) return { id: ids[0] };
		// Rows first, so a grid becomes rows of columns as the grid preset sizes them.
		for (const dir of ['column', 'row'] as const) {
			const lo = (id: string) => (dir === 'row' ? rects.get(id)!.x : rects.get(id)!.y);
			const hi = (id: string) => lo(id) + (dir === 'row' ? rects.get(id)!.w : rects.get(id)!.h);
			const sorted = [...ids].sort((a, b) => lo(a) - lo(b));
			const groups: string[][] = [];
			let end = -Infinity;
			for (const id of sorted) {
				if (!groups.length || lo(id) >= end - 1) groups.push([id]);
				else groups[groups.length - 1].push(id);
				end = Math.max(end, hi(id));
			}
			if (groups.length < 2) continue;
			const kids = groups.map(build) as Node[];
			const sizes = groups.map((g) => Math.max(1, Math.max(...g.map(hi)) - Math.min(...g.map(lo))));
			return { dir, kids, sizes };
		}
		// Tiles that no straight cut separates: side by side, in reading order.
		const sorted = [...ids].sort((a, b) => rects.get(a)!.y - rects.get(b)!.y || rects.get(a)!.x - rects.get(b)!.x);
		return { dir: 'row', kids: sorted.map((id) => ({ id })), sizes: sorted.map(() => 1) };
	};
	const n = build([...rects.keys()]);
	return n && normalize(n);
}

/**
 * Where a window without a drop position goes: near split along its longer
 * side, else its shorter one, when both halves keep the minimum size; else the
 * largest tile that splits that way; else the largest tile.
 */
export function autoSpot(n: Node, near: string | null, desk: Size, o: Opts = DEFAULTS, shown: Shown = all): { target: string; side: Side } | null {
	const areas = leafAreas(n, desk, o, shown);
	if (!areas.size) return null;
	const sides = (r: Rect): Side[] =>
		(r.w >= r.h ? (['right', 'bottom'] as const) : (['bottom', 'right'] as const)).filter((s) => (s === 'right' ? (r.w - o.gap) / 2 >= o.minW : (r.h - o.gap) / 2 >= o.minH));
	const nr = near ? areas.get(near) : undefined;
	if (near && nr && sides(nr).length) return { target: near, side: sides(nr)[0] };
	const bySize = [...areas].sort((a, b) => b[1].w * b[1].h - a[1].w * a[1].h);
	for (const [id, r] of bySize) {
		const s = sides(r);
		if (s.length) return { target: id, side: s[0] };
	}
	const [id, r] = bySize[0];
	return { target: id, side: r.w >= r.h ? 'right' : 'bottom' };
}

/** Adds a window without a drop position, at autoSpot; with nothing shown it goes along the right of the desk. */
export function insertAuto(n: Node | null, id: string, near: string | null, desk: Size, o: Opts = DEFAULTS, shown: Shown = all): Node {
	if (!n) return { id };
	const spot = autoSpot(n, near, desk, o, shown);
	return spot ? insertAt(n, id, spot.target, spot.side) : insertRoot(n, id, 'right');
}

/** The tree after a window is dropped in a zone; the window may or may not be in the tree yet. */
export function applyZone(n: Node | null, id: string, zone: Zone, desk: Size, o: Opts = DEFAULTS, shown: Shown = all): Node {
	if (zone.kind === 'swap' && n && has(n, id) && has(n, zone.target)) return swap(n, id, zone.target);
	const t = remove(n, id);
	if (zone.kind === 'edge') return insertRoot(t, id, zone.side);
	if (!has(t, zone.target)) return insertRoot(t, id, 'right');
	if (zone.kind === 'side') return insertAt(t, id, zone.target, zone.side);
	// A window from elsewhere cannot trade places: it takes the first half of the target.
	const r = layout(t, desk, o, shown).get(zone.target);
	return insertAt(t, id, zone.target, r && r.w < r.h ? 'top' : 'left');
}

/** Index path from the root to a leaf, or null. */
function pathTo(n: Node, id: string): number[] | null {
	if (isLeaf(n)) return n.id === id ? [] : null;
	for (let i = 0; i < n.kids.length; i++) {
		const p = pathTo(n.kids[i], id);
		if (p) return [i, ...p];
	}
	return null;
}

const nodeAt = (n: Node, path: number[]) => path.reduce((m, i) => (m as Split).kids[i], n);

interface EdgeSplit {
	at: number[];
	i: number;
	j: number;
	dir: Dir;
	step: 1 | -1;
}

/** The split an edge of a shown leaf moves: the nearest ancestor of that orientation with a shown neighbour on that side. */
function edgeSplit(n: Node, id: string, edge: Edge, shown: Shown): EdgeSplit | null {
	const path = pathTo(n, id);
	if (!path || !shown(id)) return null;
	const dir: Dir = edge === 'e' || edge === 'w' ? 'row' : 'column';
	const step = edge === 'e' || edge === 's' ? 1 : -1;
	for (let k = path.length - 1; k >= 0; k--) {
		const at = path.slice(0, k);
		const split = nodeAt(n, at) as Split;
		if (split.dir !== dir) continue;
		let j = path[k] + step;
		while (j >= 0 && j < split.kids.length && !visible(split.kids[j], shown)) j += step;
		if (j >= 0 && j < split.kids.length) return { at, i: path[k], j, dir, step };
	}
	return null;
}

/** Where an edge of a window is now, in desk pixels along its axis; null when nothing borders it. */
export function edgeAt(n: Node, id: string, edge: Edge, desk: Size, o: Opts = DEFAULTS, shown: Shown = all): number | null {
	const hit = edgeSplit(n, id, edge, shown);
	if (!hit) return null;
	const r = nodeAreas(n, desk, o, shown).get(nodeAt(n, [...hit.at, hit.i]));
	if (!r) return null;
	return edge === 'e' ? r.x + r.w : edge === 'w' ? r.x : edge === 's' ? r.y + r.h : r.y;
}

/**
 * Moves the split on one edge of a window to desk coordinate to: the subtree
 * holding the window and its neighbour across that split trade space, both
 * kept at their minimum. The tree is resolved afresh on every call, so a tree
 * that changed since the drag began is moved, not overwritten. Null when
 * nothing borders that edge.
 */
export function moveEdge(n: Node, id: string, edge: Edge, to: number, desk: Size, o: Opts = DEFAULTS, shown: Shown = all): Node | null {
	const hit = edgeSplit(n, id, edge, shown);
	if (!hit) return null;
	const { at, i, j, dir, step } = hit;
	const areas = nodeAreas(n, desk, o, shown);
	const split = nodeAt(n, at) as Split;
	const row = dir === 'row';
	const start = (m: Node) => (row ? areas.get(m)!.x : areas.get(m)!.y);
	const len = (m: Node) => (row ? areas.get(m)!.w : areas.get(m)!.h);
	const own = split.kids[i];
	const pair = len(own) + len(split.kids[j]);
	const axis = row ? 'w' : 'h';
	const minOwn = minSize(own, axis, o, shown);
	const minNb = minSize(split.kids[j], axis, o, shown);
	const t = clone(n);
	if (pair < minOwn + minNb) return t;
	const want = step > 0 ? to - start(own) : start(own) + len(own) - to;
	const next = Math.min(Math.max(want, minOwn), pair - minNb);
	// Weights become the laid-out lengths first, so a kid held at its minimum cannot skew the trade; hidden kids keep their share.
	const vis = split.kids.map((k) => visible(k, shown));
	const scale = sum(split.sizes.filter((_, k) => vis[k])) / sum(split.kids.filter((_, k) => vis[k]).map(len));
	const s = nodeAt(t, at) as Split;
	split.kids.forEach((k, m) => {
		if (vis[m]) s.sizes[m] = len(k) * scale;
	});
	s.sizes[i] = next * scale;
	s.sizes[j] = (pair - next) * scale;
	return t;
}

/** moveEdge by pointer travel d from where the edge is now; null when nothing borders that edge. */
export function resizer(n: Node, id: string, edge: Edge, desk: Size, o: Opts = DEFAULTS, shown: Shown = all): ((d: number) => Node) | null {
	const from = edgeAt(n, id, edge, desk, o, shown);
	if (from === null) return null;
	return (d: number) => moveEdge(n, id, edge, from + d, desk, o, shown) ?? clone(n);
}

/**
 * The zone under a point during a drag: within edge px of the desk border, that
 * whole side; on a tile, its outer band fraction on each side places beside it,
 * the centre swaps. Corners go to the nearest edge. self is the dragged window,
 * which never targets itself.
 */
export function zoneAt(p: { x: number; y: number }, desk: Size, rects: Map<string, Rect>, self: string | null, edge = 24, band = 0.3): Zone | null {
	const others = [...rects].filter(([id]) => id !== self);
	if (!others.length) return null;
	if (p.x < 0 || p.y < 0 || p.x > desk.w || p.y > desk.h) return null;
	const ds: [Side, number][] = [
		['left', p.x],
		['right', desk.w - p.x],
		['top', p.y],
		['bottom', desk.h - p.y]
	];
	const [side, d] = ds.reduce((a, b) => (b[1] < a[1] ? b : a));
	if (d <= edge) return { kind: 'edge', side };
	const own = self ? rects.get(self) : undefined;
	const inside = (r: Rect) => p.x >= r.x && p.x <= r.x + r.w && p.y >= r.y && p.y <= r.y + r.h;
	if (own && inside(own)) return null;
	// In a gap the nearest tile counts.
	const dist = (r: Rect) => Math.hypot(Math.max(r.x - p.x, 0, p.x - r.x - r.w), Math.max(r.y - p.y, 0, p.y - r.y - r.h));
	const [target, r] = others.reduce((a, b) => (dist(b[1]) < dist(a[1]) ? b : a));
	const fx = Math.min(Math.max((p.x - r.x) / r.w, 0), 1);
	const fy = Math.min(Math.max((p.y - r.y) / r.h, 0), 1);
	const bands: [Side, number][] = (
		[
			['left', fx],
			['right', 1 - fx],
			['top', fy],
			['bottom', 1 - fy]
		] as [Side, number][]
	).filter(([, f]) => f < band);
	if (!bands.length) return { kind: 'swap', target };
	return { kind: 'side', target, side: bands.reduce((a, b) => (b[1] < a[1] ? b : a))[0] };
}
