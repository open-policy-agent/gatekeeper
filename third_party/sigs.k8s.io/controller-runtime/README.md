# sigs.k8s.io/controller-runtime

Forked from sigs.k8s.io/controller-runtime@6d2d247cb6f3a26e6b5597c2aa4a943a90c988bb (v0.14.1).

This fork adds the ability to dynamically
remove informers from the informer cache. Additionally, non-blocking APIs were added to fetch informers
without waiting for cache sync. `pkg/cache` was renamed to `pkg/dynamiccache` for clarity.

The original code can be found at https://github.com/kubernetes-sigs/controller-runtime/tree/v0.8.2/pkg/cache.

