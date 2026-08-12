// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * The one pending create confirmation, crossing exactly one navigation.
 *
 * D-88's honest half is that the player is shown the name the SERVER stored,
 * not the string they typed — and the surface that shows it is the roster, one
 * navigation away from the form that submitted. This module is how the value
 * gets there.
 *
 * IT IS NOT A QUERY PARAMETER, deliberately. A display name in the URL is a
 * shareable string, and any visitor reaching `/characters?created=<anything>`
 * could synthesise a confirmation the server never issued. A module-level value
 * written only by a successful create cannot be forged from outside the app.
 *
 * IT IS ONE-SHOT. `takeCreatedNotice` returns the notice and clears it in the
 * same call, so a later visit to the roster does not re-announce a create from
 * an earlier one.
 *
 * A plain module value rather than a Svelte store: the roster reads it once, on
 * mount, so nothing subscribes and reactivity would buy nothing.
 */

export type CreatedNotice = {
	/** The SERVER-returned display form, read from the create response. Never the submitted string. */
	name: string;
	/** The created character's id — the target of the repair link the incomplete variant offers. */
	characterId: string;
	/**
	 * True when a value the client submitted is absent from the create
	 * response. The create still SUCCEEDED; this flags a partial outcome and
	 * points at the surface that repairs it. It never means "try again".
	 */
	profileIncomplete: boolean;
};

let pending: CreatedNotice | null = null;

/** Records the confirmation a successful create earned. Called by the create flow only. */
export function setCreatedNotice(notice: CreatedNotice): void {
	pending = notice;
}

/**
 * Returns the pending confirmation and clears it, or null when none is pending.
 * The clear is part of the read: a second call returns null.
 */
export function takeCreatedNotice(): CreatedNotice | null {
	const notice = pending;
	pending = null;
	return notice;
}
