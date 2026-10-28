# CI and Testing Infrastructure

This document describes the CI/CD pipeline and testing infrastructure for the Crossplane Provider RedisCloud.

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
`scripts/test_track_terraform_release.py` and `scripts/test_qa_live.py`.

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

## Code Coverage

Code coverage is automatically reported to Codecov on every CI run. Configuration is in `codecov.yml`.

Coverage requirements:
- Project: Auto-target with 2% threshold
- Patch: 80% target with 5% threshold

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

- Go 1.24, matching `.github/workflows/ci.yml`
- Python 3.x (for check-examples.py)
- PyYAML (`pip install pyyaml`)
- Docker (for local deployment)
- Kind (for local Kubernetes cluster)