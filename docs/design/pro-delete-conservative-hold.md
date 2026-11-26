# Opt-in Pro database delete administrative hold

**TLDR:** A default-off conservative mode records a durable intent before a Pro database Delete and blocks further reconciliation of that deleting CR until an administrator resolves it. It intentionally holds successful deletes too.

**Date:** 2026-11-26

**Status:** Draft; not deployed

**Ticket:** [RED-225344](https://redislabs.atlassian.net/browse/RED-225344)

## Problem and scope

[#108](https://github.com/RedisLabs/crossplane-provider-rediscloud/pull/108) and [#110](https://github.com/RedisLabs/crossplane-provider-rediscloud/pull/110) reproduce a second fake DELETE after an accepted-then-503 response while the child remains visible. Cloud task processing completion may precede actual deprovisioning, and a lost server-generated task ID cannot be safely recovered by guessing. This proposal addresses replay by this one Pro database controller under a trusted-writer operating model; it does not solve general shared-account serialization.

## Proposed behavior

`--enable-pro-database-delete-hold` defaults to false. When opted in, the Pro database controller requires a deleting CR with the managed finalizer, canonical positive `subscription/database` external identity matching its subscription field, and a named ProviderConfig. It reads directly from the API server, compares UID, identity and spec, then stores a random-token intent with optimistic resourceVersion update before dispatch. A conflict, timeout, ambiguous response, changed identity or existing intent forbids dispatch. A second direct read verifies the intent and identity. No timer or GET alone clears it.

Any intent, including malformed or empty, blocks this connector’s observation and deletion even if the flag is disabled later. For a deleting CR with Delete policy, the managed reconciler retains its finalizer on both successful and failed/unknown deletes. Crossplane handles an Orphan/no-Delete policy before invoking the connector and can remove the finalizer despite the marker; that is an explicit trusted-administrator bypass, not automatic proof of Cloud cleanup. Admission/RBAC must prevent untrusted policy and finalizer changes. An intent may be persisted even when Upjet merely requeues provider scheduling and sends no DELETE, so a nil return is not proof of dispatch. Crash-before-dispatch has the same uncertain, manual-resolution treatment. The feature does not infer completion from an async callback or a single 404. Supported recovery is a deliberate administrative decision: preserve the CR and intent while the task is unknown; independently identify the exact Cloud account and resource and obtain authoritative task/absence evidence. A trusted administrator may then intentionally choose Orphan/finalize the CR using standard Kubernetes/Crossplane procedures only after deciding the residual-risk and ownership implications. Such an action bypasses this connector, can leave external resources and requires separate evidence and authorization. Never remove the marker merely to retry, replay DELETE after an uncertain response, or treat a processing-completed task or one 404 as sufficient terminal proof. This is intentionally inconvenient and requires an explicit runbook and review before use.

## Trust and safety limits

The marker is a CR annotation, not an authorization boundary. This mode assumes all CR metadata/spec/finalizer writers, ProviderConfig and credential Secret writers, and cluster administrators are trusted and access controlled; they can bypass it. The provider cannot atomically prevent changes after its recheck. It does not fence another CR for the same Cloud resource, another provider or direct API credentials, or an old/in-flight controller that lacks the guard. Before use, drain all old or differently configured writers, adjudicate pre-existing uncertain deletions, freeze ProviderConfig/Secret identities and contents for the operation, establish account identity and relevant admission/RBAC controls, and protect the intent. A ProviderConfig name alone does not prove its UID, credential version or Cloud account. Disabling the flag alone does not release an existing marker in a guard-aware binary; rolling back to an older binary can bypass it. Missing credentials, unverifiable IDs or failed reads hold rather than assume success. Opt-in changes availability: every managed Pro database deletion needs operator resolution. Do not enable this by default or claim complete cross-writer fencing.

## Verification and rollout

Hosted-only tests should exercise actual managed reconciler and pinned Terraform provider against a fake API and a real disposable API server: default mode, opt-in successful and accepted-503 deletes, present then absent fake child, restart, concurrent CAS, failed/ambiguous persistence, mutation/identity/finalizer guards and explicit administrative resolution. Require exact fake resource identity, one DELETE maximum and finalizer retention; do not label simulation as live qualification. Verify generated controller postprocessing remains reproducible. No deployment or live Cloud write is proposed until independent security review, the trusted-writer boundary, recovery procedure and release decision are accepted.

## Decisions

The related [ADR-0003](../adr/ADR-0003-pro-database-delete-hold.md) proposes this trust and availability tradeoff; it remains unaccepted until the responsible reviewers establish protections and recovery. No business decision is made.
