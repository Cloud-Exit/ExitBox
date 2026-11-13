/** A held finger opens the element's context menu, as a right click does: iOS never sends contextmenu. */
export function longPress(node: HTMLElement) {
	let timer: ReturnType<typeof setTimeout> | null = null;
	let at = { x: 0, y: 0 };
	/** A menu opened on this press: the click that ends it must not also open the row. */
	let opened = false;
	const cancel = () => {
		if (timer) clearTimeout(timer);
		timer = null;
	};
	const down = (e: PointerEvent) => {
		if (e.pointerType !== 'touch') return;
		opened = false;
		at = { x: e.clientX, y: e.clientY };
		cancel();
		timer = setTimeout(() => {
			timer = null;
			opened = true;
			node.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: at.x, clientY: at.y, button: 2 }));
		}, 500);
	};
	const move = (e: PointerEvent) => {
		if (timer && Math.hypot(e.clientX - at.x, e.clientY - at.y) > 10) cancel();
	};
	// Android sends its own contextmenu for a long press: that one opens the menu instead.
	const native = (e: MouseEvent) => {
		if (!e.isTrusted || !timer) return;
		cancel();
		opened = true;
	};
	const click = (e: MouseEvent) => {
		if (!opened) return;
		opened = false;
		e.preventDefault();
		e.stopPropagation();
	};
	node.addEventListener('pointerdown', down);
	node.addEventListener('pointermove', move);
	node.addEventListener('pointerup', cancel);
	node.addEventListener('pointercancel', cancel);
	node.addEventListener('contextmenu', native, true);
	node.addEventListener('click', click, true);
	return () => {
		cancel();
		node.removeEventListener('pointerdown', down);
		node.removeEventListener('pointermove', move);
		node.removeEventListener('pointerup', cancel);
		node.removeEventListener('pointercancel', cancel);
		node.removeEventListener('contextmenu', native, true);
		node.removeEventListener('click', click, true);
	};
}
