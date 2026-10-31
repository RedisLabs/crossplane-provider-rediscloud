# CI and Testing Infrastructure

This document describes the CI/CD pipeline and testing infrastructure for the Crossplane Provider RedisCloud. The suite names, when each suite runs, and which cases the Terraform provider already has are in [design/test-suites.md](design/test-suites.md).

## Automatic tests

CI runs on every pull request and does not call Redis Cloud. Nightly QA calls the QA Redis Cloud account on a schedule. Uptest runs only when someone with write access asks for it on a pull request.

### CI

Name: workflow `.github/workflows/ci.yml`. The required check on `main` is the job `ci-success`.

It runs on every pull request and on pushes to `main`. It does not call Redis Cloud. A documentation-only pull request skips the jobs below, and `ci-success` still passes when `detect-noop` succeeded.

| Job | What it checks |
| --- | --- |
| `detect-noop` | Documentation-only diffs skip the rest of CI. |
| `lint` | `golangci-lint`. |
| `check-diff` | Generated code matches `make generate`. |
| `unit-tests` | `make test`. The cases are listed below. |
| `local-deploy` | The provider package installs on a local cluster. |
| `check-examples` | Example YAML matches the CRDs. It does not call Redis Cloud. |
| `report-breaking-changes` | CRD OpenAPI changes that break existing manifests. |
| `publish-artifacts` | The provider package builds. |
| `govulncheck` | Called code is affected by a known vulnerability. The report is the `govulncheck` artifact. |
| `ci-success` | `detect-noop` succeeded, and no needed job failed or was cancelled. A skipped job on a docs-only pull request counts as success. |

`make test` cases, all without Redis Cloud:

- All 27 managed resources are registered, including the six added after Terraform v2.1.5, and each of those six references a subscription.
- Controller timeout stays 30 minutes.
- Active-Active `clientTlsCertificatesSecretRef` stays a secret-reference list.
- Database types have no `subscriptionIdRef`. String children, such as peering, keep that reference.
- The Pro subscription example includes `payment_method_id`, a dataset size, and `throughput_measurement_by` of `operations-per-second`. The shape Redis Cloud rejected on 27 October 2026 fails this test.
- A database connection secret publishes `public_endpoint` and `private_endpoint`. `port` is published only when Terraform already returned it.
- An omitted `enablePasswordless` sends no password key. An explicit `true` sends only `enable_passwordless`.
- Payment method, Essentials plan, and region lookups match an id. The payment-method reconciler writes that id and hides HTML error bodies.
- `scripts/test_qa_live.py` checks the Nightly QA script with a fake API: it refuses to create while an `xp-ci-` subscription exists, deletes the subscription when the database step fails, requires Cloud `active`, the two secret key names, and HTTP 404 after delete, and the workflow file has no pull-request trigger.
- `scripts/test_ci_success.py` checks that a failed or cancelled job fails `ci-success`, a skipped job does not, and `ci-success` does not depend on Nightly QA or uptest.
- `scripts/test_alignment_guards.py` rejects an unknown `forProvider` field and checks that generation keeps both served versions and that workflows use Go 1.26.
- `scripts/test_track_terraform_release.py` checks the release-tracking workflow: the same pin opens nothing, a newer tag would bump only `TERRAFORM_PROVIDER_VERSION`, a 3 minute timeout does not open a pull request, and the generate job does not see a GitHub token.

A coverage file is uploaded to Codecov from `unit-tests`. That upload does not fail the job. The ruleset on `main` does not require Codecov. `codecov.yml` ignores generated `zz_` files.

### Nightly QA

Name: workflow `.github/workflows/qa-live.yaml`. The script is `scripts/qa_live.py`.

It runs at 06:17 UTC, and from Actions → Run workflow. It does not run on pull requests, and it is not a required check. It builds the provider on a kind cluster and uses the `UPTEST_CLOUD_CREDENTIALS` secret. Logs print status codes and secret key names. They do not print secret values.

Live cases, in order, on the QA Redis Cloud account:

1. Refuse to start when any subscription name begins with `xp-ci-`.
2. Payment method, Essentials plan, and region lookups each become Ready. They only read.
3. Create one ACL rule named `xp-ci-<run id>` with rule text `+@read ~*`. Wait until Ready, confirm Cloud status `active`, delete it, and confirm that name is gone.
4. Create one Pro subscription named `xp-ci-<run id>` with a fresh CIDR, `paymentMethodId` from the lookup, a dataset size, and `operations-per-second`. Wait until Cloud status is `active` and the Crossplane resource is Ready.
5. Copy `status.atProvider.id` into the database `subscriptionId`. Create one database. Wait until Cloud status is `active` and Ready.
6. Read the connection secret key names. Require `public_endpoint` and `private_endpoint`. `port` is not required.
7. Delete the database. The database request returns HTTP 404.
8. Delete the subscription. The subscription request returns HTTP 404.

A failed later step still deletes the subscription and the database. The next run deletes a leftover `xp-ci-` name before it creates another. Peering, transit gateway, private link, Private Service Connect, Active-Active, and Essentials are not in this script.

### Uptest

Uptest is the Crossplane end-to-end runner from [crossplane/uptest](https://github.com/crossplane/uptest). In this repository the workflow is `.github/workflows/uptest-trigger.yaml`, and `make e2e` is the same runner locally.

What the name means: apply the YAML you name, wait until each object is Ready, then delete it and check that it is gone. A failure saves a cluster dump.

Why it is here:

- Nightly QA always runs the same three objects: one ACL rule, one Pro subscription, and one database.
- A pull request that changes a different example can ask Uptest to apply that example on a real cluster.
- Uptest is not a required check. `ci-success` does not wait for it. A merge does not need an `Uptest-` status.

How a run starts:

- Someone with write or admin permission comments on the pull request: `/test-examples="examples/rediscloud"`.
- The workflow builds a kind cluster, installs this provider, and passes `UPTEST_CLOUD_CREDENTIALS` into `make e2e`.
- The comment's path selects the YAML files. One file, or every `*.yaml` under a directory.

What a passing run verifies, for the files in that path:

- The provider installs.
- Each manifest is accepted by the API server.
- The controller reaches Ready, which means Terraform finished the create against Redis Cloud.
- Delete removes the object.

What each example is for, and what blocks a real pass:

- `examples/rediscloud/acl-rule.yaml` creates one ACL rule. This is the case that can pass with only the QA Redis Cloud credential. Nightly QA covers the same case on a schedule.
- `examples/lookup/paymentmethod.yaml`, `essentialsplan.yaml`, and `regions.yaml` read an id and become Ready. Delete does not change the Redis Cloud account.
- `examples/rediscloud/subscription.yaml` is a Pro subscription. The payment method id in git is the placeholder `example-payment-method-id`, so Redis Cloud rejects it until that value is a real id.
- `examples/rediscloud/database.yaml` is a Pro database. `subscriptionId` is the placeholder number `123456`. Uptest cannot wait for a subscription and then copy `status.atProvider.id`, because that field is a number. Nightly QA does that copy in `scripts/qa_live.py`.
- `examples/rediscloud/acl-user.yaml` creates an ACL user. The password comes from a Secret named `redis-user-password`, which the example does not create.
- `examples/rediscloud/cloud-account.yaml` registers a cloud account. It needs real cloud access keys in a Secret.
- `examples/rediscloud/subscription-peering.yaml` creates a VPC peering. It references a subscription named `example-subscription` and needs a real AWS account, VPC id, and CIDR.
- `examples/private/link.yaml` and `examples/active/private-link.yaml` create a private link. Each file only sets `subscriptionIdRef`. A pass needs the parent subscription and an AWS setup.
- `examples/transit/gateway-invitation-acceptor.yaml`, `examples/transit/gateway-route.yaml`, and the Active-Active copies under `examples/active/` accept an invitation or set CIDR routes. A pass needs the parent subscription and an invitation that already exists in AWS.
- `examples/providerconfig/providerconfig.yaml` is the credential object. It is applied with the other manifests. It is not a Redis Cloud resource.

A comment can name one of those paths. The workflow does not run the whole `examples/` tree by itself.

## CI/CD Workflows

### Main CI Pipeline (`.github/workflows/ci.yml`)

The main CI pipeline runs on every push to main/release branches and on pull requests. It includes:

- **detect-noop**: Skips CI for documentation-only changes
- **report-breaking-changes**: Detects breaking CRD OpenAPI schema changes
- **lint**: Runs golangci-lint with caching for faster runs
- **check-diff**: Verifies generated code is up-to-date
- **unit-tests**: Runs Go unit tests with coverage reporting
- **local-deploy**: Tests local deployment of the provider
- **check-examples**: Validates example manifests against CRDs
- **publish-artifacts**: Builds and publishes provider artifacts
- **ci-success**: Depends on detect-noop and the jobs above. `detect-noop` must be success. `failure` and `cancelled` fail this job. `skipped` on the other jobs is success, which is what a docs-only pull request produces. This is the status check to require on `main`. It does not depend on the nightly QA workflow.

### End-to-End Testing (`.github/workflows/uptest-trigger.yaml`)

Triggered via PR comments with `/test-examples="<path>"`:

- Permission-based access control (write/admin only)
- Dynamic example selection based on paths
- Creates GitHub status checks for test results
- Captures cluster dumps on failure for debugging
- Uses `uptest` framework for automated testing

Example usage:
```
/test-examples="examples/rediscloud"
```

### Stale Issue Management (`.github/workflows/stale.yml`)

Automatically manages stale issues and PRs:
- Marks issues/PRs as stale after 90 days of inactivity
- Closes them after 14 additional days without activity
- Users can use `/fresh` comment to remove stale label

### Terraform provider release tracking (`.github/workflows/terraform-release.yaml`)

Runs every day, and from Actions → Run workflow. It compares `TERRAFORM_PROVIDER_VERSION` with the latest release of `RedisLabs/terraform-provider-rediscloud`. The same version opens nothing. A newer stable release updates that pin, runs `make generate` in a job that cannot push, and a second job opens a pull request. The workflow does not merge that pull request, and it does not change the Crossplane, Go, or Upjet versions. If the branch was already pushed, the next run opens the pull request from that branch.

A manual run can set `dry_run` to print the decision without opening a pull request, or set `tag` to a fixture such as `v2.20.0`.

### Nightly QA (`.github/workflows/qa-live.yaml`)

Runs at 06:17 UTC, and from Actions → Run workflow. It does not run on pull requests. The job builds the provider on a kind cluster and runs `scripts/qa_live.py`, which creates and deletes one ACL rule, one Pro subscription, and one database on the QA account. The credential comes from the `UPTEST_CLOUD_CREDENTIALS` secret. The script requires secret keys `public_endpoint` and `private_endpoint`. `port` is not required. `attribute.password` may also be present because the sensitive-field mapping owns the password. This workflow is not a required status check.

## Testing

### Unit Tests

Unit tests live in `*_test.go` files. `make test` runs the packages under
`cmd`, `internal`, `apis`, and `config`. The subscription example test and
the connection-secret tests are in `config/`. The lookup tests are in
`internal/lookup/` and `internal/controller/lookup/`. `make test` also runs
`scripts/test_track_terraform_release.py`, `scripts/test_qa_live.py`,
`scripts/test_ci_success.py`, and `scripts/test_alignment_guards.py`.

`config/alignment_guard_test.go` fails if a generated controller returns to a
3 minute timeout, if Active-Active `clientTlsCertificatesSecretRef` is no
longer a secret-reference list, or if a database type grows
`subscriptionIdRef`. String children such as peering keep that reference.
Managed resources are stored and reconciled as `v1beta1`. `v1alpha1` stays served for those kinds. Lookups stay `v1alpha1`. Generation keeps the `zz_*` files for both served versions.

The `govulncheck` job runs govulncheck v1.9.0 with Go 1.26. It fails when called code is affected by a vulnerability. The report is uploaded as the `govulncheck` artifact.

Run unit tests:
```bash
make test
```

### Example Validation

The `scripts/check-examples.py` script validates all example manifests against their CRDs:

```bash
make check-examples
```

Or directly:
```bash
python3 scripts/check-examples.py package/crds examples
```

Requirements:
- Python 3.x
- PyYAML (`pip install pyyaml`)

### End-to-End Tests

`make e2e` creates the Redis Cloud resources listed in `UPTEST_EXAMPLE_LIST`.
The main CI workflow leaves that run to a `/test-examples` comment, which
needs the `UPTEST_CLOUD_CREDENTIALS` repository secret.

```bash
UPTEST_EXAMPLE_LIST="examples/rediscloud/acl-rule.yaml" make e2e
```

Required environment variables:
- `UPTEST_EXAMPLE_LIST`: Comma-separated list of example files
- `UPTEST_CLOUD_CREDENTIALS`: Provider credential JSON (`api_key`, `secret_key`, optional `url`)

A Pro subscription example needs a payment method id, and an Essentials
subscription example needs a plan id. Apply the lookup manifests under
`examples/lookup/` and copy `status.atProvider.id`. Account-specific ids stay
out of the repository.

## Code coverage

`unit-tests` uploads `_output/tests/linux_amd64/coverage.txt` to Codecov. The upload sets `fail_ci_if_error: false`. `codecov.yml` ignores generated `zz_` files, tests, examples, and `package/`. Codecov is not a required check on `main`.

## Example Manifests

Example manifests are located in `examples/` directory:

- `examples/rediscloud/`: Pro subscription, database, peering, ACL, and cloud account
- `examples/active/`: Active-Active private link and transit gateway examples
- `examples/private/`: Private link
- `examples/transit/`: Transit gateway examples
- `examples/lookup/`: Read-only payment method, Essentials plan, and region lookups
- `examples/providerconfig/`: Provider configuration

## Make Targets

Testing-related Make targets:

```bash
make test              # Run unit tests
make e2e               # Run end-to-end tests
make uptest            # Run uptest framework
make check-examples    # Validate example manifests
make lint              # Run linting
make check-diff        # Verify generated code
make crddiff           # Check for breaking CRD changes
```

## Contributing

When submitting PRs:

1. Ensure all tests pass: `make test`
2. Validate examples: `make check-examples`
3. Run linting: `make lint`
4. Verify generated code: `make check-diff`
5. When the change needs a real Redis Cloud resource, comment `/test-examples="<path>"` on the pull request

## Dependencies

- Go 1.26, matching `.github/workflows/ci.yml`
- Python 3.x (for check-examples.py)
- PyYAML (`pip install pyyaml`)
- Docker (for local deployment)
- Kind (for local Kubernetes cluster)