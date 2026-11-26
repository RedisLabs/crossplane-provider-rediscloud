# Pro database uncertain-delete fence

**TLDR:** The hosted delayed-delete fixture observed a second DELETE after an accepted-then-503 fake response. A durable pre-dispatch fence can prevent automatic replay, but must also prevent finalizer removal when the outcome is unknown.

**Date:** 2026-11-26

**Status:** Experimental; not enabled

**Ticket:** [RED-225344](https://redislabs.atlassian.net/browse/RED-225344)

## Evidence and scope

In the #108 hosted fixture, the checksum-pinned Terraform provider v2.20.0 was exercised through the actual Crossplane managed reconciler and Upjet against a loopback fake. After the fake accepted a DELETE, returned 503 and kept the child visible, a subsequent reconcile produced a second DELETE. This reproduces one injected failure, not live Cloud behavior. In Upjet v2.5.1 `Workspace.Refresh` clears an ended operation and refreshes state; the managed reconciler invokes Delete again while the resource is observed to exist. The Terraform provider's Pro database delete returns an SDK DELETE error without an enduring uncertain-operation journal.

## Proposed protocol

For a deleting Pro database CR, obtain an authoritative API-server read and require the expected CR UID, deletion timestamp, provider reference, external identity and subscription binding. Persist a versioned, unique intent using a resourceVersion-guarded update before invoking Upjet's Delete. Only the updater that gets a successful CAS response may initiate the first dispatch. A conflict, timeout or ambiguous update response never authorizes a write; a pre-existing intent forbids all later automatic DELETEs, including after process restart or leader change. An operation token must be bound to the exact UID and immutable identity, not the CR name alone. Separate policy is required for other resource kinds.

Before finalization the controller must account for uncertainty. A missing callback, failed callback, or restart before durable proof is unknown even if the current Cloud read says 404; a single absent read is not proof that an asynchronous operation has reached its authoritative terminal state. Hold the finalizer, report an actionable sanitized condition, and permit authenticated read-only reconciliation. Do not retry based solely on a timer, status, or active/absent read. Any successful callback must be correlated to the exact dispatched UID, identity and token, and its semantics independently verified before it can establish completion. Loss of its update remains unknown. Recovery must be a separately authorized, auditable operation that verifies a durable task outcome or explicit operator evidence; it may not clear a marker just because time elapsed. An operational recovery design is not yet established.

## Boundary and tradeoffs

A marker on a CR can be modified or removed by identities with CR write access. It is not a durable security boundary against such actors, deletion/recreation, privileged administrators, other API credentials or external tools. The experimental CAS primitive alone does not validate the external name against the declared subscription, establish an authorized provider account, or protect the managed finalizer. Its result never authorizes a Cloud write. Deployment requires admission/authorization that protects marker, finalizer and identity fields, documented trusted actors, and a route to resolve crash-before-dispatch without unsafe replay. An old controller, already in-flight operation, another CR or alternate Cloud writer is not fenced by this primitive; a safe rollout must drain older writers and enforce the boundary across every credential-bearing write path. Before any production enabling, establish these protections and Cloud task/404 semantics. The conservative approach may hold a resource indefinitely, including after a successfully completed delete; never deploy it as a hidden permanent finalizer. Existing writers remain gated.

## Validation and rollout

Keep the experiment disabled and use hosted CI only. Exercise actual reconciler, pinned Terraform/provider, a fake Cloud, and a real disposable API server. Race two independent clients on CAS; simulate failed/ambiguous persistence, crash before dispatch, accepted-503 with visible and absent child, process restart with same persisted UID, stale callback/name replacement, conflicting identity and tampering. Require no extra DELETE, no premature finalization and no claim of a Cloud terminal state from simulated evidence. Test the successful completion transition only after its authoritative evidence and marker protection are specified. Compare generator output to ensure any controller hookup survives regeneration. This is not a deployed mitigation or live qualification.

## Decision assessment

A durable fence is a trust-boundary decision; an ADR is required when the storage, authorization and completion semantics are chosen. No business decision is made. The current document is a proposed experiment, not an accepted architecture.
