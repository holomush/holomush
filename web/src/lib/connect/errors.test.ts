// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect } from 'vitest';
import { ConnectError, Code } from '@connectrpc/connect';
import { isUnimplementedError } from './errors';

describe('isUnimplementedError', () => {
	it('returns true for ConnectError with Code.Unimplemented', () => {
		const err = new ConnectError('not implemented', Code.Unimplemented);
		expect(isUnimplementedError(err)).toBe(true);
	});

	it('returns false for ConnectError with a different code', () => {
		const err = new ConnectError('not found', Code.NotFound);
		expect(isUnimplementedError(err)).toBe(false);
	});

	it('returns false for non-ConnectError values', () => {
		expect(isUnimplementedError(new Error('boom'))).toBe(false);
		expect(isUnimplementedError('boom')).toBe(false);
		expect(isUnimplementedError(null)).toBe(false);
		expect(isUnimplementedError(undefined)).toBe(false);
	});
});
