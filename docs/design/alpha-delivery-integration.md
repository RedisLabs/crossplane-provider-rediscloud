# Crossplane alpha integration candidate

**TLDR:** Integrate the reviewed alpha lookup/API changes and hosted fake-Cloud fixture coverage into one reviewable candidate. The known uncertain-delete replay remains an open release gap.

**Date:** 2026-11-26

**Status:** Draft; not released

**Ticket:** [RED-225344](https://redislabs.atlassian.net/browse/RED-225344)

## Scope and behavior

This candidate builds on #81's Pro subscription read-only lookup, lookup error and SDK log protections, and Pro, ACL, PSC and PrivateLink simulated fixture integration. It combines #102's ACL User Observe-only Secret handling, #105's missing ACL lookup ID checks, #107's PSC response service-ID consistency and #109's PrivateLink response identity checks. It also brings in the independent fake accepted-create uncertainty and cloud-selection fixtures from #96, #97, #101, #103 and #104, and the immediate-absence accepted-delete fixture from #106. These tests are simulated; they do not claim live cloud qualification, independent source authentication, or coverage of omitted optional IDs.

The Pro parent fixture preserves separate GCP and AWS happy paths and a GCP accepted-then-503 path; CI requires each named case. Fixture task/error status and reconcile behavior are not a guarantee about live Cloud behavior.

## Known release gap

[#108](https://github.com/RedisLabs/crossplane-provider-rediscloud/pull/108) and [#110](https://github.com/RedisLabs/crossplane-provider-rediscloud/pull/110) show a duplicate fake Pro database DELETE after an accepted-then-503 response while the child remains visible, including with API-server persistence and an in-process reconstructed reconciler/store. This candidate does not contain a fix and must not be represented as closing that gap. The disabled [#113](https://github.com/RedisLabs/crossplane-provider-rediscloud/pull/113) test-only experiment does not protect production writers or establish live terminal deletion. Cloud processing completion can precede deprovisioning, and safe fencing, task correlation, marker protection and recovery remain unresolved. Keep live shared-account writers gated; release safety requires separate review and resolution.

## Validation and rollout

Run generation, lint, unit and all named hosted fixture jobs on this integrated commit; verify the combined results rather than inheriting green states from sibling PRs. Run Kubernetes acceptance only in GitHub Actions; no real Cloud mutations are authorized by this candidate. A built package or successful fake fixture is not a published release or live qualification. Preserve individual PRs and request human review of the public CRD and security changes. No merge or publication is implied. Roll back via the normal provider release process if changes are later shipped.

## Decisions

This is integration of existing proposals, not a new architecture or business decision. See the linked component designs for behavior and the separate uncertain-delete work for its unresolved trust boundary.
