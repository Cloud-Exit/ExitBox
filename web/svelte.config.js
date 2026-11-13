import adapter from '@sveltejs/adapter-static';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	kit: {
		// The Go binary embeds the build (internal/web/assets).
		adapter: adapter({ pages: '../internal/web/assets', assets: '../internal/web/assets', fallback: 'index.html', strict: true }),
		output: { bundleStrategy: 'single' },
		// A fixed name keeps builds reproducible; CI checks the committed build.
		version: { name: 'exitbox-web' },
		// bun test files import bun:test, whose types are not installed.
		typescript: {
			config: (c) => {
				c.exclude = [...(c.exclude ?? []), '../src/**/*.test.ts'];
			}
		}
	}
};

export default config;
