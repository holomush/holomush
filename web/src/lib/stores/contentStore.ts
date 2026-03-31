// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';

const client = createClient(WebService, transport);

export interface ContentItem {
  key: string;
  contentType: string;
  body: string;
  metadata: Record<string, string>;
}

function decodeItem(item: { key: string; contentType: string; body: Uint8Array; metadata: { [key: string]: string } } | undefined): ContentItem | null {
  if (!item) return null;
  return {
    key: item.key,
    contentType: item.contentType,
    body: new TextDecoder().decode(item.body),
    metadata: item.metadata ?? {},
  };
}

export async function getContent(key: string): Promise<ContentItem | null> {
  try {
    const resp = await client.webGetContent({ key });
    return decodeItem(resp.item);
  } catch {
    return null;
  }
}

export async function listContent(prefix: string): Promise<ContentItem[]> {
  try {
    const resp = await client.webListContent({ prefix, limit: 0, cursor: '' });
    return (resp.items ?? []).map(decodeItem).filter(Boolean) as ContentItem[];
  } catch {
    return [];
  }
}
