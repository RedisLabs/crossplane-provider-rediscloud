# ACL lookup identity validation

**TLDR:** Reject an ACL role, rule or user lookup response when the selected resource has no ID. Clear prior observation and retry through the existing safe failure path.
**Date:** 2026-11-26
**Status:** Proposed
**Ticket:** [RED-225344](https://redislabs.atlassian.net/browse/RED-225344)

## Problem and scope

Name matching can select an ACL response whose optional SDK ID is missing. The current observation converts a missing ID to `0` and reports Ready. A composition could then consume an invalid identifier. This change applies to the three ACL lookups; it does not establish the completeness of account list responses or change managed ACL resources.

## Behavior and safety

After selecting one matching name, require the ID to be present before building an observation. Existing lookup failure handling clears stale observed fields, reports a fixed sanitized condition and retries; it does not mutate Redis Cloud. Valid IDs and the public API shape remain unchanged. No new architectural or business decision is required.

## Validation and rollout

A hosted-only fake Cloud fixture runs the actual lookup reconciler for role, rule and user, with valid, missing, zero and negative IDs followed by recovery; zero and negative values remain accepted because the API does not establish a minimum. It asserts exact GET paths, Ready state, ID clearing, sanitized failure and retry. CI requires explicit named Go test receipts. This is not live Cloud qualification or proof of list completeness. Deploy through the normal reviewed provider release; roll back the provider version if valid upstream IDs are unexpectedly rejected. The API does not establish a minimum, so this change deliberately preserves any numeric value returned by Cloud.
