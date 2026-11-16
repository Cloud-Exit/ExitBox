import { expect, test } from 'bun:test';
import { takeLines } from './touchScroll';

test('whole lines, with the rest carried over', () => {
	expect(takeLines(45, 20)).toEqual({ lines: 2, rest: 5 });
	expect(takeLines(-45, 20)).toEqual({ lines: -2, rest: -5 });
	expect(takeLines(19, 20)).toEqual({ lines: 0, rest: 19 });
});

test('an unmeasured terminal scrolls nothing', () => {
	expect(takeLines(100, 0)).toEqual({ lines: 0, rest: 0 });
	expect(takeLines(100, NaN)).toEqual({ lines: 0, rest: 0 });
});
