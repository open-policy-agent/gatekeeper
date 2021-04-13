# Release Process

## Overview

The release process consists of three phases: versioning, building, and publishing.

Versioning involves maintaining the following files:
- **Makefile** - the Makefile contains a VERSION variable that defines the version of the project.
- **manager.yaml** - the controller-manager deployment yaml contains the latest release tag image of the project.
- **gatekeeper.yaml** - the gatekeeper.yaml contains all gatekeeper resources to be deployed to a cluster including the latest release tag image of the project.

The steps below explain how to update these files. In addition, the repository should be tagged with the semantic version identifying the release.

Building involves obtaining a copy of the repository and triggering a build as part of the GitHub Actions CI pipeline.

Publishing involves creating a release tag and creating a new *Release* on GitHub.

## Cherry picking

There is an optional script for cherry picking PRs that should make the process easier.

Prerequisites:
- `hub` binary is installed. If not, `hub` can be installed by `go get github.com/github/hub`.
- Set GitHub user name with `export GITHUB_USER=<your GitHub username>`
- Set fork remote with `export FORK_REMOTE=<your fork remote name, by default it is "origin">`
- Set upstream remote with `export UPSTREAM_REMOTE=<upstream remote name, by default it is "upstream">`

Usage: `./third_party/k8s.io/kubernetes/hack/cherry_pick_pull.sh upstream/release-3.1 123`
For example, this will cherry pick PR #123 into `upstream/release-3.1` branch and will create a PR for you.

You can also combine multiple PRs by separating them with spaces (`./third_party/k8s.io/kubernetes/hack/cherry_pick_pull.sh upstream/release-3.1 123 456`)

If you want to run the script with dry run mode, set `DRY_RUN` to `true`.

Cherry pick script is copied over from https://github.com/kubernetes/kubernetes/blob/master/hack/cherry_pick_pull.sh. For more information, see https://github.com/kubernetes/community/blob/master/contributors/devel/sig-release/cherry-picks.md

## Versioning

1. Obtain a copy of the repository.

	```
	git clone git@github.com:open-policy-agent/gatekeeper.git
	```

1. If this is a patch release for a release branch, check out applicable branch, such as `release-3.1`. If not, branch should be `master`

1. Execute the release-patch target to generate patch. Give the semantic version of the release:

	```
	make release-manifest NEWVERSION=v3.0.4-beta.x
	```

1. Promote staging manifest to release.

	```
	make promote-staging-manifest
	```

1. Preview the changes:

	```
	git diff
	```

## Building and releasing

1. Commit the changes and push to remote repository to create a pull request.

	```
	git checkout -b release-<NEW VERSION>
	git commit -a -s -m "Prepare <NEW VERSION> release"
	git push <YOUR FORK>
	```

2. Once the PR is merged to `master` or `release` branch (`<BRANCH NAME>` below), tag that commit with release version and push tags to remote repository.

	```
	git checkout <BRANCH NAME>
	git pull origin <BRANCH NAME>
	git tag -a <NEW VERSION> -m '<NEW VERSION>'
	git push origin <NEW VERSION>
	```

1. Pushing the release tag will trigger GitHub Actions to trigger `tagged-release` job.
This will build the `openpolicyagent/gatekeeper` image automatically, Then publish the new release image tag and the `latest` image tag to the `openpolicyagent/gatekeeper` repository. Finally, verify step will run e2e tests to verify the new released tag.

## Publishing

1. GitHub Action will create a new release, review and edit it at https://github.com/open-policy-agent/gatekeeper/releases
