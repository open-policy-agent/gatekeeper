# sigs.k8s.io/controller-runtime

Forked from sigs.k8s.io/controller-runtime@f7a3dc6a7650289b6ca7afca12b97819329b0d06 (v0.7.0).

This fork adds the ability to dynamically
remove informers from the informer cache. Additionally, non-blocking APIs were added to fetch informers
without waiting for cache sync. `pkg/cache` was renamed to `pkg/dynamiccache` for clarity.

The original code can be found at https://github.com/kubernetes-sigs/controller-runtime/tree/v0.7.0/pkg/cache.

