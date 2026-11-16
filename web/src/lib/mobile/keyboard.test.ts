import { expect, test } from 'bun:test';
import { keyboardUp, tallest } from './keyboard';

test('the tallest height at one width is kept', () => {
	let f = tallest(null, 390, 700);
	f = tallest(f, 390, 400);
	expect(f).toEqual({ w: 390, h: 700 });
	expect(tallest(f, 390, 760)).toEqual({ w: 390, h: 760 });
});

test('a rotation starts over', () => {
	expect(tallest({ w: 390, h: 760 }, 844, 340)).toEqual({ w: 844, h: 340 });
});

test('a keyboard takes far more than a browser bar', () => {
	const f = { w: 390, h: 760 };
	expect(keyboardUp(420, f)).toBe(true);
	expect(keyboardUp(700, f)).toBe(false);
	expect(keyboardUp(760, f)).toBe(false);
	expect(keyboardUp(300, null)).toBe(false);
});
