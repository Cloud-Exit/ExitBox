import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import type { Terminal } from '@xterm/xterm';
import { codexQuestionInput, codexViewport, parseCodexQuestion } from './codexQuestions';
// Verbatim Apache-2.0 upstream snapshots at https://github.com/openai/codex/tree/rust-v0.171.0/codex-rs/tui/src/bottom_pane
const snapshot = (name: string, async = false) => readFileSync(new URL(`./testdata/codex-0.171.0/${async ? 'async_codex_tui__bottom_pane__async_questions__tests__' : 'codex_tui__bottom_pane__request_user_input__tests__request_user_input_'}${name}.snap`, import.meta.url), 'utf8').split('---\n').at(-1)!.split('\n');
for (const name of ['options', 'multi_question_first', 'wrapped_options', 'footer_wrap', 'hidden_options_footer', 'tight_height']) {
	test(`recognizes pinned modal ${name} and sends exactly a digit`, () => {
		const q = parseCodexQuestion(snapshot(name))!;
		expect(q?.kind).toBe('options');
		const digit = q.options[0].digit;
		expect(codexQuestionInput(q, q, { kind: 'choice', digit })).toBe(digit);
		expect(codexQuestionInput(q, q, { kind: 'choice', digit: 'arbitrary answer' })).toBeNull();
		expect(codexQuestionInput(q, q, { kind: 'submit' })).toBeNull();
		expect(codexQuestionInput(q, q, { kind: 'notes' })).toBe('\t');
	});
}
test('modal notes and freeform use the real editor and explicit submit, never digit actions', () => {
	for (const name of ['options_notes_visible', 'freeform', 'multi_question_last']) {
		const q = parseCodexQuestion(snapshot(name))!;
		expect(q?.kind).toBe('text');
		expect(codexQuestionInput(q, q, { kind: 'choice', digit: '1' })).toBeNull();
		expect(codexQuestionInput(q, q, { kind: 'notes' })).toBeNull();
		expect(codexQuestionInput(q, q, { kind: 'submit' })).toBe('\r');
	}
	expect(parseCodexQuestion(snapshot('multi_question_last'))?.submitAll).toBe(true);
});
test('typed drafts stay in the native editor and do not become question text', () => {
	const modal = parseCodexQuestion(snapshot('freeform').map(l => l.replace('› Type your answer (optional)', '› A draft answer')))!;
	expect(modal?.question).toBe('Share details.');
	expect(codexQuestionInput(modal, modal, { kind: 'submit' })).toBe('\r');
	const notes = parseCodexQuestion(snapshot('options_notes_visible').map(l => l.replace('› Add notes', '› Some notes')))!;
	expect(notes?.question).toBe('Choose an option.');
	expect(notes.kind).toBe('text');
	const async = parseCodexQuestion(snapshot('question_multiline', true).map(l => l.replace('Type your answer', 'A draft answer')))!;
	expect(async?.question).toContain('Then review the diagnostics');
	expect(async.question).not.toContain('A draft answer');
});
test('upstream named Other is a normal choice, distinct from synthetic Other', () => {
	const q = parseCodexQuestion(snapshot('question_named_other', true))!;
	expect(q?.kind).toBe('options');
	expect(q.options.map(o => o.label)).toEqual(['Other', 'Other (write an answer)']);
	expect(codexQuestionInput(q, q, { kind: 'choice', digit: '1' })).toBe('1');
	const other = parseCodexQuestion(snapshot('question_named_other', true).map(l => l.replace('› 1.', '  1.').replace('  2.', '› 2.')))!;
	expect(other?.kind).toBe('text');
	const typedOther = parseCodexQuestion(snapshot('question_named_other', true).map(l => l.replace('› 1.', '  1.').replace('  2. Other (write an answer)', '› 2. 123自由回答')))!;
	expect(typedOther?.kind).toBe('text');
	expect(codexQuestionInput(typedOther, typedOther, { kind: 'choice', digit: '1' })).toBeNull();
	expect(codexQuestionInput(q, other, { kind: 'choice', digit: '1' })).toBeNull();
});
test('async multiline freeform and wrapped selected options', () => {
	const text = parseCodexQuestion(snapshot('question_multiline', true))!;
	expect(text?.kind).toBe('text');
	expect(codexQuestionInput(text, text, { kind: 'submit' })).toBe('\r');
	const q = parseCodexQuestion(snapshot('question_wrapped_selected_option', true))!;
	expect(q?.kind).toBe('options');
	expect(q.options[0].label).toBe('A suggested answer that is long enough to wrap across multiple rows');
	expect(codexQuestionInput(q, q, { kind: 'choice', digit: '2' })).toBe('2');
});
test('screenshot queued question opens only on explicit Shift+Left action', () => {
	const lines = ['• Queued follow-up inputs', '  ? 1 question', '    shift+← to answer', '› Ask Codex to do anything', 'GPT-6.2-Astra xhigh Fast · /workspace'];
	const q = parseCodexQuestion(lines)!;
	expect(q?.kind).toBe('queued');
	const countdown = parseCodexQuestion(lines.map(l => l.replace('? 1 question', '? 1 question · 20s')))!;
	expect(countdown?.countdown).toBe(20);
	expect(countdown.id).toBe(q.id);
	expect(codexQuestionInput(q, q, { kind: 'open' })).toBe('\x1b[1;2D');
	expect(codexQuestionInput(q, q, { kind: 'choice', digit: '1' })).toBeNull();
	expect(parseCodexQuestion(lines.map(l => l.replace('shift+←', 'f9')))).toBeNull();
});
test('ordinary output, startup, confirmation, clipped and remapped prompts stay native', () => {
	for (const lines of [[], ['Starting OpenAI Codex'], ['1. Option 1', '2. Option 2'], ['Question 1/2 (2 unanswered)', 'Run tests?'], snapshot('unanswered_confirmation'), snapshot('question_narrow_other', true), snapshot('question_remapped_choice_footer', true), snapshot('question_clipped_choice_rejected', true), snapshot('selected_other_placeholder', true)])
		expect(parseCodexQuestion(lines)).toBeNull();
	const q = parseCodexQuestion(snapshot('options'))!;
	expect(parseCodexQuestion([...snapshot('options'), '$ echo done'])).toBeNull();
	expect(codexQuestionInput(q, null, { kind: 'choice', digit: '1' })).toBeNull();
	expect(codexQuestionInput(q, parseCodexQuestion(snapshot('multi_question_first')), { kind: 'choice', digit: '1' })).toBeNull();
});
test('countdown redraw does not change identity but expiration rejects actions', () => {
	const q = parseCodexQuestion(snapshot('auto_resolution_countdown'))!;
	expect(q.countdown).toBe(60);
	const expired = parseCodexQuestion(snapshot('auto_resolution_countdown').map(l => l.replace('1m 00s', '0m 00s')))!;
	expect(expired.id).toBe(q.id);
	expect(codexQuestionInput(q, expired, { kind: 'choice', digit: '1' })).toBeNull();
});
test('parsed viewport reads current screen only, respects scrollback, wraps and tmux status/scrollbar', () => {
	const rows = [{ text: 'old scrollback', wrapped: false }, { text: 'Question 1/', wrapped: false }, { text: '1 (1 unanswered)', wrapped: true }, { text: 'body│', wrapped: false, bar: true }, { text: '[0] 0:bash*', wrapped: false, status: true }];
	const t = { cols: 20, rows: 4, buffer: { active: { baseY: 1, viewportY: 1, getLine: (y: number) => { const r = rows[y]; return r && { isWrapped: r.wrapped, translateToString: (_trim: boolean, _start?: number, end?: number) => end ? r.text.slice(0, -1) : r.text, getCell: (x: number) => ({ isBgPalette: () => !!r.status || (!!r.bar && x === 19), getBgColor: () => r.status ? 2 : 54 }) }; } } } } as unknown as Terminal;
	expect(codexViewport(t)).toEqual(['Question 1/1 (1 unanswered)', 'body']);
	t.buffer.active.viewportY = 0;
	expect(codexViewport(t)).toBeNull();
});
