# PrivateLink lookup response identity

**TLDR:** Reject explicitly contradictory subscription or region IDs in PrivateLink lookup responses and do not fetch a Pro endpoint script after a contradictory parent response.

**Date:** 2026-11-26

**Status:** Proposed

**Ticket:** [RED-225344](https://redislabs.atlassian.net/browse/RED-225344)

## Problem and scope

PrivateLink response fields may include subscription and region IDs, but Pro and Active-Active lookups previously ignored them. A contradictory response could appear Ready under the requested ID. The Pro script lookup also read a subscription and link without checking any explicitly returned parent identity before fetching its script.

## Behavior and safety

When present, the response subscription ID must match the requested subscription; Active-Active links also check an explicitly returned region ID. The Pro script lookup checks its subscription and link before making downstream calls. Missing optional fields retain existing behavior because the SDK defines them as optional. Existing errors clear stale status and expose a sanitized condition. This does not establish identity when fields are omitted or prevent unrelated Cloud writes.

## Validation and rollout

Hosted-only fake Cloud tests run actual Pro and Active-Active lookup reconcilers and the Pro script reconciler through matching, mismatched, missing and recovered identities. Tests require no downstream script call after mismatch and verify stale status, fixed failure conditions and named CI receipts. This simulation is not live qualification. Release through the standard reviewed provider process; revert if the API contract establishes different response identity semantics. No ADR or BDR is needed for a localized consistency check.
