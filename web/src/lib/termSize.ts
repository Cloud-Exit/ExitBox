/** A terminal size in cells. */
export type Size = { cols: number; rows: number };

/**
 * One window's part in sizing a session that several browsers show. The
 * owner fits the terminal to its window and sends that size; every other
 * window draws at the session's size, so output made for it lands right.
 */
export type SizeState = {
	owner: boolean;
	/** The size this page last sent ("colsxrows"); '' makes the next fit send. */
	sent: string;
	/** Waiting for the server to echo `sent`: other sizes before it are older. */
	awaiting: boolean;
	/** The session's size as the server last said; null until it says. */
	pty: Size | null;
};

export const sizeKey = (s: Size) => `${s.cols}x${s.rows}`;

/** The range the server accepts (Session.Resize). */
export function validSize(s: Size) {
	return Number.isInteger(s.cols) && Number.isInteger(s.rows) && s.cols >= 2 && s.rows >= 2 && s.cols <= 1000 && s.rows <= 1000;
}

/** Not an owner until it claims: a page opened in the background must not take the size. */
export function initialSize(): SizeState {
	return { owner: false, sent: '', awaiting: false, pty: null };
}

/** Makes this page the owner; force sends its size even if it sent the same before. */
export function claimSize(s: SizeState, force = false): SizeState {
	if (s.owner && !force) return s;
	return { ...s, owner: true, sent: '' };
}

/** Whether a fit may resize xterm: not while it draws at another screen's size. */
export function mayFit(s: SizeState) {
	return s.owner || !s.pty;
}

/** After a fit on an open connection: whether to send the size. */
export function fitSize(s: SizeState, size: Size): { state: SizeState; send: boolean } {
	if (!s.owner || !validSize(size)) return { state: s, send: false };
	const k = sizeKey(size);
	if (k === s.sent) return { state: s, send: false };
	return { state: { ...s, sent: k, awaiting: true }, send: true };
}

/** A size message from the server; resize is the size xterm must take, if any. */
export function heardSize(s: SizeState, size: Size): { state: SizeState; resize: Size | null } {
	if (!validSize(size)) return { state: s, resize: null };
	const k = sizeKey(size);
	if (s.awaiting) {
		// Sizes the server set before ours: ours is still to come.
		if (k !== s.sent) return { state: s, resize: null };
		return { state: { ...s, owner: true, awaiting: false, pty: size }, resize: null };
	}
	if (s.owner && k === s.sent) return { state: { ...s, pty: size }, resize: null };
	return { state: { ...s, owner: false, pty: size }, resize: size };
}

/** A new connection: the old one's echo never comes. */
export function reconnectSize(s: SizeState): SizeState {
	return s.awaiting ? { ...s, awaiting: false } : s;
}

/**
 * On `live`, for a page that just connected while visible: take the size
 * unless the attach message said others view the session. No attach message
 * (an older server, or a size unknown after a restart) takes it.
 */
export function claimsOnConnect(visible: boolean, attach: { others: boolean } | null) {
	return visible && !attach?.others;
}

/** On `free`: only a window that does not hold the size takes it. */
export function claimsOnFree(s: SizeState) {
	return !s.owner;
}

/** Going to the background: still the owner until another takes it, but sends its size again when back. */
export function releaseSize(s: SizeState): SizeState {
	return s.owner ? { ...s, sent: '', awaiting: false } : s;
}

/** The scale that shows a whole screen in a smaller window; never above 1. */
export function fitScale(box: { w: number; h: number }, screen: { w: number; h: number }) {
	if (box.w <= 0 || box.h <= 0 || screen.w <= 0 || screen.h <= 0) return 1;
	return Math.min(1, box.w / screen.w, box.h / screen.h);
}
