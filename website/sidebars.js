module.exports = {
  docs: [
    {
      type: 'category',
      label: 'Getting Started',
      collapsed: false,
      items: [
        'intro',
        'install',
        'examples'
      ],
    },
    {
      type: 'category',
      label: 'How to use Gatekeeper',
      collapsed: false,
      items: [
        'howto',
        'audit',
        'violations',
        'sync',
        'exempt-namespaces',
        'library',
        'customize-startup',
        'customize-admission',
        'metrics',
        'debug',
        'emergency',
        'vendor-specific',
        'failing-closed',
        'mutation'
      ],
    },
    {
      type: 'category',
      label: 'Contributing',
      collapsed: false,
      items: [
        'help',
        'security'
      ],
    }
  ]
};
