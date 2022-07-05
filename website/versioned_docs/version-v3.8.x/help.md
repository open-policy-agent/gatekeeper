---
id: help
title: How to contribute
---
Thanks for your interest in contributing to the Gatekeeper project! This document will help answer common questions you may have during your contribution.

## Where to start?
Join us to help define the direction and implementation of this project!

- File [GitHub Issues](https://github.com/open-policy-agent/gatekeeper/issues)
  to report bugs, request features, or ask questions asynchronously.

- Ask questions in [OPA Gatekeeper Community Discussions](https://github.com/open-policy-agent/community/discussions/categories/gatekeeper)
  
- Join the [`#opa-gatekeeper`](https://openpolicyagent.slack.com/messages/CDTN970AX)
  channel on [OPA Slack](https://slack.openpolicyagent.org/) to talk to the maintainers and other contributors asynchronously.

- Join [weekly meetings](https://docs.google.com/document/d/1A1-Q-1OMw3QODs1wT6eqfLTagcGmgzAJAjJihiO3T48/edit) to discuss development, issues, use cases, etc with maintainers and other contributors.

- Add a policy to the [Gatekeeper policy library](https://www.github.com/open-policy-agent/gatekeeper-library).

## Contributing Process

Please follow these 3 steps for contributions:

1. Commit changes to a git branch in your fork, making sure to sign-off those changes for the [Developer Certificate of Origin](#developer-certification-of-origin-dco).
1. Create a GitHub Pull Request for your change, following the instructions in the pull request template and use [semantic PR title](https://github.com/zeke/semantic-pull-requests)
1. Perform a [Pull Request Review](#pull-request-review-process) with the project maintainers on the pull request.

### Developer Certification of Origin (DCO)

This project requires contributors to sign a DCO (Developer Certificate of Origin) to ensure that the project has the proper rights to use your code. 

The DCO is an attestation attached to every commit made by every developer. In the commit message of the contribution, the developer simply adds a Signed-off-by statement and thereby agrees to the DCO, which you can find at <http://developercertificate.org/>.

#### DCO Sign-Off via the command line

Configure your local git to sign off your username and email address that is associated with your GitHub user account.

```sh
$ git config --global user.name "John Doe" 
$ git config --global user.email johndoe@example.com 
```

Then, for every commit, add a signoff statement via the `-s` flag. 

```sh
$ git commit -s -m "This is my commit message"
```

If you forget to add the sign-off you can also amend a previous commit with the sign-off by running `git commit --amend -s`. If you've pushed your changes to GitHub already you'll need to force push your branch with `git push -f`.

### Pull Request Review Process

Please take a look at [this article](https://help.github.com/articles/about-pull-requests/) if you're not familiar with GitHub Pull Requests.

Once you open a pull request, project maintainers will review your changes and respond to your pull request with any feedback they might have.

#### Pull Request Test Requirements

For code updates, to ensure high quality commits, we require that all pull requests to this project meet these specifications:

1. **Tests:** We require all the code in Gatekeeper to have at least unit test coverage.
2. **Green CI Tests:** We require these test runs to succeed on every pull request before being merged.

## Contributing to Docs

If you want to contribute to docs, Gatekeeper auto-generates versioned docs. If you have any doc changes for a particular version, please update in [website/docs](https://github.com/open-policy-agent/gatekeeper/tree/master/website/docs) as well as in [website/versioned_docs/version-vx.y.z](https://github.com/open-policy-agent/gatekeeper/tree/master/website/versioned_docs) directory. If the change is for next release, please update in [website/docs](https://github.com/open-policy-agent/gatekeeper/tree/master/website/docs), then the change will be part of next versioned doc when we do a new release.

## Contributing to Helm Chart

If you want to contribute to Helm chart, Gatekeeper auto-generates versioned Helm charts from static manifests. If you have any changes in [charts](https://github.com/open-policy-agent/gatekeeper/tree/master/charts) directory, they will get clobbered when we do a new release. The generator code lives under [cmd/build/helmify](https://github.com/open-policy-agent/gatekeeper/tree/master/cmd/build/helmify). To make modifications to this template, please edit `kustomization.yaml`, `kustomize-for-helm.yaml` and `replacements.go` under that directory and then run `make manifests`. Your changes will show up in the [manifest_staging](https://github.com/open-policy-agent/gatekeeper/tree/master/manifest_staging) directory and will be promoted to the root charts directory the next time a Gatekeeper release is cut.

## Contributing to Code

If you want to contribute code, check out the [Developers](developers.md) guide to get started.