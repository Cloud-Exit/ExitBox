// Codex rust-v0.171.0: bottom_pane/{request_user_input,async_questions,questions.rs}.
// Recognize complete live viewport prompts; never interpret a stream fragment or terminal scrollback as a question.
import type { Terminal } from '@xterm/xterm';
export type CodexQuestion = {
	id: string;
	kind: 'queued' | 'options' | 'text';
	title: string;
	question: string;
	options: {
		digit: string;
		label: string;
	}[];
	notes: boolean;
	canAddNotes: boolean;
	submitAll: boolean;
	countdown?: number;
};
export type CodexQuestionAction = {
	kind: 'open' | 'notes' | 'submit';
} | {
	kind: 'choice';
	digit: string;
};
const compact = (s: string) => s.replace(/\s+/g, ' ').trim();
/** Read only the current screen at its live bottom, with xterm's soft wraps joined. */
export function codexViewport(t: Terminal): string[] | null {
	const buf = t.buffer.active;
	if (buf.viewportY !== buf.baseY)
		return null;
	const lines: string[] = [];
	for (let y = buf.baseY; y < buf.baseY + t.rows; y++) {
		const row = buf.getLine(y);
		if (!row)
			continue;
		let text = row.translateToString(true);
		// tmux's one-column pane scrollbar uses these reserved palette backgrounds (where enabled by tmux).
		const edge = row.getCell(t.cols - 1);
		if (edge?.isBgPalette() && (edge.getBgColor() === 54 || edge.getBgColor() === 129))
			text = row.translateToString(true, 0, t.cols - 1);
		// A tmux status line is one full-width palette background, unlike the program's default background.
		if (y === buf.baseY + t.rows - 1 && edge?.isBgPalette() && edge.getBgColor() !== 54 && edge.getBgColor() !== 129) {
			const bg = edge.getBgColor();
			if (Array.from({ length: t.cols }, (_, x) => row.getCell(x)).every(c => c?.isBgPalette() && c.getBgColor() === bg))
				continue;
		}
		if (row.isWrapped && lines.length)
			lines[lines.length - 1] += text;
		else
			lines.push(text);
	}
	return lines;
}
/** The countdown may redraw without changing which question a button answers. */
function countdown(text: string): number | undefined {
	const match = text.match(/(?:auto-resolves in | · )(?:(\d+)m\s*)?(\d+)s/);
	return match ? Number(match[1] ?? 0) * 60 + Number(match[2]) : undefined;
}
export function parseCodexQuestion(viewport: readonly string[]): CodexQuestion | null {
	const lines = viewport.map(s => s.trim());
	while (lines.at(-1) === '')
		lines.pop();
	if (!lines.length)
		return null;
	const queue = lines.findLastIndex(s => /^•?\s*Queued follow-up inputs$/.test(s));
	if (queue >= 0) {
		const summary = lines.slice(queue + 1).findIndex(s => /^\? [1-9]\d* questions?(?: · (?:auto-resolves in )?(?:\d+m\s*)?\d+s)?$/.test(s));
		if (summary >= 0) {
			const at = queue + 1 + summary;
			if (/^(?:shift\+←|⇧←) to answer$/.test(lines[at + 1] ?? '') && lines.length - at <= 18) {
				const title = lines[at].replace(/^\? /, '').replace(/ · (?:auto-resolves in )?(?:\d+m\s*)?\d+s$/, '');
				return { id: `queued:${title}`, kind: 'queued', title, question: '', options: [], notes: false, canAddNotes: false, submitAll: false, countdown: countdown(lines[at]) };
			}
		}
	}
	// The complete footer is the mode/keymap proof. Unknown, clipped or remapped key bindings use the native TUI.
	let footerAt = lines.findLastIndex(s => /^(?:(?:option \d+\/\d+ \| )?tab (?:to add notes|or esc to clear notes)|enter to submit (?:answer|all)|enter submit)/.test(s));
	if (footerAt < 0)
		return null;
	// Wrapped footer rows can start with a submit tip; include the preceding notes tip.
	if (footerAt > 0 && /tab (?:to add notes|or esc to clear notes)/.test(lines[footerAt - 1]))
		footerAt--;
	const footer = compact(lines.slice(footerAt).join(' '));
	const modal = /enter to submit (?:answer|all)/.test(footer);
	const async = /^enter submit\s+⌃\] skip/.test(footer);
	if (!modal && !async)
		return null;
	const known = modal
		? /option \d+\/\d+|tab to add notes|tab or esc to clear notes|enter to submit (?:answer|all)|←\/→ to navigate questions|⌃p \/ ⌃n change question|esc to interrupt/g
		: /enter submit|⌃\] skip|option \d+\/\d+|⇧→ (?:main prompt|prev question)|⇧← (?:next question|queued messages)/g;
	if (footer.replace(known, '').replace(/[|\s]/g, ''))
		return null;
	const body = lines.slice(0, footerAt);
	const headerAt = body.findLastIndex(s => /^Question \d+\/\d+ \(\d+ unanswered\)(?: · auto-resolves in .+)?$/.test(s));
	if (modal && headerAt < 0)
		return null;
	const start = headerAt >= 0 ? headerAt + 1 : Math.max(0, body.findLastIndex(s => /^\d+ of \d+$/.test(s)) + 1);
	const content = body.slice(start);
	const options: CodexQuestion['options'] = [];
	let selected: string | undefined;
	let firstOption = -1;
	let notesAt = -1;
	for (let i = 0; i < content.length; i++) {
		const match = content[i].match(/^(›\s*)?([1-9])\.\s+(.+)$/);
		if (match) {
			if (firstOption < 0)
				firstOption = i;
			if (options.some(o => o.digit === match[2]))
				return null;
			options.push({ digit: match[2], label: match[3] });
			if (match[1])
				selected = match[2];
		}
		else if (/^›\s+(?![1-9]\.)/.test(content[i]) || /^›?\s*(?:Add notes|Type your answer(?: \(optional\))?)$/.test(content[i])) {
			notesAt = i;
		}
		else if (options.length && content[i]) {
			// ratatui wraps long option labels into indented continuation rows.
			options[options.length - 1].label += ' ' + content[i];
		}
	}
	const notes = /tab or esc to clear notes/.test(footer);
	// Async questions always append a synthetic Other input after model-authored choices. Its label
	// becomes arbitrary text while editing; the final option number, not that text, identifies it.
	const optionTotal = footer.match(/option \d+\/(\d+)/)?.[1];
	const finalDigit = optionTotal ?? options.at(-1)?.digit;
	const selectedOther = async && selected !== undefined && selected === finalDigit;
	const textMode = notes || !options.length || !!selectedOther;
	if (!textMode && (!selected || !options.length))
		return null;
	if (modal && !notes && options.length && !footer.includes('tab to add notes'))
		return null;
	if (modal && !notes && !footer.includes('esc to interrupt'))
		return null;
	if (options.some(o => /…$/.test(o.label)))
		return null;
	const end = firstOption >= 0 ? firstOption : notesAt >= 0 ? notesAt : content.length;
	let question = compact(content.slice(0, end).join(' '));
	if (!modal) {
		// A single async question has no progress title. Use the last text paragraph above its choices/input.
		const paragraphs = content.slice(0, end).join('\n').split(/\n\s*\n/).filter(s => s.trim());
		const questionBlock = !options.length && notesAt < 0 ? paragraphs.at(-2) : paragraphs.at(-1);
		question = compact(questionBlock ?? '');
	}
	if (!question)
		return null;
	const title = headerAt >= 0 ? body[headerAt].replace(/ · auto-resolves in .+$/, '') : 'Codex question';
	const id = JSON.stringify([modal ? 'modal' : 'async', title, question, options, textMode, notes]);
	return { id, kind: textMode ? 'text' : 'options', title, question, options, notes, canAddNotes: modal && !textMode, submitAll: footer.includes('submit all'), countdown: headerAt >= 0 ? countdown(body[headerAt]) : undefined };
}
/** Returns only the explicit action's key. In particular choosing a digit never adds Enter. */
export function codexQuestionInput(expected: CodexQuestion, current: CodexQuestion | null, action: CodexQuestionAction): string | null {
	if (!current || current.id !== expected.id || current.countdown === 0)
		return null;
	if (action.kind === 'open')
		return current.kind === 'queued' ? '\x1b[1;2D' : null;
	if (action.kind === 'choice')
		return current.kind === 'options' && current.options.some(o => o.digit === action.digit) ? action.digit : null;
	if (action.kind === 'notes')
		return current.kind === 'options' && current.canAddNotes ? '\t' : null;
	return current.kind === 'text' ? '\r' : null;
}
