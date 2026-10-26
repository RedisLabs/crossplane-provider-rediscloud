# Testing the RedisCloud Crossplane Provider

This guide explains how to test the provider. Unit tests stay on this machine.
A local deploy starts a Kind cluster. End-to-end tests create real Redis Cloud
resources and run only when someone starts them.

## Prerequisites

- Docker or Podman
- Go 1.24, which is the version used in CI
- Make
- Redis Cloud API credentials for any test that calls the API

## Quick start

1. Copy the credential template and fill in your own keys. Keep the filled file
   local.

   ```bash
   cp .envrc.example .envrc.local
   ```

   `.envrc` loads `.envrc.local` when direnv is active. Real keys stay out of
   the repository.

2. Run the unit tests.

   ```bash
   make test
   ```

3. Deploy the provider into a local control plane when a change needs a cluster.

   ```bash
   make local-deploy
   ```

   That target installs Crossplane into the `upbound-system` namespace, builds
   the provider, and waits until the provider is healthy.

## Old harness

`scripts/quick-start.sh`, `scripts/setup-podman.sh`, and
`scripts/test-provider.sh` are the previous podman harness. `test-provider.sh`
creates and deletes a Kind cluster named `rediscloud-test`. Use `make test` and
`make local-deploy` for current work.

## Unit tests

`make test` runs the Go tests. Coverage for generated files is filtered in the
`cobertura` target. Focused packages:

```bash
go test ./config/ ./internal/lookup/ ./internal/controller/lookup/
```

## Local deploy

`make local-deploy` builds the provider and installs it into the local control
plane. Provider logs for that install are in `upbound-system`:

```bash
kubectl logs -n upbound-system -l pkg.crossplane.io/provider=provider-rediscloud
```

Create the credential secret and `ProviderConfig` before applying an example.
The secret shape is in the README. The `ProviderConfig` manifest is
`examples/providerconfig/providerconfig.yaml`.

## End-to-end tests

`make e2e` installs the local control plane and then runs uptest. It creates
the Redis Cloud resources named in `UPTEST_EXAMPLE_LIST`. Set that variable to
the example files for this run, and set `UPTEST_CLOUD_CREDENTIALS` to the
provider credential JSON (`api_key`, `secret_key`, and optional `url`).

The main CI workflow runs lint, unit tests, a local deploy, and example
checks. A reviewer with write access starts the cloud tests by commenting on
the pull request:

```text
/test-examples="examples/rediscloud/acl-rule.yaml"
```

That comment runs `.github/workflows/uptest-trigger.yaml`. The workflow reads
`UPTEST_CLOUD_CREDENTIALS` from the repository secret of the same name.

## Payment method, plan, and region

A Pro subscription needs a payment method id. An Essentials subscription needs
a plan id. Copy those values from the lookup resources after they become Ready:

- `examples/lookup/paymentmethod.yaml`
- `examples/lookup/essentialsplan.yaml`
- `examples/lookup/regions.yaml`

The id is `status.atProvider.id`. Account-specific ids stay out of the
repository. `examples/rediscloud/database.yaml` shows the same pattern for a
numeric subscription id.

## Example manifests

`make check-examples` validates the manifests under `examples/` against the
CRDs. The directories are `examples/rediscloud/`, `examples/active/`,
`examples/private/`, `examples/transit/`, `examples/lookup/`, and
`examples/providerconfig/`.

## Troubleshooting

Provider install:

```bash
kubectl get providers
kubectl describe provider provider-rediscloud
```

On a cluster created by `make local-deploy`, the provider namespace is
`upbound-system`. A cluster built by the old harness used `crossplane-system`.

Credential secret:

```bash
kubectl get providerconfig default -o yaml
```

Resource status:

```bash
kubectl describe subscription my-subscription
kubectl get events --sort-by=.metadata.creationTimestamp
```

Remove the local control plane with `make controlplane.down`.

## Environment variables

- `UPTEST_EXAMPLE_LIST` lists the example files for `make e2e`
- `UPTEST_CLOUD_CREDENTIALS` is the provider credential JSON
- `UPTEST_DATASOURCE_PATH` is an optional uptest datasource file
- `REDISCLOUD_API_KEY`, `REDISCLOUD_SECRET_KEY`, and `REDISCLOUD_URL` are the
  local credential variables in `.envrc.local`
- `CROSSPLANE_NAMESPACE` for `make local-deploy` and `make e2e` is
  `upbound-system`, set in the Makefile
