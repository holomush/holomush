// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLlmsTxt from 'starlight-llms-txt';
import starlightClientMermaid from '@pasqal-io/starlight-client-mermaid';

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
      plugins: [starlightClientMermaid(), starlightLlmsTxt({ projectName: 'HoloMUSH' })],
      sidebar: [
        { label: 'Guide', items: [
          { slug: 'guide' }, { slug: 'guide/the-world' }, { slug: 'guide/connecting' },
          { slug: 'guide/commands' }, { slug: 'guide/building' },
        ] },
        { label: 'Operating', items: [
          { slug: 'operating' }, { slug: 'operating/deployment' }, { slug: 'operating/installation' },
          { slug: 'operating/configuration' }, { slug: 'operating/database' },
          { slug: 'operating/authentication' }, { slug: 'operating/telnet-security' },
          { slug: 'operating/ca-rotation' }, { slug: 'operating/crypto-setup' },
          { slug: 'operating/operations' }, { slug: 'operating/sentry' },
          { slug: 'operating/verifying-releases' },
        ] },
        { label: 'Extending', items: [
          { slug: 'extending' }, { slug: 'extending/getting-started' },
          { slug: 'extending/plugin-guide' }, { slug: 'extending/plugin-config' },
          { slug: 'extending/access-control' }, { slug: 'extending/abac-attribute-resolver' },
          { slug: 'extending/event-sensitivity' }, { slug: 'extending/plugin-crypto-readback' },
          { slug: 'extending/api-guide' }, { slug: 'extending/events' },
        ] },
        { label: 'Contributing', items: [
          { slug: 'contributing' }, { slug: 'contributing/architecture' },
          { slug: 'contributing/coding-standards' }, { slug: 'contributing/authentication' },
          { slug: 'contributing/database-migrations' }, { slug: 'contributing/event-store' },
          { slug: 'contributing/event-delivery' }, { slug: 'contributing/event-emit-pipeline' },
          { slug: 'contributing/hostfunc-context-audit' }, { slug: 'contributing/lifecycle-and-health' },
          { slug: 'contributing/pr-guide' }, { slug: 'contributing/sessions' },
        ] },
        { label: 'Reference', items: [
          { slug: 'reference' }, { slug: 'reference/access-control' },
          { slug: 'reference/grpc-api' }, { slug: 'reference/events' },
        ] },
      ],
    }),
  ],
});
