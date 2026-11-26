# PSC endpoint opt-in delete hold

**TLDR:** Default-off per-CR guard records intent before invoking Terraform destroy and holds after any outcome. This is a draft, not deployed or a shared-account fence.

**Date:** 2026-11-26  
**Ticket:** [RED-225344](https://redislabs.atlassian.net/browse/RED-225344)

## Problem and design

[#124](https://github.com/RedisLabs/crossplane-provider-rediscloud/pull/124) demonstrates a repeat fake DELETE after an accepted-then-503 response while the endpoint remains listed. The default-off `--enable-psc-endpoint-delete-hold` persists intent with API-server resourceVersion CAS before the managed connector invokes destroy, requires matching CR UID, finalizer, full spec, provider reference and `subscription/service/endpoint` identity, then rechecks. The ID components use the same `strconv.Atoi` grammar as provider v2.20.0. Declared subscription and integral exactly representable service must match. Unverifiable identity or persistence holds; the guard does not reinterpret IDs. Existing intent blocks guard-aware binaries even after the flag is disabled. A fixed sanitized message requests preservation and administrator help; no token or API response is printed.

All outcomes remain held including nil before dispatch and success. This applies even when Terraform would not issue a DELETE: noninitialized endpoints normally wait for customer GCP resources to be removed, so enabling the flag substantially changes availability and requires manual adjudication for ordinary deletion. Do not infer any DELETE from a marker. No automatic clearing or resolution is offered. Orphan/no-Delete policies may bypass the connector and finalizer, not prove Cloud cleanup.

## Trust and recovery

The CR annotation is not an authorization boundary. This requires trusted CR metadata/spec/finalizer writers, protected ProviderConfig and Secret, an independently confirmed account and a fully drained rollout including older and differently configured controllers. ProviderConfig name does not establish account or credential version. Concurrent other CRs, Terraform/direct API writers, changes after reread and in-flight older binaries remain unfenced. Never enable as complete replay protection. Resolve uncertainty with authorized expert review of exact authenticated account, subscription, service and complete endpoint collection plus authoritative task state. Missing parent, incomplete data, loss of task ID or task processing-completed is not proof of terminal deletion. GCP-side resources and their deletion are independently unverified. Do not remove intent simply to retry.

## Verification and rollout

Hosted fake-only tests must cover real managed reconciliation and API-server CAS, default-off lifecycle, initialized accepted-503, noninitialized wait, restart and concurrent reservations, stale and replaced identities, flag-off marker, orphan behavior and sanitized diagnostics. Independent security review and exact-head CI are required. No live Cloud test or deployment is authorized here. Disable the flag to prevent new reservations; this does not clear prior holds. Rolling back to an old binary may bypass them. Existing [Pro design](pro-delete-conservative-hold.md) documents the broader unresolved trust model; no business decision is made.
## Hosted integration coverage (pending)

The credential-free hosted fixture is designed to run the actual managed reconciler and pinned
Terraform provider against a loopback Cloud model and a disposable Kubernetes
API server. Named cases cover the flag-off normal path, opt-in successful delete,
and opt-in accepted-then-503 with a still-visible initialized endpoint. Each
held case reconstructs the reconciler with the feature disabled and requires
the original API-server UID, deletion timestamp, managed finalizer and intent,
no opened upstream workspace and one fake DELETE. This is an in-process
reconstruction, not a process or pod restart. It does not establish Cloud task
semantics, live GCP cleanup, trusted markers, cross-writer exclusion or recovery.
Non-initialized endpoints, Orphan policies and concurrent actual API reservations
need separate hosted coverage.

The additional pending `noninitialized_hold` case sets the fake endpoint to
active, requires an in-flight provider destroy and a subsequent fake collection
read, checks no DELETE was sent, and only then removes the endpoint in the fake
model. The case expects the marker and managed finalizer to remain after the
operation completes and the connector is reconstructed. The model does not
contact GCP and cannot prove real external cleanup or Cloud task behavior.

The pending policy-boundary fixture starts with the persisted PSC intent and
managed finalizer, then exercises Orphan and a management policy without Delete
using a real API server and managed reconciler. The synthetic inner connector
counts connections and deletions. Observing that the CR disappears without an
inner call would demonstrate abandonment of management, not Cloud cleanup.

The pending PSC reservation fixture uses the real disposable API server and
two simultaneous stale snapshots, synchronized before their updates. It checks
that one guarded synthetic Delete is forwarded, and separately models inner
error, pre-write rejection and an API update committed before its response is
lost. A committed marker blocks replay. The faults and downstream connector are
synthetic; this is not proof of a real API outage, Cloud dispatch, durable
multi-actor authority or safe recovery.

The pending hosted process fixture keeps one disposable API server, workspace,
and dummy-authenticated loopback PSC model while starting distinct reconciler
processes. In the abrupt case, the fake accepts the exact initialized endpoint
DELETE and withholds the HTTP response; the supervisor SIGKILLs the worker's
process group and confirms it exited before releasing that response. A second
process, with the flag disabled, must retain the same UID, full identity marker
and managed finalizer without a second fake DELETE. It is a process test, not a
Kubernetes pod-restart, production network, Cloud cleanup, or recovery proof.
