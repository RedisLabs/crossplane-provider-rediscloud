# Testing gate

Date: 28 October 2026.
Status: In progress. Steps 1 and 2 are on main. Step 3 adds `ci-success`. The ruleset is applied after that pull request merges and `main` has one green `ci-success` run.
Owner: Shay Berman.
Ticket: none.
Design: this file.
ADR: none. The rejected approaches are recorded here.
BDR: none.

## TLDR

Three pull requests. The first makes `make test` fail on the subscription example that Redis Cloud rejected, and stops promising a `port` secret key the API does not return. The second adds a nightly script on `main` that creates and deletes one ACL rule, one Pro subscription, and one database on the QA account. The third makes the existing CI jobs plus the first pull request's tests a required check. The nightly script is not a required check.

## Problem

CI on `main` can be red and the pull request can still merge: `main` has no branch protection and no ruleset. The jobs that do run never call Redis Cloud. `scripts/check-examples.py` checks the example against the CRD schema. On 27 October 2026 that check passed for `examples/rediscloud/subscription.yaml`, and the QA account then rejected the same manifest three times: `PAYMENT_INFO_NOT_PROVIDED`, `DATABASE_SIZE_LIMIT_NOT_DEFINED`, and `DATABASE_MODULE_NOT_SUPPORTING_SHARD_THROUGHPUT_OPTION`.

The database became Ready. Its connection secret contained `public_endpoint` and `private_endpoint`. `status.atProvider.port` was empty. In `config/schema.json`, the endpoints are computed and `port` is an optional input, so Terraform leaves it empty unless the manifest sets it. Before step 1, `docs/terraform-alignment.md` said the secret publishes `port`.

`uptest` cannot express the database half of that suite. `subscriptionId` is a number. The database manifest cannot be written until the subscription id exists.

## Goals

- A pull request that leaves the subscription example in the shape Redis rejected fails `make test`.
- The connection-secret test uses the observation shape from the live run: both endpoints set, `port` absent.
- The written QA contract lists the keys the live secret actually had.
- A script on `main` can repeat the live create and delete, including Cloud status, secret key names, and HTTP 404 after delete.
- After the third pull request, a failing CI job blocks merge.

## Non-goals

- A fake Redis Cloud HTTP API. It would only return errors we typed, and it would miss combination rules such as `number-of-shards` without RediSearch.
- Parsing a port out of the endpoint string. The live endpoint format was not saved, and Active-Active has one `port` for many regions.
- A second grep for `3 * time.Minute`. `make check-diff` already runs `make generate`, and `scripts/fix_generated_types.py` rewrites that timeout.
- A skip file that classifies all 27 resources. A reason line is not a test.
- Making the nightly Cloud run a required pull-request check. A failure the next morning does not unmerge.
- Running peering, transit gateway, private link, or Active-Active. Those need an AWS or GCP account.
- Putting the QA payment method id, API keys, or secret values in the repository or in a log.
- Requiring Codecov. Coverage of generated `zz_` files does not measure behavior.

## Decisions

`Subscription.GetParameters()` is the seam for the example. It turns the manifest into the Terraform map the controller would send. That catches a missing field. It does not prove Redis will accept the combination. The nightly script is the only check that proves acceptance, and it runs after merge.

The nightly workflow triggers on `schedule` and `workflow_dispatch` only. It does not trigger on `pull_request`. The QA credential is available to that workflow on `main` and nowhere else.

The database step is a script. It waits until the subscription is active, reads `status.atProvider.id`, and only then creates the database. `uptest` stays the optional comment path for networking examples. It is not this suite.

## Steps

Do them in order. A later step branches from `main` after the earlier pull request is merged.

### Step 1. Example and connection-secret tests

One pull request. No Redis Cloud account. No new workflow.

What it changes:

- A Go test loads `examples/rediscloud/subscription.yaml` and calls `GetParameters()`.
- The subscription example is updated so that test passes. `paymentMethodId` stays a placeholder string. The real QA id stays out of git.
- `TestDatabaseConnectionDetails` gains the live input: `public_endpoint` and `private_endpoint` set, `port` absent. The secret keys are `public_endpoint` and `private_endpoint`.
- `docs/terraform-alignment.md` stops saying the secret contains `port`. The published keys are `public_endpoint` and `private_endpoint`. When Terraform state already has `port`, that key is still published.

#### Exit criteria

- `GetParameters()` on the subscription example includes `payment_method_id`, one of `dataset_size_in_gb` or `memory_limit_in_gb`, and `throughput_measurement_by` equal to `operations-per-second`.
- The same test fails if those fields are removed from the example.
- The connection-details function, given the live input, returns `public_endpoint` and `private_endpoint` and does not return `port` or `password`.
- The existing case, where Terraform already set `port`, still publishes that `port`.
- `docs/terraform-alignment.md` names `public_endpoint` and `private_endpoint` as the keys the live secret had. It still says a `port` value already present in Terraform state is published.

#### How to verify

1. On a temporary copy of the current example, run the new test and confirm it fails for the missing payment method id, the missing size, and `number-of-shards`.
2. Restore the updated example and run `make test`. It passes, including the new assertions and the existing 22 release-tracking tests.
3. Read the connection-details test and confirm one case omits `port` and one case sets `port`.
4. Search `docs/terraform-alignment.md`. The live secret is no longer required to contain `port`. A `port` that Terraform already returned is still documented as published.

This step does not call Redis Cloud. A green `make test` does not mean the API accepts the example.

#### When this pull request can merge

Merge when all of these are true:

- `make test` passes on the pull request head.
- The CI jobs that already run on pull requests are green: lint, check-diff, unit-tests, local-deploy, check-examples, report-breaking-changes, and publish-artifacts.
- The diff contains the example test, the live connection-details case, the example update, and the doc change. It contains no credential, no real payment method id, and no workflow file.
- Review comments are resolved.

Do not wait for the nightly script. Do not run `/test-examples`.

### Step 2. Nightly QA script

One pull request, branched from `main` after step 1 is merged.

What it changes:

- A script creates the QA objects, checks them, and deletes them.
- `.github/workflows/` gains one workflow whose only triggers are `schedule` and `workflow_dispatch`.
- Hermetic tests cover the script's safety decisions with a fake API or a stubbed command runner. They do not call Redis Cloud.

The live order is fixed:

1. Refuse to start when any subscription name begins with `xp-ci-`.
2. Run the three lookups. Each must become Ready. They only read.
3. Create one ACL rule named `xp-ci-<run id>`, wait until Ready, confirm Cloud status `active` and the rule text, delete it, and confirm that name is gone.
4. Create one Pro subscription named `xp-ci-<run id>` with a fresh CIDR, `paymentMethodId` from the lookup, a dataset size, and `operations-per-second`. Wait until Cloud status is `active` and the Crossplane resource is Ready.
5. Copy `status.atProvider.id` into the database `subscriptionId`. Create one database. Wait until Cloud status is `active` and Ready.
6. Read the connection secret key names. Require `public_endpoint` and `private_endpoint`. Do not print values. `port` is not required.
7. Delete the database. The database request returns HTTP 404.
8. Delete the subscription. The subscription request returns HTTP 404.

The workflow passes the QA credential only from the `UPTEST_CLOUD_CREDENTIALS` secret. The script deletes the subscription and the database by name when a later step fails. It deletes them again at the start of the next run if a name with the prefix is still present. It will not create a second `xp-ci-` subscription while one exists.

Peering, transit gateway, private link, and Active-Active are not in the script.

#### Exit criteria

- The workflow file has no `pull_request` trigger.
- A hermetic test proves the script exits before create when an `xp-ci-` subscription is already present.
- A hermetic test proves a failed database step still issues the subscription delete.
- A hermetic test proves the script's success check requires Cloud `active`, the two secret key names, and HTTP 404 after both deletes.
- Logs and test output contain no credential and no secret value.
- One local run against the QA account finishes the live order and leaves the account with no `xp-ci-` subscription.

#### How to verify

1. Run the script's unit tests. Confirm the "already exists" case does not call create, and the failed database case does call delete.
2. Inspect the workflow YAML. The `on` keys are `schedule` and `workflow_dispatch` only. The credential env var is the secret reference, not a literal.
3. Run the script once from a workstation against the QA account, with the credential file outside the repo. Record the result as: lookups Ready, ACL created and gone, subscription Cloud `active` then HTTP 404, database Cloud `active` then HTTP 404, secret keys `public_endpoint` and `private_endpoint`.
4. After that run, list subscriptions and confirm none are named `xp-ci-*`.
5. On the pull request, the existing CI jobs are green, including `make test`.

The GitHub secret must exist before merge. A schedule with a missing secret would fail every night. Confirm the secret name is present in the repository settings. Do not print its value. The first GitHub run happens after merge, from Actions → Run workflow.

#### When this pull request can merge

Merge when all of these are true:

- The hermetic script tests pass in CI.
- The local QA run in step 3 of verification succeeded, and the account has no `xp-ci-` subscription afterward.
- `UPTEST_CLOUD_CREDENTIALS` exists in the repository secrets.
- The workflow cannot run on a pull request.
- Review comments are resolved.

Do not add this workflow as a required status check. After merge, run it once from Actions → Run workflow and confirm that run is green. A red scheduled run is fixed with a follow-up pull request. It does not block other pull requests.

Run this script by hand before merging a later pull request that changes subscription or database behavior. That run is a review step for those pull requests, not a GitHub required check.

### Step 3. Required check on main

One pull request, branched from `main` after step 2 is merged. The repository setting is applied after this pull request merges, not inside it.

What it changes:

- CI gains one job, `ci-success`. It depends on detect-noop, lint, check-diff, unit-tests, local-deploy, check-examples, report-breaking-changes, and publish-artifacts.
- `detect-noop` must be `success`. `ci-success` fails when any needed result is `failure` or `cancelled`. A `skipped` result on the other jobs, which is what a docs-only pull request produces, is success. A failed `detect-noop` fails `ci-success` even when those jobs were skipped.
- It does not depend on the nightly workflow.

#### Exit criteria

- A pull request with a failing unit test makes `ci-success` fail.
- A docs-only pull request, where detect-noop succeeded and the other jobs are skipped, makes `ci-success` succeed.
- A failed detect-noop makes `ci-success` fail.
- `ci-success` has no dependency on the nightly workflow or on an `Uptest-` status.

#### How to verify

1. Read the job's `needs` list and the result condition. Confirm `detect-noop` must be `success`, `failure` and `cancelled` fail the job, and `skipped` on the other jobs does not.
2. On this pull request, wait until `ci-success` itself is green.
3. After merge, confirm `main` has one green `ci-success` run so the check name exists.
4. Add a ruleset on `main` whose only required status check is `ci-success`. Do not require the nightly workflow, Codecov, or `Uptest-`.

The ruleset is a repository setting. It is not part of the diff. Apply it only after step 3 of verification, so the pull request that creates the check is not blocked by a check name GitHub has not seen yet.

#### When this pull request can merge

Merge when all of these are true:

- `ci-success` is green on the pull request.
- The other CI jobs are green, or skipped because the diff is docs-only. This diff is a workflow, so they should be green.
- The job does not require the nightly script.
- Review comments are resolved.

Enable the ruleset after that merge, not before.

## Security and operational impact

The QA API credential stays in the GitHub secret `UPTEST_CLOUD_CREDENTIALS` and in the workstation file outside the repo. Step 2 is the only workflow that reads it. A pull request from a fork cannot start that workflow.

The script may hold one Pro subscription for the length of the run, which on 27 October 2026 was under 20 minutes for create and under 5 minutes for delete. It refuses to create another while one `xp-ci-` subscription exists. A crashed runner is cleaned by the next run's refuse-and-delete preamble, once someone starts that run. The schedule is that next run.

Logs print status codes, Ready conditions, and secret key names. They do not print secret values, the API key, or the payment method id.

## Rollout

Merge step 1, then step 2, then step 3, then enable the ruleset. Rollback of step 1 or step 2 is reverting that merge. Rollback of the ruleset is deleting the ruleset. Reverting step 3's workflow without deleting the ruleset blocks merges, because `ci-success` would disappear while still required. Delete the ruleset first.

## Open questions

- The cron hour is `17 6 * * *` (06:17 UTC). `.github/workflows/terraform-release.yaml` runs at 04:17 UTC. The two hours between them keep the jobs from overlapping.
- Step 1's `GetParameters()` test will not notice a new Redis combination rule. The hand run of the step 2 script before a subscription or database merge is what notices it.
