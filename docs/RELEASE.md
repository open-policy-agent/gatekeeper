# Release Process

## Overview

The release process consists of three phases: versioning, building, and publishing.

Versioning involves maintaining the following files:
- **Makefile** - the Makefile contains a VERSION variable that defines the version of the project.
- **manager.yaml** - the controller-manager deployment yaml contains the latest release tag image of the project.
- **gatekeeper.yaml** - the gatekeeper.yaml contains all gatekeeper resources to be deployed to a cluster including the latest release tag image of the project.

The steps below explain how to update these files. In addition, the repository should be tagged with the semantic version identifying the release.

Building involves obtaining a copy of the repository and triggering a build as part of the Travis-CI pipeline.

Publishing involves creating a release tag and creating a new *Release* on GitHub.

## Versioning

1. Obtain a copy of the repository.

	```
	git clone git@github.com:open-policy-agent/gatekeeper.git
	```

1. Execute the release-patch target to generate boilerplate patch. Give the semantic version of the release:

	```
	make release NEWVERSION=v3.0.4-beta.x
	```
1. Preview the changes:

	```
	git diff
	```

## Building

1. Commit the changes and push to remote repository.

	```
	git commit -a -s -m "Prepare <version> release"
    git commit --amend --signoff
	git push origin master
	```

1. Tag repository with release version and push tags to remote repository.

	```
	git tag -a <NEWVERSION> -m '<NEWVERSION>'
	git push origin <NEWVERSION>
	```

1. Pushing the release tag will trigger the Travis-CI pipeline to run `make travis-dev-release`. 
This will build the `quay.io/open-policy-agent/gatekeeper` image automatically, Then publish the new release image tag and the `latest` image tag 
to the `quay.io/open-policy-agent/gatekeeper` repository.

## Publishing

1. Open browser and go to https://github.com/open-policy-agent/gatekeeper/releases

1. Create a new release from the new tag version.
	- Release title: <NEWVERSION>
    - Update release message with Features, Bug Fixes, Breaking Changes, etc.
	- Click `Publish release` will automatically include the binaries from the tag.
