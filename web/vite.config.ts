import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vitest/config';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	test: {
		globals: false,
		// Shared across both projects (inherited via `extends: true`).
		environment: 'jsdom',
		setupFiles: ['./src/test-setup.ts'],
		// Two projects: server-side logic tests (`*.test.ts`) run as before,
		// while Svelte component tests (`*.svelte.test.ts`) resolve the
		// `browser` entry points so `mount` works under jsdom (per the Svelte
		// testing docs).
		projects: [
			{
				extends: true,
				test: {
					name: 'server',
					include: ['src/**/*.test.ts'],
					exclude: ['src/**/*.svelte.test.ts']
				}
			},
			{
				extends: true,
				resolve: { conditions: ['browser'] },
				test: {
					name: 'client',
					include: ['src/**/*.svelte.test.ts'],
					// Both entries are required: a project-level `setupFiles` REPLACES
					// the inherited one rather than appending to it, so dropping the
					// shared file here would silently un-polyfill localStorage.
					// The client-only addition drains bits-ui's deferred body-scroll-lock
					// cleanup, which otherwise outlives the jsdom environment and fails
					// the run with an unhandled `document is not defined`.
					setupFiles: ['./src/test-setup.ts', './src/test-setup.client.ts']
				}
			}
		]
	}
});
