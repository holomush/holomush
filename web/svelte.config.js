import adapter from '@sveltejs/adapter-static';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	kit: {
		adapter: adapter({
			pages: 'build',
			assets: 'build',
			fallback: 'index.html'
		}),
		// Content-Security-Policy for the prerendered SPA.
		//
		// adapter-static emits frozen HTML at build time, so a per-request nonce
		// is impossible — that leaves hashes. In `hash` mode SvelteKit computes the
		// sha256 of its own inline hydration bootstrap <script> during prerender and
		// injects a <meta http-equiv="content-security-policy"> tag carrying those
		// hashes. This meta tag is the authoritative script-src/style-src/connect-src
		// policy for the document.
		//
		// Header-only directives (frame-ancestors, base-uri, object-src) stay on the
		// Go SecurityHeadersMiddleware — browsers ignore frame-ancestors in a <meta>
		// tag, so it can only be enforced via the response header. The Go header is
		// deliberately split to NOT set default-src/script-src; otherwise both
		// policies would enforce and the header's same-origin script-src would block
		// the hashed inline bootstrap (two CSPs intersect — a script must satisfy
		// every active policy). See internal/web/security_headers.go.
		csp: {
			mode: 'hash',
			directives: {
				'default-src': ['self'],
				'script-src': ['self'],
				'style-src': ['self', 'unsafe-inline'],
				'img-src': ['self', 'data:'],
				'connect-src': ['self', 'ws:', 'wss:']
			}
		}
	}
};

export default config;
