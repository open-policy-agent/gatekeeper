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
    algolia: {
      appId: 'PT2IX43ZFM',
      apiKey: '9e442eec9ecd30ad131824f9738db98d',
      indexName: 'gatekeeper',
    },
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
        href: 'https://open-policy-agent.github.io/gatekeeper/website/docs/',
      },
      items: [
        {
          href: 'https://github.com/open-policy-agent/gatekeeper-library',
          label: 'Library',
          position: 'left',
        },
        {
          type: 'docsVersionDropdown',
          position: 'right',
        },
        {
          href: 'https://github.com/open-policy-agent/gatekeeper',
          position: 'right',
          className: 'header-github-link',
          'aria-label': 'GitHub repository',
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
            'https://github.com/open-policy-agent/gatekeeper/edit/master/website',
        },
        theme: {
          customCss: require.resolve('./src/css/custom.css'),
        },
      },
    ],
  ],
};
