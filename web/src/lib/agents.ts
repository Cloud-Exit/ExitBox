// Original glyphs for each agent, not the vendors' logos: a coloured tile with a simple mark.

export interface AgentMark {
	/** Tile background; white glyphs read on it in light and dark. */
	tile: string;
	/** Stroke paths in a 24x24 box, or null for a monogram. */
	glyph: string | null;
}

const marks: Record<string, AgentMark> = {
	claude: { tile: 'bg-orange-600', glyph: '<path d="M12 3v18M3 12h18M5.6 5.6l12.8 12.8M18.4 5.6 5.6 18.4"/>' },
	codex: { tile: 'bg-emerald-600', glyph: '<polyline points="8 6 2 12 8 18"/><polyline points="16 6 22 12 16 18"/>' },
	copilot: { tile: 'bg-violet-600', glyph: '<circle cx="12" cy="12" r="9"/><polygon points="16 8 13.5 13.5 8 16 10.5 10.5"/>' },
	cursor: { tile: 'bg-slate-600', glyph: '<path d="M5 3l5.5 17 2.5-7 7-2.5z"/>' },
	kimi: { tile: 'bg-indigo-600', glyph: '<path d="M20 14.5A8.5 8.5 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/>' },
	opencode: { tile: 'bg-zinc-600', glyph: '<path d="M9 3H5v18h4"/><path d="M15 3h4v18h-4"/>' },
	pi: { tile: 'bg-rose-600', glyph: '<path d="M3 7h18"/><path d="M8.5 7c0 5-1 9.5-3 13"/><path d="M15.5 7v9.5c0 2 1 3.5 3.5 3.5"/>' },
	qwen: { tile: 'bg-sky-600', glyph: '<circle cx="11.5" cy="11.5" r="7.5"/><path d="M14 14l6.5 6.5"/>' }
};

export function agentMark(name: string): AgentMark {
	return marks[name] ?? { tile: 'bg-slate-500', glyph: null };
}
