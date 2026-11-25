# Guarded Pro subscription lookup discovery

**Status:** Candidate; hosted checks required before QA dispatch  
**Date:** 2026-11-25  
**Tracking:** RED-225344

## TL;DR

Discover an existing active single-region Pro subscription from an authenticated
QA inventory, then exercise the proposed ProSubscription reconciler by exact ID.
Both requests are GET-only and scoped by an in-process transport guard. If the
account, inventory completeness or unique selector cannot be proven, stop before
the lookup; do not create resources or guess an ID.

## Behavior and boundaries

An opt-in Actions dispatch first builds a test binary without Cloud credentials,
tests denial of writes, other paths, redirects and ambiguous inventories, and
records its digest. The live job verifies that exact artifact, supplies the QA
credentials only to the binary, and verifies the configured HTTPS QA origin
against the existing independent origin allowlist.

The first allowed route is GET `/v1/subscriptions`. The response must contain a
positive account ID, a `subscriptions` list and explicit `total` equal to the
number of rows; duplicate JSON keys, pagination indicators, malformed identities,
duplicate IDs or names, missing deployment types and ambiguous candidates are
not evidence of a safe selector. Exactly one active single-region Pro candidate
must be present. Missing total or unsupported pagination blocks; this conservative
contract may block even when a usable subscription exists. The guard then permits
only the exact selected ID's GET endpoint. The actual ProSubscription reconciler
uses an in-memory Kubernetes client and must report the same ID, active status
and single-region type. Names, IDs, credentials and inventory bodies are never
logged. No Cloud resources are created, changed or deleted.

The guard covers this process's Cloud HTTP transport, not independent account
writers or global network activity. This is a read-only lookup, not lifecycle,
deployment, cleanup, isolation or shared-lock qualification. Any uncertain
response remains blocked. No fallback creates a selector. The source feature is
proposed in PR #71 and must not be described as merged or released.

## Rollout and rollback

Run only credential-free checks until the exact revision and request guard pass
in Actions; review the code and dispatch the read-only job only under the
authorized QA scope. Record the run, revision, guarded outcomes and limitations.
Rollback removes this candidate workflow and tests; do not cancel other cleanup.
ADR: existing lookup decision; no new architecture decision for a test harness.
BDR: none.
