# FAQ

## Finalizers

### How can I remove finalizers? Why are they hanging around?

If Gatekeeper is running, it should automatically clean up the finalizer. If it
isn't this is a misbehavior that should be investigated. Please file a bug with
as much data as you can gather. Including logs, memory usage and utilization, CPU usage and
utilization and any other information that may be helpful.

If Gatekeeper is not running:

- If it did not have a clean exit, Gatekeeper's garbage collection routine would
  have been unable to run. Reasons for an unclean exit are:
  - The service account was deleted before the Pod exited, blocking the GC
    process (this can happen if you delete the gatekeeer-system namespace
    before deleting the deployment or deleting the manifest all at
    once).
  - The container was sent a hard kill signal
  - The container had a panic

Finalizers can be removed manually via `kubectl edit` or `kubectl patch`


