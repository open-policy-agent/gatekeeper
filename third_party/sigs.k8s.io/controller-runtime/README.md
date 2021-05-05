# sigs.k8s.io/controller-runtime

Forked from sigs.k8s.io/controller-runtime@a8c19c49e49cfba2aa486ff322cbe5222d6da533 (v0.8.2).

This fork adds the ability to dynamically
remove informers from the informer cache. Additionally, non-blocking APIs were added to fetch informers
without waiting for cache sync. `pkg/cache` was renamed to `pkg/dynamiccache` for clarity.

The original code can be found at https://github.com/kubernetes-sigs/controller-runtime/tree/v0.8.2/pkg/cache.

