# sigs.k8s.io/controller-runtime

Forked from sigs.k8s.io/controller-runtime@1c83ff6f06bc764c95dd69b0f743740c064c4bf6 (v0.6.0).

This fork adds the ability to dynamically
remove informers from the informer cache. Additionally, non-blocking APIs were added to fetch informers
without waiting for cache sync. `pkg/cache` was renamed to `pkg/dynamiccache` for clarity. 

The original code can be found at https://github.com/kubernetes-sigs/controller-runtime/tree/v0.6.0/pkg/cache.

