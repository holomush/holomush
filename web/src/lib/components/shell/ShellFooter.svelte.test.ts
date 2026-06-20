// web/src/lib/components/shell/ShellFooter.svelte.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import { clearFooter } from '$lib/stores/footerBridge';
import ShellFooter from './ShellFooter.svelte';

afterEach(() => {
  clearFooter();
  document.body.replaceChildren();
});

describe('ShellFooter baseline', () => {
  it('renders the active section name and a go-to hint when nothing is registered', () => {
    clearFooter();
    const target = document.createElement('div');
    document.body.appendChild(target);
    const component = mount(ShellFooter, { target, props: { pathname: '/scenes' } });
    expect(target.textContent).toContain('Scenes');
    expect(target.textContent?.toLowerCase()).toContain('go to');
    unmount(component);
  });

  it('falls back to a generic label off any registered section', () => {
    clearFooter();
    const target = document.createElement('div');
    document.body.appendChild(target);
    const component = mount(ShellFooter, { target, props: { pathname: '/characters' } });
    expect(target.textContent).toContain('Workspace');
    unmount(component);
  });
});
