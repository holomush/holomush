// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'https://holomush.dev',
  integrations: [
    starlight({
      title: 'HoloMUSH',
      description: 'Modern MUSH platform with Lua & Go plugins',
      logo: { src: './src/assets/logo.png', alt: 'HoloMUSH' },
      favicon: '/favicon.png',
      social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/holomush/holomush' }],
      customCss: ['./src/styles/custom.css'],
      // sidebar added in Task 12; plugins (mermaid, llms-txt) added in Tasks 8 & 13
    }),
  ],
});
