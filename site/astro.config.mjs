// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	integrations: [
		starlight({
			title: 'HoloMUSH',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/holomush/holomush' }],
		}),
	],
});
