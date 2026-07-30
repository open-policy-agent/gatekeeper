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

#### Optional benchmarking of changes

To ensure that any changes made to the code do not negatively impact its performance, you can run benchmark tests on the changes included in a pull request. To do this, simply comment `/benchmark` on the pull request. This will trigger the benchmark tests to run on both the current HEAD and the code changes in the pull request. The results of the benchmark tests will then be commented on the pull request using [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat).

If you are introducing a new feature, doing a big refactor, or fixing a critical bug, it's especially important to run benchmark tests on the changes you are trying to merge. This will help ensure that the changes do not negatively impact the performance of the code and that it continues to function as expected.

Below is the sample output that will be commented on the pull request:

```
name                                              old time/op  new time/op  delta
pkg:github.com/open-policy-agent/gatekeeper/v3/pkg/mutation goos:linux goarch:amd64
System_Mutate                                     1.48µs ± 5%  1.50µs ± 4%    ~     (p=0.468 n=10+10)
pkg:github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/assign goos:linux goarch:amd64
AssignMutator_Mutate/always_mutate_1-depth         235ns ± 5%   234ns ± 5%    ~     (p=0.726 n=10+10)
AssignMutator_Mutate/always_mutate_2-depth         287ns ± 6%   279ns ± 5%    ~     (p=0.190 n=10+10)
AssignMutator_Mutate/always_mutate_5-depth         420ns ± 2%   416ns ± 3%    ~     (p=0.297 n=9+9)
AssignMutator_Mutate/always_mutate_10-depth        556ns ± 4%   570ns ± 6%    ~     (p=0.123 n=10+10)
AssignMutator_Mutate/always_mutate_20-depth        977ns ± 3%   992ns ± 2%    ~     (p=0.063 n=10+10)
AssignMutator_Mutate/never_mutate_1-depth          196ns ± 4%   197ns ± 6%    ~     (p=0.724 n=10+10)
AssignMutator_Mutate/never_mutate_2-depth          221ns ± 4%   222ns ± 4%    ~     (p=0.971 n=10+10)
AssignMutator_Mutate/never_mutate_5-depth          294ns ± 4%   296ns ± 4%    ~     (p=0.436 n=10+10)
AssignMutator_Mutate/never_mutate_10-depth         424ns ± 2%   425ns ± 3%    ~     (p=0.905 n=9+10)
AssignMutator_Mutate/never_mutate_20-depth         682ns ± 3%   680ns ± 5%    ~     (p=0.859 n=9+10)
pkg:github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/assignimage goos:linux goarch:amd64
AssignImageMutator_Mutate/always_mutate_1-depth    579ns ± 7%   573ns ± 3%    ~     (p=0.650 n=9+9)
AssignImageMutator_Mutate/always_mutate_2-depth    625ns ± 5%   627ns ± 2%    ~     (p=0.536 n=10+9)
AssignImageMutator_Mutate/always_mutate_5-depth    758ns ± 5%   768ns ± 6%    ~     (p=0.631 n=10+10)
AssignImageMutator_Mutate/always_mutate_10-depth  1.06µs ± 8%  1.08µs ± 5%    ~     (p=0.143 n=10+10)
AssignImageMutator_Mutate/always_mutate_20-depth  1.38µs ± 3%  1.42µs ± 3%  +2.80%  (p=0.003 n=9+10)
AssignImageMutator_Mutate/never_mutate_1-depth     237ns ± 3%   233ns ± 3%    ~     (p=0.107 n=10+9)
AssignImageMutator_Mutate/never_mutate_2-depth     266ns ± 4%   266ns ± 3%    ~     (p=1.000 n=10+10)
AssignImageMutator_Mutate/never_mutate_5-depth     336ns ± 6%   342ns ± 2%  +1.85%  (p=0.037 n=10+9)
AssignImageMutator_Mutate/never_mutate_10-depth    463ns ± 3%   479ns ± 5%  +3.53%  (p=0.013 n=9+10)
AssignImageMutator_Mutate/never_mutate_20-depth    727ns ± 3%   727ns ± 2%    ~     (p=0.897 n=10+8)
...
```

If a significantly positive increase in the delta occurs, it could suggest that the changes being implemented have a negative impact on the respective package. However, there might be cases where the delta may be higher even without significant changes. In such situations, it is advisable to rerun the benchmarks for more precise and accurate results.

## Contributing to Docs

If you want to contribute to docs, Gatekeeper auto-generates versioned docs. If you have any doc changes for a particular version, please update in [website/docs](https://github.com/open-policy-agent/gatekeeper/tree/master/website/docs) as well as in [website/versioned_docs/version-vx.y.z](https://github.com/open-policy-agent/gatekeeper/tree/master/website/versioned_docs) directory. If the change is for next release, please update in [website/docs](https://github.com/open-policy-agent/gatekeeper/tree/master/website/docs), then the change will be part of next versioned doc when we do a new release.

## Contributing to Helm Chart

If you want to contribute to Helm chart, Gatekeeper auto-generates versioned Helm charts from static manifests. If you have any changes in [charts](https://github.com/open-policy-agent/gatekeeper/tree/master/charts) directory, they will get clobbered when we do a new release. The generator code lives under [cmd/build/helmify](https://github.com/open-policy-agent/gatekeeper/tree/master/cmd/build/helmify). To make modifications to this template, please edit `kustomization.yaml`, `kustomize-for-helm.yaml` and `replacements.go` under that directory and then run `make manifests` to generate changes in the [manifest_staging](https://github.com/open-policy-agent/gatekeeper/tree/master/manifest_staging) directory. You should push all the modified files to your PR. Once it's merged, the changes will be promoted to the root charts directory the next time a Gatekeeper release is cut.

## Contributing to Code

If you want to contribute code, check out the [Developers](developers.md) guide to get started.

## Contributing Templates

If you'd like to contribute a Constraint Template to the [Gatekeeper Policy Library](https://open-policy-agent.github.io/gatekeeper-library/website/), you can find documentation on how to do that [here in the library's README](https://github.com/open-policy-agent/gatekeeper-library?tab=readme-ov-file#how-to-contribute-to-the-library).