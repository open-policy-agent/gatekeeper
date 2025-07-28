---
id: mutation-background
title: Background Information on Mutation
---

Mutation webhooks in Kubernetes is a nuanced concept with many gotchas. This
page explores some of the background of mutation webhooks in Kubernetes, their
operational and syntactical implications, and how Gatekeeper is trying to provide
value on top of the basic Kubernetes webhook ecosystem.

# Mutation Chaining

A key difference between mutating webhooks and validating webhooks are that
mutating webhooks are called in series, whereas validating webhooks are called in parallel.

This makes sense, since validating webhooks can only approve or deny (or warn) for a given
input and have no other side effects. This means that the result of one validating webhook
cannot impact the result of any other validating webhook, and it's trivial to aggregate
all of the validation responses as they come in: reject if at least one deny comes in, return
all warnings and denies that are encountered back to the user.

Mutation, however, changes what the input resource looks like. This means that the output
of one mutating webhook can have an effect on the output of another mutating webhook.
For example, if one mutating webhook adds a sidecar container, and another webhook sets
`imagePullPolicy` to `Always`, then the new sidecar container means that this second webhook
has one more container to mutate.

The biggest practical issue with this call-in-sequence behavior is latency. Validation webhooks
(which are called in parallel), have a latency equivalent to the slowest-responding webhook.
Mutation webhooks have a total latency that is the sum of all mutating webhooks to be called. This
makes mutation much more latency-sensitive.

This can be particularly harmful for something like external data, where a webhook reaches out to
a secondary service to gather necessary information. This extra hop can be extra expensive,
especially if these external calls are not minimized. Gatekeeper translates external data
references scattered across multiple mutators into a single batched call per external data provider,
and calls each provider in parallel, minimizing latency.

# Mutation Recursion

Not only are mutators chained, but they recurse as well. This is not only due to Kubernetes'
[reinvocation policy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#reinvocation-policy),
but also due to the nature of the Kubernetes control plane itself, since controllers may modify resources periodically.
Whether because of the reinvocation policy, or because of control plane behavior, mutators are likely to
operate on their own output. This has some operational risk. Consider a mutating webhook that prepends a hostname to a docker
image reference (e.g. prepend `gcr.io/`), if written naievly, each successive mutation would add another prefix, leading to results
like `gcr.io/gcr.io/gcr.io/my-favorite-image:latest`. Because of this, Kubernetes requires mutation webhooks to be
[idempotent](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#idempotence).

This is a good idea, but there is one problem: webhooks that are idempotent in isolation may not be idempotent as a group.
Let's take the above mutator and make it idempotent. We'll give it the following behavior: "if an image reference does
not start with `gcr.io/`, prepend `gcr.io/`". This makes the webhook idempotent, for sure. But, what if there is another
team working on the cluster, and they want their own image mutation rule: "if an image reference for the `billing`
namespace does not start with `billing.company.com/`, prepend `billing.company.com/`". Each of these webhooks would
be idempotent in isolation, but when chained together you'll see results like
`billing.company.com/gcr.io/billing.company.com/gcr.io/my-favorite-image:latest`.

At small scales, with small teams, it's relatively easy to ensure that mutations don't interfere with each other,
but at larger scales, or when multiple non-communicating parties have their own rules that they want to set, it
can be hard, or impossible to maintain this requirement of "global idempotence".

Gatekeeper attempts to make this easier by designing mutation in such a way that "global idempotence" is an
emergent property of all mutators, no matter how they are configured. Here is a [proof](https://docs.google.com/document/d/1mCHHhBABzUwP8FtUuEf_B-FX-HHgh_k4bwZcGUYm7Sw/edit#heading=h.j5thjfnqybpn), where we attempt to show that our language
for expressing mutation always converges on a stable result.

# Summary

By using Gatekeeper for mutation, it is possible to reduce the number of mutation webhooks, which should improve latency
considerations. It should also help prevent decoupled management of mutation policies from violating the Kubernetes API
server's requirement of idempotence.