# Pro delete fence test-only integration experiment

**TLDR:** Exercise an unwired test wrapper around the actual managed reconciler, Upjet workspace and pinned Terraform provider against envtest and fake Cloud. This is not a product mitigation.

**Date:** 2026-11-26

**Status:** Experimental; test-only

**Ticket:** [RED-225344](https://redislabs.atlassian.net/browse/RED-225344)

## Purpose and behavior

The #108 fixture reproduced a second DELETE after an accepted-then-503 fake response. A test-only wrapper reserves the experimental API-server intent before Upjet Delete. It holds uncertain intents across in-process reconciler/store reconstruction even if the fake child is absent. For the successful fixture only, an exact UID/token callback records a test completion marker; only then may observation proceed and the fixture requires a new authenticated exact fake 404 before finalization. The callback and hard-coded fake identity are test instrumentation, not evidence of Cloud task semantics.

The wrapper rereads and checks marker, UID, fixture identity, spec, deletion timestamp and finalizer before dispatch, and binds callbacks per object. A read-before-dispatch does not close the subsequent race. The test marker is writable on the CR and can be forged or removed. Other CRs, stale controller versions, in-flight operations, alternate providers and Cloud credentials are not fenced. Failure, lost callback, crash-before-dispatch and stale replacement remain held. There is no automatic recovery and no production setup, feature gate or release path.

## Validation and limitations

Hosted CI only: run real API-server/etcd with actual managed reconciler and pinned Terraform/provider against loopback fake Cloud. Exercise success, uncertain delayed 503 with child present then absent, recreated reconciler/store, crash-before-dispatch, stale callback after UID replacement and interleaved per-object callback bindings. Require no repeated DELETE or finalizer loss in uncertain scenarios, and a post-callback fake 404 before success. These are bounded in-process simulations, not pod restart, Cloud authorization, shared-account serialization, task proof, secure marker admission or live qualification. Use the separate [durable fence design](pro-delete-durable-fence.md) for unresolved architecture and rollout conditions; no deployment or customer release is proposed.
