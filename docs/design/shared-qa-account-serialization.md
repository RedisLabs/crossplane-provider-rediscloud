# Shared QA account writer serialization

**Status:** Proposed; no lock implementation or deployment is established.

**Date:** 2026-11-24.

**Tracking:** RED-225344.

## TL;DR

Operator and Crossplane have independently verified that their current QA credentials target the same Cloud account. Pulumi's account identity has not yet been compared. A durable account-scoped coordinator and enforcement of its admission at the Cloud credential boundary are prerequisites to safely resume unattended writes. Repository-local workflow concurrency and point-in-time audits do not enforce that requirement. Existing cleanup must retain its ability to finish and the coordinator must keep an interrupted account occupied until cleanup is verified.

## Problem and current entry points

These are workflow observations on default branches at the date above, not a complete inventory of every human or external integration that may possess account credentials. A run on an older ref may execute older workflow code. Running jobs retain their original code.

| Connector | Known writer entry points | Credential boundary | Current limitation |
| --- | --- | --- | --- |
| Operator | `redis-cloud-test.yml` schedule 06:15 UTC; manual normal, diagnostic, strict proof and recovery modes; local suite | Repository QA API secrets; local suite credentials are a separate path | Matrix and concurrency serialize within its repository. The strict proof/recovery manual audit is a point-in-time confirmation; it cannot prevent a subsequent competing writer. Schedule and other modes have no shared lock. |
| Crossplane | `qa-live.yaml` schedule 06:17 UTC; manual standard and strict modes; comment-triggered Uptest; local direct callers | `UPTEST_CLOUD_CREDENTIALS` repository secret | Current main defers scheduled/standard modes. PR #48 proposes deferring strict and Uptest too. No account-wide lock exists; read-only and in-process cleanup remain available. |
| Pulumi | `qa-live.yaml` schedule 08:17 UTC; manual AWS/GCP dispatch; script and cleanup-only paths | `REDISCLOUD_QA` repository secret; account equivalence is unknown | Concurrency is scoped to this repository. The always-run cleanup must remain possible. |

Recoveries can themselves mutate the account. Direct Crossplane Cloud recovery is disabled; controller-led cleanup already in progress must not be stopped. A complete production inventory of other QA consumers and scheduled environment resets is required before claiming exclusivity.

## Goals and non-goals

Serialize every authorized Cloud writer to each verified account, preserve cleanup after failure or runner loss, and prove both properties without a Cloud writer before staged live rollout. Keep read-only diagnostics available. This design does not select or provision a store, assert Pulumi account identity, change secrets, authorize a lock bypass, or qualify any connector lifecycle.

## Proposed protocol

1. A trusted coordinator authenticates the workflow identity and maps its independently verified Cloud account identity to one opaque stable lock key. An unrecognized origin, account, workflow, source commit, or attempt is rejected. A run ID alone is insufficient: GitHub reruns retain the run ID and increment the attempt.
2. Acquire atomically with an owner record containing repository, workflow, run ID, attempt, commit, unguessable capability and monotonically increasing generation. A failed, lost, or ambiguous acquire response never grants permission to write. The record persists without automatic expiration. Competing acquires fail while ownership or cleanup state is unresolved.
3. Before each Cloud mutation, an enforcement point verifies current ownership and generation. A check in a client immediately before a request is insufficient by itself: the runner may pause after the check and resume after ownership changes. The preferred architecture is an authenticated mutation proxy that alone holds the Cloud write credential, checks the generation on every request, and prevents stale writers from reaching Cloud. If no proxy or Cloud-native fencing is available, no enforcement guarantee exists. Read-only clients may retain separately scoped credentials.
4. Persist encrypted run-owned resource identities and in-flight request/task intent before each mutation. Unknown request outcomes remain unknown. Recovery continues under the original ownership, with exact IDs and identity checks, never prefix-wide deletion. Only the specifically authorized recovery path can assume an interrupted owner after establishing the prior writer cannot continue; a new generation cannot by itself stop a client with an already issued static API key.
5. Release is an atomic, owner-and-generation checked transition only after authenticated complete inventory proves all resources owned by the run are absent, relevant asynchronous operations are terminal, and pre-existing resources remain intact. Incomplete pagination, identity mismatch, inaccessible task history, unknown API response, or inability to establish writer termination retains the lock and records a manual recovery state. No wall-clock timeout, job cancellation, or `always()` release clears it automatically. Account-specific standing resources are excluded only by an authenticated baseline and explicit ownership rules.

The proxy/coordinator must protect durable state across crash, restart and failover, and its storage ACL must prevent untrusted clients from changing or deleting ownership records or release evidence. Every Cloud write path, including controllers, Pulumi operations, recovery and old workflow refs, must be subject to the same enforcement. Otherwise this is cooperative scheduling, not a lock. Existing static credentials and old jobs require an audited transition: quiesce writers, verify and preserve outstanding cleanup, disable or revoke old write access where supported, and verify old refs cannot retrieve or use write credentials before enabling the new path. Never remove the credentials needed by an active cleanup job. How Cloud API credentials can be constrained or revoked is not yet established.

## Alternatives and dependencies

- The existing `redislabsdev/github-actions-shared/actions/env-sync-pool` uses a Redis QA pool lock with a 9,000-second TTL and a force-override path. It is an existing environment-management service, but its expiration and override can allow a new writer while a previous Cloud mutation or cleanup remains unresolved. It is not suitable unchanged. Its Redis credential, ACLs, durability, usage authorization and access from the three connector repositories are unverified; do not reuse the credential or modify its pool.
- GitHub Actions concurrency is useful within one repository but does not supply a durable cross-repository account lock. A protected shared ref would still need verified atomic creation, no untrusted deletion or rewrite, durable history, and narrow cross-repository access. No such policy or approved GitHub App has been established. The existing one-shot recovery DELETE fence addresses a different problem and is not this account lock.
- A dedicated transactional store and proxy could enforce the protocol if approved and if both its durability and credential boundary are established. Existing GitHub-to-GCP workload identity documented for infrastructure deployment is scoped to that deployment repository, not evidence of authorization for these connector repositories or a suitable coordinator store.

Record the eventual durable architectural choice in an ADR after its owner, trust boundary, store and alternatives are evidenced. Do not mark the choice accepted merely because this proposal exists.

## Verification and rollout

Before any Cloud writer, use a credential-free fake broker/store and an isolated authorization rehearsal to prove: one winner under simultaneous acquire; run-attempt collision refusal; unknown acquisition response remains blocked; crash before and after intent, write and task completion retains ownership; stale generation and replay are denied at the mutation proxy; unauthorized release, premature cleanup release, missing pages, task uncertainty and foreign resources preserve the lock; a recovery actor can finish an old run without admitting a new writer; and only verified complete cleanup releases it. Test source-ref and OIDC constraints, denial of historical workflows and expired/revoked credentials separately. A fake store proves client logic, not service durability, credentials or authorization.

Integrate each connector behind its current fail-closed gate. Verify read-only preflight and in-flight cleanup separately. Obtain evidence for Pulumi account identity via an authenticated keyed comparison without exposing account IDs. Then audit all writer paths and credential access and rehearse forced runner loss. Once the approved store, proxy, policies, identity boundaries and recovery process are demonstrably enforced, run one bounded account writer at a time and verify exact resource cleanup before attempting concurrent admission. Roll back by reapplying the writer gates, leaving any active cleanup and lock in place; never delete an occupied lock to restore availability.

## Open questions

Which service and owner can provide an approved durable coordinator and appropriately restricted store? Can all Cloud writes be routed through a proxy and can old API credentials be revoked without harming other consumers? What are the authoritative complete account-task and inventory APIs? Do Pulumi credentials resolve to the same account? Which additional workflows, people or reset jobs can write it? What verified recovery procedure handles an indeterminate asynchronous task or unavailable owner?
