module.exports = {
  title: 'Gatekeeper',
  tagline: 'Policy Controller for Kubernetes',
  url: 'https://open-policy-agent.github.io/gatekeeper/website/docs/',
  baseUrl: '/gatekeeper/website/',
  onBrokenLinks: 'throw',
  favicon: 'img/favicon.ico',
  organizationName: 'open-policy-agent',
  projectName: 'gatekeeper',
  themeConfig: {
    colorMode: {
      defaultMode: 'light',
      disableSwitch: false,
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Gatekeeper',
      logo: {
        alt: 'Gatekeeper logo',
        src: 'img/logo.svg',
      },
      items: [
        {
          href: 'https://github.com/open-policy-agent/gatekeeper',
          label: 'GitHub',
          position: 'left',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Community',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/open-policy-agent/gatekeeper',
            },
            {
              label: 'Slack',
              href: 'https://openpolicyagent.slack.com/messages/CDTN970AX',
            },
            {
              label: 'Meetings',
              href: 'https://docs.google.com/document/d/1A1-Q-1OMw3QODs1wT6eqfLTagcGmgzAJAjJihiO3T48/edit)',
            },
          ],
        },
      ],
    },
  },
  presets: [
    [
      '@docusaurus/preset-classic',
      {
        docs: {
          sidebarPath: require.resolve('./sidebars.js'),
          editUrl:
            'https://open-policy-agent.github.io/gatekeeper/website/docs',
        },
        theme: {
          customCss: require.resolve('./src/css/custom.css'),
        },
      },
    ],
  ],
};
