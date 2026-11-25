# Shared connector acceptance scenarios

**Status:** Proposed contract prototype; no live adapters are implemented.

**Date:** 2026-11-25. **Tracking:** RED-225344.

## TL;DR

Define a common ordered set of lifecycle claims and an adapter/result interface for Terraform, Pulumi and Crossplane. Keep provider-specific execution and recovery behind adapters. This initial prototype validates structure with fake result producers only; it is not a live runner and cannot authenticate evidence or authorize a Cloud mutation. Existing provider tests are not yet wired to it.

## Problem and scope

The repositories exercise similar resources through different tools and report different slices of the lifecycle. Terraform's `TestAccResourceRedisCloudEssentialsDatabase_CRUDI` uses `resource.Test` steps, provider state, import verification and `CheckDestroy`. Crossplane's `strict_essentials` uses managed resources, authenticated Cloud reads, a durable intent and controller-driven cleanup. Pulumi's `run_acceptance` uses a stack and Cloud reads. These implementations do not currently expose the same adapter interface and their existing evidence must not be retroactively treated as proof of every shared step.

The first two declared cases are Essentials database create/read/delete and its eviction-policy update. Import, rename, Pro, Active-Active, ACL and networking cases are not yet modeled. An adapter that cannot execute a case must return `unsupported` or `blocked`, with a reason in its surrounding report; missing coverage is not a pass. Separate cases should be added only with agreed assertions and supported recovery paths.

## Contract and implementation

[`acceptance/scenarios.json`](../../acceptance/scenarios.json) names ordered steps. [`acceptance/contract.py`](../../acceptance/contract.py) defines a provider adapter boundary and validates claimed evidence. The loader rejects malformed cases, duplicate steps and missing admission/cleanup boundaries. The classifier refuses a pass claim if steps are absent or reordered, cleanup is unresolved, direct Cloud recovery was required, or provenance is missing. A mode-qualified claim still **requires independent verification**; strings or adapter claims do not establish that an API read, lock, workflow, revision or cleanup actually occurred. A simulated result never counts as live coverage.

Step semantics for both cases:

1. Admission must bind a verified account to an enforced serialization mechanism and a durable run-owned intent. Authenticated, complete inventory establishes the pre-run baseline. Missing lock, incomplete listing or unknown account blocks writes.
2. Create parent and child through the connector, capture immutable account/parent/child identity, and use authenticated exact Cloud reads to show both active. A timeout or ambiguous create is not success.
3. The update case changes the agreed eviction policy and reads back the new policy on the same exact parent and child IDs. It does not establish rename, import, no-op or drift correction.
4. Request database deletion through the connector. Authenticated exact reads must show the database absent while the same subscription remains present. Then delete the subscription through the connector and prove its exact absence. Complete authenticated run-owned inventory and task outcomes establish cleanup. A direct Cloud deletion preserves the original failed qualification even if recovery succeeds.

Each real adapter must independently implement and prove these semantics, not simply return step names. Provider-specific handling includes Terraform's state and test framework, Pulumi stack state, and Crossplane CR identity, conditions and finalizers. An adapter must retain a run-owned durable journal, use scoped recovery, preserve active cleanup after cancellation or runner loss, and fail closed when identity, pagination, task state or lock ownership is uncertain. This prototype neither supplies those facilities nor replaces the proposed [shared-account serialization design](https://github.com/RedisLabs/crossplane-provider-rediscloud/pull/50); that design is not assumed deployed.

## Provenance and coverage

For a pass claim require provider, case, explicit live/simulated mode, a full 40-character source commit or `sha256:` artifact digest, HTTPS run and evidence links, an ordered per-step passed outcome with its own HTTPS evidence link, and clean teardown. Live claims reject reserved `.invalid`, `.test`, `.example` and `.localhost` fixture hosts and all-zero commit or digest placeholders. These are untrusted claims: syntactically valid URLs, outcomes and commit strings do not authenticate the external records. A reporting integration must authenticate those references, establish that the artifact came from the claimed source and inspect sanitized Cloud/controller evidence before marking any live case verified. Failed and blocked runs retain their cleanup status and residual-resource evidence outside the compact classifier. Do not expose credentials or account/resource identifiers in public results. This repository is a provisional home for the schema; a neutral shared location, versioning and ownership must be agreed before other repositories pin it.

## Validation and rollout

CI runs `python3 -m unittest discover -s acceptance -p 'test_*.py'` without credentials or Cloud. Tests use fake result producers and check ordering, provenance, invalid cleanup, simulated versus live labeling and explicit unsupported outcomes. These tests verify the contract consumer only, not any provider lifecycle or durable lock.

Next implement one adapter at a time in its owning repository; use real harness tests against local fakes first, validate independent evidence collection, then perform a single coordinated live case only after serialization and recovery are established. Roll back by leaving the contract unconsumed; never disable an active cleanup or release an unresolved lock. Design ownership, neutral hosting, artifact verification, account allocation and safe recovery after uncertain asynchronous operations remain open. No ADR is accepted by this prototype; no business decision is made.

The Crossplane loopback adapter invokes `TestEssentialsRefreshAndPlan` through the shared update case. Hosted CI checks the Terraform and provider archives against pinned SHA-256 values before running the fixture. It exercises an actual managed reconciler and Terraform provider against an in-memory Kubernetes client and a fake loopback API, including no-op and eviction drift correction. It returns `blocked` even on fixture success: it does not implement account admission, parent/child creation or deletion, or full cleanup. Missing dependencies, a skipped test or failed test cannot count as a pass. The fixture result is simulated and does not run a live provider lifecycle.

An additional credential-free `TestEssentialsManagedCreateLoopback` increment runs an actual Upjet managed reconciler and Terraform provider to create and delete a fake Essentials database under a pre-existing fake parent. Authenticated fake reads check both active IDs before deletion, then check child 404 while parent remains 200; the fake Kubernetes client must also observe the managed object disappear. The test rejects unexpected fake requests, uses only loopback and dummy credentials, and CI uses the same pinned archives. It is separate from the shared-case adapter, and it does not prove managed subscription creation/deletion, full account inventory, account admission, Kubernetes API server behavior, or live Cloud behavior. Those remain open.
