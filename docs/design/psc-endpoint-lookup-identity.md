# PSC endpoint lookup service identity

**TLDR:** Reject contradictory service IDs and malformed or duplicate endpoint entries before fetching scripts or publishing status.

**Date:** 2026-11-26

**Status:** Proposed

**Ticket:** [RED-225344](https://redislabs.atlassian.net/browse/RED-225344)

## Problem and scope

The PSC endpoints response can include `pscServiceId`, but the lookup previously ignored it and published endpoints under the requested service ID. A contradictory response could also cause script requests for endpoints belonging to another service. Both Pro and Active-Active lookup paths use the same response structure.

## Behavior and safety

If the response includes a service ID, require it to match the requested ID before fetching endpoint scripts. The SDK makes the field optional; preserve existing behavior when it is absent. Existing error handling clears stale status, publishes a sanitized failure and retries. No Cloud mutation or API shape changes are introduced. Before any script request, reject a null endpoint, a missing endpoint ID or repeated endpoint IDs. A malformed collection clears stale status and remains unavailable rather than publishing an incomplete or ambiguous list. The existing endpoint mapper already requires IDs; optional parent service ID behavior remains unchanged. This does not establish response completeness, account identity, or safe live-writer serialization.

## Validation and rollout

Hosted-only fake Cloud tests run both actual lookup reconcilers through matching, mismatched, null, missing-ID and duplicate endpoint entries, absent optional service ID, and recovery responses. They require no script fetch on mismatch, stale status clearing, sanitized conditions and named test receipts. This is simulation, not live qualification. Roll out through the normal reviewed provider release; revert if an API contract later establishes different identity semantics. No ADR or BDR is required for this localized consistency check.

## Malformed SDK task response containment

The pinned SDK can synchronously panic on a task response with no task ID or a completed task without `response.resource`, before lookup validation runs. As a temporary containment, only the synchronous SDK calls made by the Pro and Active-Active PSC service, endpoint-list and endpoint-script read paths are wrapped. A panic yields a fixed error without the panic value; existing lookup handling clears status, marks the object unavailable and retries after one minute. This also catches unrelated synchronous SDK panics in those calls, potentially concealing defects; it does not protect asynchronous goroutines, unrecoverable runtime failures, managed-resource writers, or establish that Cloud sends malformed responses. Clearing existing status can affect consumers. Persistent malformed responses remain unavailable and retry at a fixed interval. The upstream SDK validation remains the preferred long-term fix; remove the boundary when a verified fixed SDK is adopted.

A hosted, dummy-authenticated fake reconciler test covers valid reads, both malformed task shapes, status clearing and recovery; a synthetic panic checks that the error omits its value. The test does not establish connection-leak behavior, every affected PSC method, real Cloud behavior or live qualification.
