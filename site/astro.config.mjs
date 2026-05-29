// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLlmsTxt from 'starlight-llms-txt';
import starlightClientMermaid from '@pasqal-io/starlight-client-mermaid';
import starlightSidebarTopics from 'starlight-sidebar-topics';
import starlightLlmActions from 'starlight-llm-actions';

export default defineConfig({
  site: 'https://holomush.dev',
  integrations: [
    starlight({
      title: 'HoloMUSH',
      description: 'Modern MUSH platform with Lua & Go plugins',
      logo: {
        light: './src/assets/logo-light.svg',
        dark: './src/assets/logo-dark.svg',
        alt: 'HoloMUSH',
        replacesTitle: true,
      },
      favicon: '/favicon.svg',
      head: [
        { tag: 'link', attrs: { rel: 'icon', type: 'image/png', sizes: '32x32', href: '/favicon-32.png' } },
        { tag: 'link', attrs: { rel: 'icon', type: 'image/png', sizes: '16x16', href: '/favicon-16.png' } },
        { tag: 'link', attrs: { rel: 'apple-touch-icon', sizes: '180x180', href: '/apple-touch-icon.png' } },
        { tag: 'meta', attrs: { property: 'og:image', content: 'https://holomush.dev/og-card.png' } },
        { tag: 'meta', attrs: { property: 'og:image:width', content: '1200' } },
        { tag: 'meta', attrs: { property: 'og:image:height', content: '630' } },
        { tag: 'meta', attrs: { name: 'twitter:card', content: 'summary_large_image' } },
        { tag: 'meta', attrs: { name: 'twitter:image', content: 'https://holomush.dev/og-card.png' } },
      ],
      editLink: { baseUrl: 'https://github.com/holomush/holomush/edit/main/site/' },
      lastUpdated: true,
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/holomush/holomush' },
        { icon: 'comment-alt', label: 'Discussions', href: 'https://github.com/holomush/holomush/discussions' },
        // Discord: add when a real invite URL exists — do NOT ship a dead link:
        // { icon: 'discord', label: 'Discord', href: '<DISCORD_INVITE_URL>' },
      ],
      customCss: ['./src/styles/custom.css'],
      plugins: [
        starlightClientMermaid(),
        starlightLlmsTxt({ projectName: 'HoloMUSH' }),
        starlightLlmActions(),
        starlightSidebarTopics(
          [
            {
              label: 'Guide',
              link: '/guide/',
              icon: 'open-book',
              items: [{ autogenerate: { directory: 'guide' } }],
            },
            {
              label: 'Operating',
              link: '/operating/',
              icon: 'setting',
              items: [{ autogenerate: { directory: 'operating' } }],
            },
            {
              label: 'Extending',
              link: '/extending/',
              icon: 'puzzle',
              items: [{ autogenerate: { directory: 'extending' } }],
            },
            {
              label: 'Contributing',
              link: '/contributing/',
              icon: 'pencil',
              items: [{ autogenerate: { directory: 'contributing' } }],
            },
            {
              label: 'Reference',
              link: '/reference/',
              icon: 'information',
              items: [{ autogenerate: { directory: 'reference' } }],
            },
          ],
          { exclude: ['/'] },
        ),
      ],
    }),
  ],
});
