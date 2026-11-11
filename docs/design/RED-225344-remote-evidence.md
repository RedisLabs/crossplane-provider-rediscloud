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
