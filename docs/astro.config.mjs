// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

const SITE_URL = 'https://skillmythorg.github.io';
const BASE_PATH = '/agentic-vc';
const REPO_URL = 'https://github.com/SkillMythOrg/agentic-vc';

export default defineConfig({
  site: SITE_URL,
  base: BASE_PATH,
  integrations: [
    starlight({
      title: 'AVC',
      description:
        'Snapshot, diff, branch, and merge primitives designed for agent-assisted development. CLI, VSCode extension, web UI, and MCP server.',
      logo: { src: './src/assets/logo.svg' },
      favicon: '/favicon.svg',
      social: [
        { icon: 'github', label: 'GitHub', href: REPO_URL },
      ],
      editLink: {
        baseUrl: `${REPO_URL}/edit/main/docs/`,
      },
      lastUpdated: true,
      pagination: true,
      customCss: ['./src/styles/custom.css'],
      sidebar: [
        {
          label: 'Introduction',
          items: [
            { label: 'Overview', slug: 'index' },
            { label: 'Why AVC', slug: 'why' },
            { label: 'Installation', slug: 'install' },
            { label: 'Quick Start', slug: 'quick-start' },
          ],
        },
        {
          label: 'Concepts',
          items: [
            { label: 'Snapshots', slug: 'concepts/snapshots' },
            { label: 'Branches', slug: 'concepts/branches' },
            { label: 'Merges', slug: 'concepts/merges' },
            { label: 'Storage Layout', slug: 'concepts/storage' },
          ],
        },
        {
          label: 'CLI Reference',
          items: [{ autogenerate: { directory: 'cli' } }],
        },
        {
          label: 'VSCode Extension',
          items: [{ autogenerate: { directory: 'extension' } }],
        },
        {
          label: 'Web UI',
          items: [{ autogenerate: { directory: 'web-ui' } }],
        },
        {
          label: 'Agent Integration',
          items: [{ autogenerate: { directory: 'agents' } }],
        },
      ],
    }),
  ],
});
