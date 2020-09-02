# Release Management

## Overview

This document describes Gatekeeper project release management, which includes release versioning, supported releases, and supported upgrades. 

## Legend

- **X.Y.Z** refers to the version (git tag) of Gatekeeper that is released. This is the version of the Gatekeeper image and the Chart version.
- **Breaking changes** refer to schema changes, flag changes, and behavior changes of Gatekeeper that may require a clean install during upgrade and it may introduce changes that could break backward compatibility. 
- **Milestone** should be designed to include feature sets to accommodate 2 months release cycles including test gates. GitHub milestones are used by maintainers to manage each release. PRs and Issues for each release should be created as part of a corresponding milestone.
- **Patch releases** refer to applicable fixes, including security fixes, may be backported to support releases, depending on severity and feasibility.
- **Test gates** should include soak tests and upgrade tests from the last minor version.

## Release Versioning

All releases will be of the form _vX.Y.Z_ where X is the major version, Y is the minor version and Z is the patch version. This project strictly follows semantic versioning.

The rest of the doc will cover the release process for the following kinds of releases:

**Major Releases**

No plan to move to 4.0.0 unless there is a major design change like an incompatible API change in the project 

**Minor Releases**

- X.Y.0-alpha.W, W >= 0 (Branch : master)
    - Released as needed before we cut a beta X.Y release
    - Alpha release, cut from master branch
- X.Y.0-beta.W, W >= 0 (Branch : master)
    - Released as needed before we cut a stable X.Y release
    - More stable than the alpha release to signal users to test things out
    - Beta release, cut from master branch
- X.Y.0-rc.W, W >= 0 (Branch : master)
    - Released as needed before we cut a stable X.Y release
    - soak for ~ 2 weeks before cutting a stable release
    - Release candidate release, cut from master branch
- X.Y.0 (Branch: master)
    - Released every ~ 2 months
    - Stable release, cut from master when X.Y milestone is complete 

**Patch Releases**

- Patch Releases X.Y.Z, Z > 0 (Branch: release-X.Y, only cut when a patch is needed)
    - No breaking changes
    - Applicable fixes, including security fixes, may be cherry-picked from master into the latest supported minor release-X.Y branches. 
    - Patch release, cut from a release-X.Y branch

## Supported Releases

Applicable fixes, including security fixes, may be cherry-picked into the release branch, depending on severity and feasibility. Patch releases are cut from that branch as needed.

We expect users to stay reasonably up-to-date with the versions of Gatekeeper they use in production, but understand that it may take time to upgrade. We expect users to be running approximately the latest patch release of a given minor release and encourage users to upgrade as soon as possible. 

We expect to "support" n (current) and n-1 major.minor releases. "Support" means we expect users to be running that version in production. For example, when v3.3.0 comes out, v3.1.x will no longer be supported for patches and we encourage users to upgrade to a supported version as soon as possible.

## Supported Kubernetes Versions

Gatekeeper is assumed to be compatible with n-3 versions of the latest stable Kubernetes release per Kubernetes Supported Versions policy (this may change to n-4 once LTS is available). If you choose to use Gatekeeper with a version of Kubernetes that it does not support, you are using it at your own risk.

## Upgrades

Users should be able to run both X.Y and X.Y + 1 simultaneously in order to support gradual rollouts.

## Acknowledgement

This document builds on the ideas and implementations of release processes from projects like Kubernetes and Helm. 
