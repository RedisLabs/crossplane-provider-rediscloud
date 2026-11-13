# Remote GCP evidence verification

Date: 2026-11-11. Status: Experimental verification branch.
Ticket: https://redislabs.atlassian.net/browse/RED-225344

## TLDR
Build baseline 5ceb4431079b46fe79159b9ff637d4a3f1efb0e6 in GitHub Actions,
separately from this harness. This branch replaces Nightly QA only for an
explicit workflow dispatch; it is not intended to replace the main workflow.

## Behavior
Create one GCP us-central1 Pro subscription, database, and ACL rule through
Crossplane in Kind. Require the explicit QA endpoint. Upload allowlisted active
evidence before waiting up to 30 minutes for a screenshot acknowledgment on
codex/red225344-evidence-ack. The verifier records screenshot SHA256 hashes in
ack/<run-id>-<attempt>.json only after saving and checking both SM images.
Timeout fails acceptance and triggers cleanup to bound live resource lifetime.
Normal cleanup waits for each child finalizer before deleting its parent.

## Security and validation
Credentials use the existing Actions secret and never enter artifacts.
Provider source is checked out at the immutable baseline; harness changes do
not change the tested provider. No release publication is performed.
Validate syntax and failure guards locally without starting Kind; validate
resource behavior in Actions. All failures remain acceptance failures.

## Risks and rollback
Runner termination can interrupt cleanup; inspect CAPI and retain recovery
credentials until absence is independently confirmed. Never cancel a run with
live resources. Rollback is discontinuing dispatch of this experimental branch.
No new architecture or business decision; ADR and BDR are not needed.

## Failure evidence and cleanup repair — 2026-11-13

Status: local harness repair, hermetically tested; live retry remains blocked.
This does not change the provider baseline or resolve the original provisioning
failure. No new Cloud run is authorized by this repair.

The adapter persists numeric subscription/database IDs as soon as observed in
its exact-name Cloud subscription lookup or managed CR reads, before Ready.
Ambiguous duplicate Cloud names and mismatched database subscription IDs fail
closed. A create exception captures allowlisted Provider, ProviderConfig, and
managed-resource status in `failure-kubernetes.json`; individual failed reads
are recorded without discarding the other statuses. Messages, specs, credential
values and arbitrary provider fields are excluded. Raw backend task payloads
are not captured: obtaining the missing creation task diagnostic is still open.

Cleanup validates that saved state belongs to the current run. Database deletion
must complete before parent subscription deletion. An unresolved child skips
its parent, while independent ACL cleanup still runs. Every resource result is
persisted in `06-cleanup.json`. Any exception, finalizer timeout, remaining run
prefix, missing subscription ID, non-404 response, or lookup cleanup failure
keeps cleanup FAILED. Accepted requests do not prove deletion. Credentials and
lookups remain available when managed-resource cleanup fails; finalizers are
never stripped. The original create step remains failed even if later cleanup
passes. Runner termination can still interrupt evidence capture and cleanup.

Hermetic validation:
`python3 -m unittest discover -s scripts -p test_evidence_run.py -v`.
Tests cover timeout/error before Ready, early IDs, foreign-state/resource
rejection, mismatched database ownership, sanitization, independent cleanup,
missing IDs, accepted-but-not-deleted responses, and confirmed absence.
The original schedule assertion passes at provider baseline `5ceb443` and
fails at unmodified experimental harness commit `1613c52`. The harness replaced
Nightly QA with manual-only verification without updating that assertion. This
repair narrowly tests the documented dispatch-only contract; it does not enable
scheduled live provisioning. Baseline comparison is retained in review evidence.

Error-path discovery also attempts an exact-name Cloud lookup when creation
fails before the first CR poll. Changed or duplicate subscription IDs persist an
ownership-conflict marker and prevent cleanup from reporting success. Missing
cleanup state fails instead of silently returning success. None of these changes
asserts that the original backend provisioning fault has been diagnosed.

Design: this document. ADR: not needed for a harness failure-path repair.
BDR: not needed; no product or commercial policy changes.

## Bounded AWS visual verification — 2026-11-13

The successful GCP retry 43468905029 completed normal cleanup. Preserve the
earlier failure and its unknown cause. The manual cloud input now selects AWS
us-east-1 or GCP us-central1; it is supplied identically to create and cleanup.
The provider baseline remains unchanged. Before each non-Secret apply, the
adapter prints the exact kubectl invocation that its apply method executes,
without stdin or credential values. Capture the genuine GitHub Actions console
while resources exist; never label a rendered artifact as a live terminal.
Build logs include commit, package hashes, image inventory and Kind node status.
The same ownership checks, evidence acknowledgment and normal cleanup apply.
