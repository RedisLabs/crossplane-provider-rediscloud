# Crossplane read-only QA region lookup

**Date:** 2026-11-25  
**Status:** Candidate, requires hosted guard verification before live dispatch  
**Tracking:** RED-225344

## TL;DR

Exercise the real Regions reconciler against the existing QA account with an
in-memory Kubernetes client. A fail-closed HTTP transport permits only GET
`/v1/regions` at an independently allowlisted HTTPS origin and rejects redirects.
No Cloud resources are created or modified. This does not provide writer
serialization or qualify live lifecycle behavior.

## Scope and behavior

An opt-in GitHub Actions dispatch to the dedicated candidate branch builds a
test binary without Cloud credentials, runs a negative transport-guard test,
and records its SHA-256. The live job downloads that exact artifact, checks its
digest, then supplies the existing protected QA credentials only to that binary.
It runs two reconciliations against a fake in-memory Kubernetes API, filtering
AWS and GCP, and checks Ready, nonempty filtered results and exactly two HTTP
200 responses. Only counts, request method/path and status are logged; account
identifiers, returned IDs and credentials are not logged.

The guard rejects other methods, hosts, paths, queries, request bodies, plaintext
and any redirect before following it. The QA URL must match the independently
configured origin hashes used by the existing QA harness; the binary additionally
requires an opt-in GitHub Actions run in this repository. The guard is an
in-process boundary, not an account-wide network policy or a control on other
independent processes. No shared lock or account ownership inference is made.

## Validation and operations

Before dispatch, the PR must pass the hosted credential-free guard test on the
exact intended revision and reviewers must inspect the workflow and compiled
test. The dispatch runs the guard again before exposing credentials and records
the exact binary digest. If any guard, HTTPS origin, auth, lookup, Ready, result
or traffic assertion fails, report failure and stop; do not attempt cleanup or
convert it into lifecycle success. Existing Cloud resources are neither named
nor changed. Rollback is to remove this candidate branch/job; preserve running
jobs and do not alter the disabled Pulumi workflow.

ADR: no new decision for a bounded test harness; the shared-account admission
architecture remains proposed separately. BDR: none. Live provider deployment,
resource lifecycle, credential scope and account isolation are out of scope.
