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
					include: ['src/**/*.svelte.test.ts']
				}
			}
		]
	}
});
