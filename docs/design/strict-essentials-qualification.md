# Strict Essentials qualification

**TLDR:** Add an opt-in hosted qualification for a single GCP Essentials subscription and database. A successful result requires authenticated exact-ID reads before and after CR deletion and an independently checked inventory.

**Date:** 2026-11-24. **Status:** Proposed. **Ticket:** RED-225344.

**Dispatch prerequisite:** The coordinator must first finish Operator GCP qualification, verify its clean inventory, and ensure no Cloud writer in either repository is active. The read-only preflight may run independently. Qualification uploads and retrieves its encrypted ownership intent and task baseline before Cloud writes, then authenticates the downloaded intent against the run, attempt, commit and account before installing the provider credentials.

## Problem and goals

The standard Nightly QA suite deletes any resource matching its shared prefix during setup and final cleanup. It also does not establish the parent remained present while the database was removed. The opt-in case must not touch another run's resources and must distinguish controller-driven deletion from recovery. It does not change standard resource coverage or claim broader connector parity. The shared HTTP client rejects redirects and reports transport failures without endpoint details, and the standard job timeout is extended to accommodate bounded recovery.

## Design

The workflow builds this commit's provider in hosted Kind. It requires an independently verified QA origin that matches the credential's URL with an HTTPS `/v1` path and rejects the default public endpoint. It verifies that the read-only plan lookup returns GCP and us-central1, then creates CRs with `deletionPolicy: Delete`. It requires Ready and Synced, UID and finalizer, matching parent and child identities, and authenticated HTTP 200 for each exact resource. The database CR is deleted first, and the parent must remain HTTP 200 until the database becomes HTTP 404 and its CR disappears. The database list is paginated to an empty authenticated page before deleting the parent. The parent must then become HTTP 404 and its CR disappear; the account's fixed-subscription list must not contain the run name.

Before deletion, the case changes the database eviction policy to a different supported value. It requires the original CR UID and parent/database IDs, Ready and Synced conditions, and matching controller and authenticated Cloud observations. It does not establish no-op reconciliation or the absence of additional writes. Other update fields, including rename and password rotation, remain unqualified by this case.

Failure remains failure. The original job holds the controller and credentials while retrying CR-only recovery for a bounded 60-minute window. It preserves a CR whose exact Cloud identity is not yet established, and preserves the parent if the database is not independently absent or its child inventory is incomplete. If the hold expires, it reports unverified residue and preserves the Actions run and encrypted intent for recovery. The job does not directly delete Cloud resources. The separate recovery workflow currently permits read-only discovery only. A failed or ambiguous read leaves absence unverified. The workflow does not invoke shared-prefix cleanup for this mode.

The separate direct recovery mode is disabled because task visibility cannot rule out an earlier uncertain DELETE. Use read-only discovery and manual investigation after runner loss until a durable replay guard is available.

## Operations and validation

Set the private Actions secret `QA_VERIFIED_ORIGIN_HASHES` to comma-separated lowercase SHA-256 digests of independently verified HTTPS QA origins (without trailing slashes). Missing or malformed values fail closed. Verify the credential points to one of those origins before dispatch. Serialize live account writes with the Operator and all scheduled workflows. Record commit, running package identity, controller evidence and sanitized logs with the Actions run. The fake Cloud tests exercise failures, scoped recovery, identity checks and pagination; hosted acceptance is separate and is not established by unit tests. If recovery cannot complete before the hosted job ends, preserve the run link and report unknown or remaining resources for follow-up.

Roll back by selecting the standard workflow mode or reverting this change. The standard workflow's shared-prefix cleanup remains unchanged and must not be used as recovery for this strict qualification. There is no new API or architecture decision; existing provider and Redis Cloud ownership contracts remain in force.
