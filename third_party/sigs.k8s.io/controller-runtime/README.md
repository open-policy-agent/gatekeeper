# sigs.k8s.io/controller-runtime

Forked from sigs.k8s.io/controller-runtime@0fcf28efebc9a977c954f00d40af966d6a4aeae3 (v0.5.0).

This fork adds the ability to dynamically
remove informers from the informer cache. Additionally, non-blocking APIs were added to fetch informers
without waiting for cache sync. `pkg/cache` was renamed to `pkg/dynamiccache` for clarity. 

The original code can be found at https://github.com/kubernetes-sigs/controller-runtime/tree/v0.5.0/pkg/cache.

