// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { listContent } from '$lib/stores/contentStore';
import type { ContentItem } from '$lib/stores/contentStore';

export async function load() {
  const items = await listContent('landing.');

  const hero = items.find((i) => i.key === 'landing.hero');
  const pitch = items.find((i) => i.key === 'landing.pitch');
  const features: ContentItem[] = items
    .filter((i) => i.key.startsWith('landing.features.'))
    .sort((a, b) => Number(a.metadata.order ?? '99') - Number(b.metadata.order ?? '99'));
  const connectInfo = items.find((i) => i.key === 'landing.connect');

  return { hero, pitch, features, connectInfo };
}
