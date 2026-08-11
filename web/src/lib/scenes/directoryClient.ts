// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { client } from './client';

export interface DirectoryCharacter {
	id: string;
	name: string;
}

/** Lists the characters this viewer can reach, id + name only, for the invite
 *  picker. The call is viewer-scoped: the rung follows from the session cookie
 *  the gateway forwards, so there is no acting-character argument to pass. A
 *  character the viewer cannot reach is simply absent from the result. */
export async function listAllCharacters(): Promise<DirectoryCharacter[]> {
	const res = await client.webListCharacterDirectory({});
	return res.characters.map((c) => ({ id: c.id, name: c.name }));
}
