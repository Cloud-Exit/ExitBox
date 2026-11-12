import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	server: {
		// `bun run dev` talks to a running `exitbox web`.
		proxy: { '/api': { target: 'http://127.0.0.1:4848', ws: true } }
	}
});
