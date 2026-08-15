// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect } from 'vitest';
import { ConnectError, Code } from '@connectrpc/connect';
import {
	isAbortedError,
	isAlreadyExistsError,
	isInvalidArgumentError,
	isNotFoundError,
	isUnimplementedError,
	classifyAdminFailure,
} from './errors';

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

/*
 * The remaining four classifiers are asserted as one table because the property
 * that matters is the SAME for each and is a property of the set: each predicate
 * answers for exactly one code and refuses every other code and every non-Connect
 * value. Written as four independent describe blocks, a copy-paste that left two
 * predicates testing the same code would pass.
 */
const classifiers: ReadonlyArray<{
	name: string;
	fn: (e: unknown) => boolean;
	code: Code;
}> = [
	{ name: 'isNotFoundError', fn: isNotFoundError, code: Code.NotFound },
	{ name: 'isAbortedError', fn: isAbortedError, code: Code.Aborted },
	{ name: 'isAlreadyExistsError', fn: isAlreadyExistsError, code: Code.AlreadyExists },
	{ name: 'isInvalidArgumentError', fn: isInvalidArgumentError, code: Code.InvalidArgument },
];

describe('the phase 5 ConnectError classifiers', () => {
	it('answers true for its own code and false for every other classifier’s code', () => {
		for (const own of classifiers) {
			for (const other of classifiers) {
				const err = new ConnectError('refused', other.code);
				expect(
					own.fn(err),
					`${own.name} against ${String(other.code)}`,
				).toBe(own.code === other.code);
			}
		}
	});

	it('answers false for non-ConnectError values', () => {
		for (const { name, fn } of classifiers) {
			expect(fn(new Error('boom')), name).toBe(false);
			expect(fn('boom'), name).toBe(false);
			expect(fn(null), name).toBe(false);
			expect(fn(undefined), name).toBe(false);
		}
	});

	it('classifies exactly one code each — no two predicates share a code', () => {
		const codes = classifiers.map((c) => c.code);
		expect(new Set(codes).size).toBe(classifiers.length);
	});
});

// classifyAdminFailure is TOTAL: every code lands in exactly one class, and
// the residue defaults to the conservative branch rather than the retry one.
describe('classifyAdminFailure', () => {
	const denialCodes: [string, Code][] = [
		['PermissionDenied', Code.PermissionDenied],
		['Unauthenticated', Code.Unauthenticated],
		['NotFound', Code.NotFound],
		['Internal', Code.Internal],
		['Unknown', Code.Unknown],
		['FailedPrecondition', Code.FailedPrecondition],
	];

	for (const [name, code] of denialCodes) {
		it(`classifies ${name} as denial, so the ordinary not-found renders`, () => {
			expect(classifyAdminFailure(new ConnectError('refused', code))).toBe('denial');
		});
	}

	it('classifies Unavailable as infrastructure', () => {
		expect(classifyAdminFailure(new ConnectError('down', Code.Unavailable))).toBe('infrastructure');
	});

	it('classifies DeadlineExceeded as infrastructure', () => {
		expect(classifyAdminFailure(new ConnectError('slow', Code.DeadlineExceeded))).toBe(
			'infrastructure',
		);
	});

	it('classifies a plain transport throw as infrastructure', () => {
		expect(classifyAdminFailure(new Error('network down'))).toBe('infrastructure');
		expect(classifyAdminFailure('boom')).toBe('infrastructure');
	});

	it('defaults an unenumerated code to denial — the fail-safe direction', () => {
		// ResourceExhausted appears in no branch of the classifier. It must land
		// on the conservative screen, not on one implying something to retry.
		expect(classifyAdminFailure(new ConnectError('quota', Code.ResourceExhausted))).toBe('denial');
	});
});
