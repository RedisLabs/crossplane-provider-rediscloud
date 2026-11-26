# ADR-0003: Opt-in conservative Pro database delete hold

**Status:** Proposed
**Recorded:** 2026-11-26
**Decision owner:** Unconfirmed
**Related:** [Design](../design/pro-delete-conservative-hold.md), [RED-225344](https://redislabs.atlassian.net/browse/RED-225344), [ADR index](ADR-index.md)

## Context

A hosted fake Cloud fixture reproduced a second Pro database DELETE after an accepted-then-503 response while the child remained visible. Upjet can retry when Terraform refresh observes the resource. Cloud task processing completion may precede deprovisioning, and a lost task ID is ambiguous. A single 404 or elapsed time does not establish safe replay or terminal completion.

## Alternatives

Continue automatic retry, accepting unresolved duplicate-dispatch risk; build an account-wide external broker with enforceable credentials and authoritative task correlation, which is not yet available; or offer an opt-in conservative hold scoped to this CR and controller. The hold trades automatic cleanup availability for reduced replay by participating replicas under explicit trusted-writer assumptions.

## Proposed decision

Offer a default-off Pro database controller mode that records a resourceVersion-CAS intent before the first delete and holds subsequent Delete-policy reconciliation until trusted administrative resolution. Treat ambiguous writes and callbacks as unknown and never auto-clear on a timer or one read. Keep this proposal unaccepted and unenabled until security and operator review establish its actual trust boundary and recovery procedure.

## Consequences and revisit condition

All opted-in deletes, including successful ones and scheduling paths that may not dispatch, require manual adjudication. This does not fence other CRs, old or disabled replicas, direct credentials or privileged writers who can alter CR metadata, ProviderConfig or Secrets; Orphan policy bypasses the connector. Enabling requires a drained rollout and appropriate RBAC/admission controls. Revisit when a protected shared write boundary and authoritative durable task/result correlation can safely distinguish terminal outcomes and recover without manual finalization.
