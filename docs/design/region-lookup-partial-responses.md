# Region lookup partial responses

**TLDR:** Refuse malformed region rows that could match the requested filters and rows without a selected ID, clearing stale status rather than publishing a partial result as Ready.
**Date:** 2026-11-26
**Status:** Proposed
**Ticket:** RED-225344

## Behavior

A null region cannot be classified. A row with a missing name or provider is rejected unless another known field excludes it. A matching row must also have an ID; explicit empty names and providers and numeric zero are preserved when filters allow them. Definitive nonmatches remain excluded. Existing error handling clears the previous observation, emits a fixed sanitized condition and retries. This GET-only lookup makes no Cloud mutation. This validation cannot detect a silently truncated or absent list.

## Validation and rollout

Hosted-only actual reconciler tests use dummy credentials and a fake GET endpoint to check malformed mixed lists, stale-status clearing, recovery, definite nonmatches and explicit zero. Named CI receipts ensure each case ran. Existing AWS and GCP lookup fixtures remain. This is simulated coverage, not live Cloud qualification. Review and deploy through the normal provider release; revert this targeted validation if an upstream response shape is legitimately incompatible. No ADR or BDR is needed for this narrow validation fix.
