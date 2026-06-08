// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * downloadBlob triggers a browser download for the given content.
 * MUST be called from a user-initiated event handler (click, etc.) so the
 * createObjectURL / anchor-click pair fires in the right security context.
 * The function is synchronous and revokes the object URL immediately after
 * the click is dispatched.
 */
export function downloadBlob(content: Uint8Array | string, mime: string, filename: string): void {
	// Cast to satisfy strict Blob constructor overload — ArrayBufferLike covers ArrayBuffer.
	const blob = new Blob([content as unknown as ArrayBuffer | string], { type: mime });
	const url = URL.createObjectURL(blob);
	const a = Object.assign(document.createElement('a'), { href: url, download: filename });
	a.click();
	URL.revokeObjectURL(url);
}
