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
