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

## Versioning

1. Obtain a copy of the repository.

	```
	git clone git@github.com:open-policy-agent/gatekeeper.git
	```

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

1. Once the PR is merged to master, tag master with release version and push tags to remote repository.

	```
	git checkout master
	git pull origin master
	git tag -a <NEW VERSION> -m '<NEW VERSION>'
	git push origin <NEW VERSION>
	```

1. Pushing the release tag will trigger GitHub Actions to trigger `tagged-release` job.
This will build the `openpolicyagent/gatekeeper` image automatically, Then publish the new release image tag and the `latest` image tag to the `openpolicyagent/gatekeeper` repository. Finally, verify step will run e2e tests to verify the new released tag.

## Publishing

1. GitHub Action will create a new release, review and edit it at https://github.com/open-policy-agent/gatekeeper/releases
