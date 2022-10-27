# Website

This website is built using [Docusaurus 2](https://v2.docusaurus.io/), a modern static website generator.

## Installation

```console
yarn install
```

## Local Development

```console
yarn start
```

This command starts a local development server and open up a browser window. Most changes are reflected live without having to restart the server.

## Build

```console
yarn build
```

This command generates static content into the `build` directory and can be served using any static contents hosting service.

## Deployment

```console
GIT_USER=<Your GitHub username> USE_SSH=true yarn deploy
```

If you are using GitHub pages for hosting, this command is a convenient way to build the website and push to the `gh-pages` branch.

## Search

Gatekeeper docs website uses Algolia DocSearch service. Please see [here](https://docusaurus.io/docs/search) for more information.

If the search index has any issues:

1. Go to [Algolia search dashboard](https://www.algolia.com/apps/PT2IX43ZFM/explorer/browse/gatekeeper)
1. Click manage index and export configuration
1. Delete the index
1. Import saved configuration
1. Go to [Algolia crawler](https://crawler.algolia.com/admin/crawlers/a953b469-c85a-4dc6-8a1c-16028abc1936/overview) and restart crawling manually (takes about a few minutes to crawl). This is scheduled to run every week automatically.
