# Pro database hold across separate processes

**TLDR:** Hosted CI runs two sequential controller test processes against the same disposable API server, loopback fake Cloud, CR identity and workspace. The first encounters an accepted-then-503 fake delete; the second runs the product hold with the feature disabled.

**Date:** 2026-11-26  
**Status:** Draft, hosted verification pending  
**Ticket:** [RED-225344](https://redislabs.atlassian.net/browse/RED-225344)

The supervisor owns the fake API and envtest API server across both workers. It prepares a CR for the existing fake Pro child, a private temporary client configuration and a workspace directory. Each worker uses the actual managed reconciler, product connector, pinned Terraform and pinned provider. The supervisor requires distinct process IDs, successful worker receipts, a failed callback on the first, retained marker/finalizer and CR UID, and one fake DELETE across both. The second worker must not open an upstream workspace. The accepted fake delete keeps the child present, making a later retry observable. CI is credential-free and hosted-only.

This tests sequential process replacement after the first callback returns. It does not simulate abrupt termination, a controller Pod restart, live Redis Cloud, duplicate external writers, safe automatic recovery or eventual Cloud deprovisioning. The existing trust, drain, administrative hold and rollback limits in [the guard design](pro-delete-conservative-hold.md) apply. The workflow downloads checksum-pinned dependencies; existing receipt validation prevents skip or missing test from passing. A passing fixture is not a release or deployment approval.

Validation requires exact-head hosted Actions and independent review. Rollback removes this fixture and workflow job; no live resources are touched. The existing proposed ADR covers the guard, and no business decision is made.
