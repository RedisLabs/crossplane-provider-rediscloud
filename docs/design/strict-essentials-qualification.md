# Strict Essentials qualification

**TLDR:** Add an opt-in hosted qualification for a single GCP Essentials subscription and database. A successful result requires authenticated exact-ID reads before and after CR deletion and an independently checked inventory.

**Date:** 2026-11-24. **Status:** Proposed. **Ticket:** RED-225344.

**Live execution is disabled.** A durable, encrypted ownership checkpoint and a separate recovery path are required before a runner can safely lose its local state. The read-only preflight may be dispatched independently.

## Problem and goals

The standard Nightly QA suite deletes any resource matching its shared prefix during setup and final cleanup. It also does not establish the parent remained present while the database was removed. The opt-in case must not touch another run's resources and must distinguish controller-driven deletion from recovery. It does not change standard resource coverage or claim broader connector parity. The shared HTTP client rejects redirects and reports transport failures without endpoint details, and the standard job timeout is extended to accommodate bounded recovery.

## Design

The workflow builds this commit's provider in hosted Kind. It requires an independently verified QA origin that matches the credential's URL with an HTTPS `/v1` path and rejects the default public endpoint. It verifies that the read-only plan lookup returns GCP and us-central1, then creates CRs with `deletionPolicy: Delete`. It requires Ready and Synced, UID and finalizer, matching parent and child identities, and authenticated HTTP 200 for each exact resource. The database CR is deleted first, and the parent must remain HTTP 200 until the database becomes HTTP 404 and its CR disappears. The database list is paginated to an empty authenticated page before deleting the parent. The parent must then become HTTP 404 and its CR disappear; the account's fixed-subscription list must not contain the run name.

Failure remains failure. Recovery retries CR deletion first. Direct API recovery is limited to the exact resource ID and name created by the run, requires a verified parent and a complete empty database list before subscription deletion, and is explicitly recorded. A failed or ambiguous read leaves absence unverified and preserves the parent and controller credentials. The workflow does not invoke shared-prefix cleanup for this mode.

## Operations and validation

Set the private Actions secret `QA_VERIFIED_ORIGIN_HASHES` to comma-separated lowercase SHA-256 digests of independently verified HTTPS QA origins (without trailing slashes). Missing or malformed values fail closed. Verify the credential points to one of those origins before dispatch. Serialize live account writes with the Operator and all scheduled workflows. Record commit, running package identity, controller evidence and sanitized logs with the Actions run. The fake Cloud tests exercise failures, scoped recovery, identity checks and pagination; hosted acceptance is separate and is not established by unit tests. If recovery cannot complete before the hosted job ends, preserve the run link and report unknown or remaining resources for follow-up.

Roll back by selecting the standard workflow mode or reverting this change. The standard workflow's shared-prefix cleanup remains unchanged and must not be used as recovery for this strict qualification. There is no new API or architecture decision; existing provider and Redis Cloud ownership contracts remain in force.
