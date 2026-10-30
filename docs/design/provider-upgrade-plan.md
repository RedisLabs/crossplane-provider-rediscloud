# Plan: close the alignment gaps, then bring the provider current

Date: 23 October 2026.
Status: Proposed.
Owner: Shay Berman.
Ticket: [RED-222728](https://redislabs.atlassian.net/browse/RED-222728). Summary: Lineslay.
Design: this file.
ADR: [ADR-0001](../adr/ADR-0001-follow-upjet-v2.md). Status Proposed. The decision is to follow Upjet v2.5.1 and stay cluster-scoped in that pull request.
BDR: none. This is a technical refresh of the public provider.

This plan starts after [pull request #2](https://github.com/RedisLabs/crossplane-provider-rediscloud/pull/2) is on `main`. That pull request is the Terraform v2.19.2 alignment. This document does not change it.

Versions below were read on 23 October 2026. The upgrade step re-reads them on the day it starts and uses those pins. It does not keep a release candidate if the stable Upjet module does not require it.

## TLDR

Finish `docs/terraform-alignment.md` before any component upgrade. That finish is two pull requests: lookups (gap 8), then the release-tracking workflow (gap 9). Gaps 1, 2, 3, 4, 5, and 7 are already settled on pull request #2. Gap 6 stays `v1alpha1` until the later upgrade. Terraform CLI stays 1.5.7. A pull request merges only after the QA Redis Cloud account suite is green and Bugbot is clean.

As of 28 October 2026 those alignment items are on `main`. Ticket [RED-222728](https://redislabs.atlassian.net/browse/RED-222728), summary Lineslay, finishes the line move in three steps: move the Upjet line, promote the managed APIs to `v1beta1`, then add `govulncheck`. Each step is verified before it merges. The activity is finished when the final check in "How to finish this activity" passes on `main`.

A one-day unattended run uses `docs/design/background-day-plan.md`. That run does not merge, and it does not start the Upjet line move.

## Motivation

Recorded 28 October 2026. The Redis Cloud resources are already aligned to Terraform provider 2.19.2. The provider binary that serves them is still Crossplane 1.16 and Upjet v1.4.1. Crossplane stopped maintaining 1.16 in February 2025. Go 1.24 stopped on 10 February 2026. The Kubernetes 1.29 clients stopped on 28 February 2025. A current Crossplane cluster can still reconcile that old binary. That run leaves the provider off the line that receives security patches.

Upjet v2.5.1 is the GA module that matches the Crossplane release still under maintenance. Go, crossplane-runtime, controller-runtime, and the Kubernetes clients move in that same pull request, because that module builds as one set. Managed APIs stay `v1alpha1` there. Promotion to `v1beta1` is the next pull request, so clients change the API version once, on the supported line.

The header records this as a technical refresh of the public provider. BDR: none.

## How to finish this activity

Ticket [RED-222728](https://redislabs.atlassian.net/browse/RED-222728), summary Lineslay. Recorded 28 October 2026.

This activity is three pull requests after the alignment work. Each pull request is verified on its own and merged before the next branch starts. When the third is on `main`, run the final check once. That final check is the proof the activity is finished.

The QA check in every step is one run of `scripts/qa_live.py` on a provider built from that pull request. It uses the QA account and the kind context `kind-local-dev`. It does not target `sm-qa`. Credentials stay outside the repository. The run is finished only when the log shows lookups Ready, the ACL rule created and gone, the subscription Cloud `active` and Ready then HTTP 404, the database Cloud `active` and Ready, secret key names `public_endpoint` and `private_endpoint`, and both deletes HTTP 404. Afterward, no subscription name starts with `xp-ci-`. Do not print secret values or the payment method id.

### Already on main

These are not redone.

| Piece | Where it landed |
| --- | --- |
| Terraform provider 2.19.2 alignment, 27 managed resources | `main` |
| Lookups for payment method, Essentials plan, and region | `main` |
| Release-tracking workflow | `main` |
| One Go 1.24 pin across CI, the nightly workflow, and the Makefile | `main` |
| Guard tests for the timeout, the TLS secret list, the database reference, and unknown example fields | Pull request 11 |
| Nightly QA script and `ci-success` ruleset | `main` |

### Step 1. Move the line

Branch from current `main`. Re-read `github.com/crossplane/upjet/v2` that day and use the versions in its `go.mod`. The 28 October 2026 re-read is still Upjet v2.5.1: Go 1.26.8, crossplane-runtime v2.4.0, Crossplane APIs v2.4.1, controller-runtime v0.23.3, and Kubernetes clients v0.35.3. crossplane-tools stays on the 19 July 2026 commit from before it required Kubernetes 0.36, so the clients stay at v0.35.3. The compiler is Go 1.26.10, the current 1.26 patch. The test cluster installs Crossplane 2.4.3. The Crossplane CLI is v2.4.1, the latest published CLI on that line. CLI v2.4.2 and v2.4.3 were not published. The build scripts no longer download `up`. Move those together. Fill kind v0.33.0, golangci-lint v2.15.0, and uptest CLI v2.2.0 from the stable tool release when Upjet does not pin them. controller-tools stays at v0.20.0, the version Upjet v2.5.1 requires, because v0.23.0 pulls Kubernetes clients v0.37. Regenerate cluster-scoped resources only (`pipeline.Run` with a nil namespaced provider). Keep 27 managed resources, the 30-minute timeout, the Active-Active TLS secret-reference list, Terraform CLI 1.5.7, and managed API `v1alpha1`. The ADR in this pull request is [ADR-0001](../adr/ADR-0001-follow-upjet-v2.md).

Verify this step before merge:

1. `make generate` reports 27 resources, including the six added after Terraform v2.1.5.
2. `make test` passes, including the guard tests. A controller that returns to `3 * time.Minute` fails. A database type that grows `subscriptionIdRef` fails.
3. CI `ci-success` is green.
4. The provider is Healthy on the test cluster.
5. The QA check above passes on this binary, and no `xp-ci-` subscription remains.
6. Review threads are resolved. Merge to `main`.

A failed generate, a red guard test, or a QA resource that does not reach Ready and then 404 stops this step. Do not open step 2 from that branch.

### Step 2. Promote the APIs

Branch from `main` after step 1 is merged. Move all 27 managed kinds from `v1alpha1` to `v1beta1`. Examples, the README, and `docs/terraform-alignment.md` follow. `v1alpha1` stays served for those kinds so a cluster that already stored it can accept the CRD. Storage and the controllers are `v1beta1`. ProviderConfig stays `v1beta1`. Lookups stay `v1alpha1`.

Verify this step before merge:

1. Every file under `examples/` that is a managed resource uses `v1beta1`.
2. `python3 scripts/check-examples.py package/crds examples` passes.
3. The QA check passes using `v1beta1` manifests for the ACL rule, the Pro subscription, and the database.
4. `docs/terraform-alignment.md` marks gap 6 fixed.
5. CI `ci-success` is green and review threads are resolved. Merge to `main`.

A resource that becomes Ready only as `v1alpha1` fails this step.

### Step 3. Add govulncheck

Branch from `main` after step 2 is merged. Run `govulncheck` in CI with the Go version from step 1. A fixable finding in a module this repository can bump, without leaving the Upjet line, fails CI. Terraform provider bumps stay in the release-tracking workflow.

Verify this step before merge:

1. The first `govulncheck` report is attached to the pull request.
2. CI `ci-success` is green.
3. One QA check passes, so the tool change did not change the running provider.
4. Review threads are resolved. Merge to `main`.

### Verify the finished activity

Run this once, on `main`, after step 3 is merged. The activity is finished when every line below is true.

1. `go.mod` and the Makefile match the Upjet module re-read in step 1. Terraform CLI is still 1.5.7.
2. `make generate` still reports 27 resources. The 30-minute timeout and the TLS secret-reference list are still present.
3. Every managed example is `v1beta1`. `make check-examples` passes.
4. `make test` passes.
5. The `ci-success` run on that `main` commit is green.
6. One QA check on that `main` build passes, and the account has no subscription whose name starts with `xp-ci-`.
7. `docs/terraform-alignment.md` shows gap 6 fixed. The ADR for the Upjet v2 choice is on `main`.

## Finish the alignment doc first

Confirmed by Shay, 23 October 2026. The first work is the open items in `docs/terraform-alignment.md`. The Go, Upjet, and Crossplane upgrades in pull requests D, E, F, and G wait until those items are merged.

| Alignment gap | Pull request to finish it |
| --- | --- |
| 1, 2, 5, 7 | Already done on pull request #2. No further pull request. |
| 3, 4 | Already decided: no further code. No further pull request. |
| 6 | Stays `v1alpha1` for this refresh. Promotion is pull request F, after the upgrade. |
| 8 | Pull request B. Lookups for payment method, plan, and region. |
| 9 | Pull request C. Workflow that opens a pull request when the Terraform provider releases. |

Pull request A, the extra tests for gaps that are already finished, is optional. It is not required to call the alignment doc complete.

## Problem

`docs/terraform-alignment.md` records nine gaps against Terraform Redis Cloud v2.19.2. Five are already fixed on the alignment branch, two are accepted limits, and two still need code (lookups, and a job that tracks later Terraform provider releases).

The provider line around those resources is old. Re-read on 28 October 2026 from the Upjet v2.5.1 `go.mod`, GitHub releases, the Crossplane release table, and endoflife.date. EOL below means the publisher stopped supporting that release. A blank EOL means no end date was published.

| Component | On main | EOL | This move uses | GA? | Latest GA, 28 October 2026 |
| --- | --- | --- | --- | --- | --- |
| Go | 1.24 | EOL 10 February 2026 | 1.26.8, the version Upjet v2.5.1 declares | GA | 1.27.3. Go 1.26 is also supported; its latest patch is 1.26.10 |
| Crossplane | 1.16.0 | EOL February 2025. Last patch v1.16.5, 18 December 2024 | 2.4 line. Upjet requires Crossplane APIs v2.4.1 | GA | v2.4.3, 22 October 2026. Maintained minors are 1.20, 2.2, 2.3, and 2.4 |
| Upjet | v1.4.1 | No EOL date. Latest v1 release is v1.11.2, 9 September 2026 | v2.5.1 (`github.com/crossplane/upjet/v2`) | GA, 30 September 2026 | v2.5.1 |
| crossplane-runtime | v1.16.0 | Same retired line as Crossplane 1.16 | v2.4.0, required by Upjet v2.5.1 | GA | v2.4.3, 22 October 2026 |
| controller-runtime | v0.17.0 | No EOL date. It belongs to the Kubernetes 1.29 era, which is EOL | v0.23.3, required by Upjet v2.5.1 | GA, 5 March 2026 | v0.25.2, 1 October 2026 |
| controller-tools | v0.14.0 | No EOL date. Upjet v2.5.1 requires v0.20.0 | v0.20.0. Latest GA v0.23.0 pulls Kubernetes clients v0.37, which leaves the Upjet client set | GA | v0.23.0 |
| Kubernetes client libraries | v0.29.1 | EOL 28 February 2025. That is Kubernetes 1.29 | v0.35.3, required by Upjet v2.5.1. Kubernetes 1.35 is supported until 28 December 2026. crossplane-tools is the 19 July 2026 commit before it required Kubernetes 0.36, so this build stays on v0.35.3 | GA | Kubernetes 1.37.2, client libraries v0.37.2, 26 August 2026 |
| golangci-lint | v2.2.2 in CI. Makefile default 1.54.0 | No EOL date. Last v1 release seen is v1.64.8, 17 March 2025 | v2.15.0 | GA, 16 October 2026 | v2.15.0 |
| kind | v0.15.0 | No EOL date | v0.33.0 | GA, 26 August 2026 | v0.33.0 |
| uptest | v0.5.0 | No EOL date | `v0.12.1-0.20260728095952-c230a8044006`, required by Upjet v2.5.1 | Pre-release commit, not a release tag | v0.12.0, 20 September 2024, on `github.com/upbound/uptest` |
| Crossplane CLI | `up` CLI v0.39.0 | No EOL date found for `up`. The current build scripts no longer download it | v2.4.1 from cli.crossplane.io. Core 2.4.3 has no CLI release; v2.4.1 is the latest CLI on the 2.4 line | GA | v2.5.0, 21 August 2026 |
| Terraform CLI | 1.5.7 | EOL 17 January 2024 | Stays 1.5.7. The Makefile rejects 1.6 and newer because that license is BSL | 1.5.7 is the last GA patch of the 1.5 line | 1.16.7 |
| Terraform Redis Cloud provider | 2.19.2 | No EOL date | Stays on the release-tracking workflow. This line move does not bump it | 2.19.2 is GA | v2.19.3, 28 October 2026 |

Upjet v2.5.1 does not compile against the v1.4.1 module set. Its `go.mod` requires Go 1.26.8, crossplane-runtime v2.4.0, Crossplane APIs v2.4.1, controller-runtime v0.23.3, and Kubernetes clients v0.35.3. Those five move because they are one build. Go 1.24, Crossplane 1.16, and Kubernetes 1.29 are already past end of support, so staying on them also means no security patches. The tool versions move so generate and CI match that line. Terraform CLI stays 1.5.7 despite being past end of support. The Redis Cloud provider bump stays in the release-tracking workflow.

The kind cluster used for the live proof already runs a current Crossplane. The provider under test is still built as a 1.16 provider. That run is evidence the current build can reconcile there. It is not a supported-line change.

## Goals

- Every gap in `docs/terraform-alignment.md` is either closed in code or closed as an accepted limit with a test that guards the limit.
- Go, Upjet, Crossplane, crossplane-runtime, controller-runtime, controller-tools, the Kubernetes libraries, kind, uptest, the `up` CLI, golangci-lint, and both GitHub workflows agree on one current line.
- The 30-minute controller timeout and the Active-Active TLS `initProvider` fix survive regeneration.
- A later Terraform Redis Cloud release opens a pull request instead of waiting for someone to notice the tag.

## Non-goals

- Terraform CLI past 1.5.7.
- A Redis package registry, CODEOWNERS, or a publish pipeline.
- Turning all 29 Terraform data sources into CRDs. Lookups cover payment method, plan, and region.
- A second writer for the database password. The sensitive-field mapping already owns that key.
- A Crossplane reference into a numeric field. Database `subscription_id` is a number. Essentials `plan_id` and Essentials `payment_method_id` are numbers. Crossplane references write strings.
- Changing pull request #2 as part of writing this plan.

## The gate on every step

"The QA Redis Cloud account" means the non-production account already used for the live ACL, Pro subscription, and Database proof. Credentials stay outside the repository. The suite runs from the existing test cluster. It does not create a second kind cluster.

Run creates only while that account is accepting creates. Delete what the step created. Confirm the Cloud object is gone before the pull request merges. Do not keep retrying a delete that Cloud has already rejected.

### Exit criteria, all of them

1. GitHub CI on the pull request is green: lint, unit tests, check-diff, check-examples, local-deploy, and publish-artifacts.
2. The QA suite below is green, including any check the step adds.
3. Bugbot has reviewed the pull request. Every finding is fixed, or answered in a thread that is resolved. No Bugbot finding stays open.
4. The pull request has no known bug left for a later step.
5. The pull request is merged to `main`.
6. The next step branches from that `main`.

If any line fails, stay on the same pull request, fix it, and run the whole gate again. Do not open the next step from a red branch.

A step whose exit criteria already pass on `main` does not get an empty pull request. Record that in `docs/terraform-alignment.md` and run the QA suite once on `main` before continuing.

### QA suite

Against the QA Redis Cloud account, on the provider built from the pull request:

- ACL rule: create, Ready, the rule exists in Cloud, delete, the rule is gone from Cloud.
- Pro subscription: create, Ready, Cloud status active, delete, Cloud returns 404.
- Database on that subscription: copy `status.atProvider.id` into `subscriptionId` by hand, Ready, Cloud status active, delete the database, Cloud returns 404, then delete the subscription, Cloud returns 404.
- Connection secret for that database contains `public_endpoint` and `private_endpoint`. `port` is not required. When Terraform state already has `port`, that key is still published. Do not print secret values. Confirm the key names only.
- `python3 scripts/check-examples.py package/crds examples` passes.
- `go test` for the packages the step touched passes, and `make test` passes in CI.

Once repository admin can store `UPTEST_CLOUD_CREDENTIALS`, also comment `/test-examples="examples/rediscloud/acl-rule.yaml"` and require the `Uptest-…` check to pass. The laptop suite stays in the gate either way. The comment author needs write or admin on `RedisLabs/crossplane-provider-rediscloud`.

## How a pull request is executed

1. Branch from current `main`.
2. Make the change for that pull request. Pull request A includes A1–A7 before the suite runs.
3. Open the pull request. The description links this plan and names the letter.
4. Run the gate once.
5. On failure, fix, re-run the gate, repeat until every exit line passes.
6. Merge.
7. Start the next pull request from that `main`.

## What shares a pull request

The code for gaps 1, 2, 4, 5, and 7 is already on pull request #2. What remains for those, plus the accepted limits on gaps 3 and 4, is tests and doc status. That is one pull request. One QA suite covers all of it. Splitting it into six pull requests would repeat the same Cloud create and delete for a review that is tests.

These stay separate pull requests:

- Gap 8, the three lookups. That is a new API. A failure there must not block the tests for the gaps that are already fixed.
- Gap 9, the release-tracking workflow. It does not depend on the lookups. It can open once pull request A has merged.
- The Crossplane line move. It regenerates the tree. Mixing it with the tests for the finished gaps makes rollback one large revert.
- The `v1beta1` promotion. It waits until that line is green.

## Part A — gaps

The alignment doc is updated in the pull request that closes each item.

### Pull request A. Tests for the gaps that are already finished

One pull request covers steps A1 through A6. Run the QA suite once, at the end. Bugbot once. Merge once.

### Step A1. Make the QA suite the proof for gap 7

Gap 7 is already proved by hand. This step turns that proof into the checklist above and wires the ACL example to the GitHub end-to-end comment.

- Change: document the suite in `docs/CI-TESTING.md`. Point the end-to-end example list at `examples/rediscloud/acl-rule.yaml` for the automated run. Leave subscription and database as laptop steps until their examples can run without a pasted account id.
- Exit criteria: one green laptop run of the full QA suite on `main` after pull request #2 has merged, and a written suite that later steps cite. The GitHub comment run is required only after the Actions secret exists.
- Test: unit checks for A2–A6 land in this same pull request. The QA suite runs once, after those checks, and covers A1–A7 together. A failed ACL delete that leaves the rule in Cloud fails the pull request.

### Step A2. Gap 1, examples match the CRDs

Already fixed: `scripts/check-examples.py` rejects unknown `forProvider` fields, and 13 of 13 examples pass.

- Change: a CI or unit check that fails when an example uses a field the CRD does not have. Skip the pull request if `make check-examples` already fails that way on `main`.
- Exit criteria: adding an unknown `forProvider` field to one example makes the check fail. Restoring the example makes it pass.
- Test: that negative check. The QA suite is the one run at the end of pull request A.

### Step A3. Gap 2, 30-minute controller deadline

Already fixed in `scripts/fix_generated_types.py` for all 27 controllers.

- Change: a test that reads the generated controllers and fails unless each one contains `30 * time.Minute` and none contain the old `3 * time.Minute` deadline.
- Exit criteria: the test fails if one controller is reverted to 3 minutes, and passes on this branch.
- Test: `go test` for that package. The Pro subscription must reach Ready in the one QA suite for pull request A. A timeout cancellation fails that pull request.

### Step A4. Gap 5, Active-Active TLS init type

Already fixed: `initProvider.clientTlsCertificatesSecretRef` is a secret-reference list.

- Change: a test on the generated type that fails if that field is no longer a secret-reference list.
- Exit criteria: the test passes, and a deliberately wrong type fails it.
- Test: `go test`, and `make generate` still applies the fixer. The QA suite is the one run at the end of pull request A.

### Step A5. Close gap 3 as an accepted limit

Pro and Essentials databases store `subscription_id` as a number (`*float64`). Crossplane references write strings. String children already have `subscriptionIdRef`.

- Change: no reference field on the database. Add a test that the database type has no `subscriptionIdRef`, and that string children still have it. The database example keeps the comment that the numeric id is copied from `status.atProvider.id`.
- Exit criteria: the test passes. The QA Database custom resource becomes Ready only after that hand copy. The alignment doc marks the gap accepted, with **Plan:** No fix.
- Test: the unit test. The database half of the QA suite is the one run at the end of pull request A.

### Step A6. Close gap 4 as an accepted limit

`databaseConnectionDetails` publishes `public_endpoint` and `private_endpoint`. The password stays on the sensitive-field mapping. `port` is published only when Terraform state already has it. The 27 October 2026 QA secret had the two endpoint keys and no `port`.

- Change: a test that the live observation (both endpoints set, `port` absent) publishes those two keys and not `password`, and that a `port` already in Terraform state is still published. No second password writer.
- Exit criteria: the test passes. The live database secret has `public_endpoint` and `private_endpoint`. The password value is never printed.
- Test: the unit test. The database half of the QA suite is the one run at the end of pull request A.

### Pull request B. Gap 8, lookups for payment method, plan, and region

Starts after pull request A is merged.

29 Terraform data sources stay without CRDs. This step adds three lookups.

Field types on the current CRDs:

- Pro and Active-Active `paymentMethodId` are strings, so a lookup status can be referenced.
- Essentials `paymentMethodId` and Essentials `planId` are numbers. The lookup still returns the id in status. The Essentials subscription copies the number by hand, same limit as gap 3.

- Change: three read-only objects, one each for payment method, Essentials plan, and region. Each lists what the Terraform data source would list and writes the id to status. Pro subscription YAML in the test can take the payment-method id from the lookup. The other 26 data sources stay pasted ids. Update the alignment doc: **Fix status:** ✅ for those three, **Plan:** No fix for the rest.
- Exit criteria: on the QA account, a payment-method lookup becomes Ready and its status id is the id the Pro subscription then uses. A region lookup returns the region used by that subscription. An Essentials plan lookup returns a plan id. No payment-method id is committed in an example.
- Test: those three lookups on the QA account, then the full QA suite. Delete any subscription the test created.

### Pull request C. Gap 9, track later Terraform provider releases

Starts after pull request A is merged. It does not wait for the lookups.

- Change: a workflow that opens a pull request when `RedisLabs/terraform-provider-rediscloud` publishes a release newer than `TERRAFORM_PROVIDER_VERSION`. The workflow changes the pin and regenerates. It does not merge itself. It does not bump Crossplane, Go, or Upjet.
- Exit criteria: a dry run against the current 2.19.2 tag opens nothing. A fixture tag newer than the pin opens a pull request that changes the Makefile pin. The alignment doc marks the gap fixed.
- Test: the dry run, CI on the workflow pull request, and the QA suite on that pull request before it merges. Regeneration must keep the 30-minute timeout.

### Step A7. Record gap 6 inside pull request A

Managed APIs are `v1alpha1`. ProviderConfig is `v1beta1`. Promoting on the Crossplane 1.16 line and then moving to Upjet v2 would break clients twice.

- Change: this is a decision in pull request A, not its own pull request and not a promotion. The alignment doc says promotion is pull request F, after the line upgrade. No CRD version changes here.
- Exit criteria: the alignment doc names pull request F as the promotion, and `main` still serves `v1alpha1`.
- Test: the one QA suite for pull request A. Schema version is unchanged, so a red suite here is a regression, not a migration.

## Part B — components

Do these after pull request A. Lookups and the release workflow do not have to merge first. Re-read the Upjet module on the day pull request E starts:

https://pkg.go.dev/github.com/crossplane/upjet/v2

Copy that module's Go version and its required Crossplane, crossplane-runtime, controller-runtime, and Kubernetes versions. Use the latest stable provider-upjet release only to fill tools that Upjet does not pin (kind, golangci-lint, controller-tools) when the Upjet module is silent.

### Pull request D. One Go version in CI, still on the current line

- Change: `.github/workflows/uptest-trigger.yaml` uses the same `GO_VERSION` as `.github/workflows/ci.yml`. The Makefile `GO_REQUIRED_VERSION` matches that version. No Upjet bump in this step.
- Exit criteria: both workflows say the same Go version. `make generate`, `make test`, and the provider binary still build.
- Test: CI and the QA suite.
- Stop rule: if a newer Go than 1.24 fails `make generate` on Upjet v1.4.1, leave Go at 1.24 in this pull request and carry the Go bump into pull request E. That is a completed pull request, not a failed one. Record the generate error in the pull request.

### Pull request E. Move the Crossplane line

One pull request. These move together, because Upjet v2 does not build on the v1.4.1 module set:

- Upjet v1.4.1 to the current `github.com/crossplane/upjet/v2` release
- crossplane-runtime v1.16.0 to the version that Upjet requires
- `CROSSPLANE_VERSION` 1.16.0 to the Crossplane release that matches that runtime
- controller-runtime, controller-tools, and the Kubernetes client libraries to the Upjet module
- Go to the version that Upjet module declares, or the newer patch that still builds it
- kind, uptest, `up`, golangci-lint, and the build submodule to the versions that the chosen Upjet line uses
- CI `GO_VERSION` and `GOLANGCI_VERSION` to those same values

These stay:

- Terraform CLI 1.5.7
- Terraform Redis Cloud provider 2.19.2, unless pull request C has already merged a newer pin
- The 30-minute timeout rewrite and the TLS `initProvider` fixer
- Managed API `v1alpha1` (promotion is pull request F)
- ProviderConfig `v1beta1`, unless the Upjet v2 generator requires a new package version. If it does, that change is part of this pull request and the description says so.

Write the ADR in this pull request. The decision is: follow the current Upjet v2 module rather than staying on Upjet v1.4.1. The alternative is a Go-only bump, which pull request D already tried.

- Exit criteria: `make generate` reports the same 27 managed resources. The six resources added after Terraform v2.1.5 are still present. The timeout test from pull request A and the TLS test from pull request A pass. The provider becomes Healthy on the test cluster and the QA suite passes on that build. `govulncheck` is run; every finding is either fixed in this pull request or listed with the upstream module that still contains it.
- Test: generate, unit tests, example check, CI, then the full QA suite on the new binary. An ACL, subscription, or database that does not reach Ready fails the step.
- Rollback: revert the merge. The previous `main` is the Crossplane 1.16 provider. Clusters already on Crossplane 2.4.2 keep running; they install the reverted package.

### Pull request F. Promote managed APIs, closing gap 6

Do this only on the line from pull request E.

- Change: managed resources move from `v1alpha1` to `v1beta1` across all 27 kinds. Examples, the alignment doc, and the README follow. Stored objects need a conversion strategy, or the pull request states that `v1alpha1` is removed and lists the kinds. ProviderConfig stays `v1beta1` if step 11 left it there.
- Exit criteria: every example uses `v1beta1`. `check-examples` passes. The QA suite creates the ACL rule, Pro subscription, and Database as `v1beta1` and deletes them. The alignment doc marks gap 6 fixed.
- Test: the QA suite on `v1beta1` manifests. A resource that only reconciles as `v1alpha1` fails the step.

### Pull request G. Vulnerability and tool drift check

- Change: `govulncheck` in CI with the Go version from pull request E. Dependabot or a scheduled workflow for Go modules, GitHub Actions, and the tool versions in the Makefile. Terraform provider bumps stay in pull request C so a provider release still regenerates CRDs.
- Exit criteria: CI fails on a new fixable vulnerability in a module this repository can bump without leaving the Upjet line. The first run's report is attached to the pull request.
- Test: CI, plus one QA suite run so the tool bump did not change the running provider.

## Order

| Pull request | What it contains | Starts when |
| --- | --- | --- |
| A | QA suite, tests for gaps 1–5 and 7, accepted limits for gaps 3 and 4, gap 6 deferred in the doc | Pull request #2 is merged |
| B | Three lookups (gap 8) | A is merged |
| C | Terraform provider release workflow (gap 9) | A is merged. Does not wait for B |
| D | One Go version on the current line | A is merged. Does not wait for B or C |
| E | Upjet v2 line, with an ADR | D is merged |
| F | `v1beta1` managed APIs (gap 6) | E is merged and the suite is green |
| G | `govulncheck` and dependency updates | F is merged |

## Security and operational impact

Credentials for the QA suite stay in the GitHub Actions secret `UPTEST_CLOUD_CREDENTIALS` or in the local file outside the repo. Examples do not gain account ids, payment-method ids, or keys. The lookup objects return ids to status on a cluster that already holds the provider credentials.

The line upgrade changes the CRDs and the controller binary. Install it on the test cluster before any other cluster. There is no production rollout in this plan.

## Validation

Each step under "How to finish this activity" has its own check, and that check passes before the pull request merges. The activity is finished only when "Verify the finished activity" passes on `main` after the third pull request.

## Rollout and rollback

Each merge is the rollout for this repository. Rollback of a step is reverting that merge. Do not start the next step in order to fix the previous one.

## Open questions

None for the sequence. Pull request E re-reads exact module versions on the day it starts, because the 23 October 2026 notes already disagree by a patch on controller-runtime and Go.
