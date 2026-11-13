# Provider RedisCloud

`provider-rediscloud` is a [Crossplane](https://crossplane.io/) provider that
is built using [Upjet](https://github.com/crossplane/upjet) code
generation tools and exposes XRM-conformant managed resources for the
RedisCloud API.

Repository vocabulary is in [GLOSSARY.md](GLOSSARY.md).

## Overview

This provider enables management of RedisCloud resources through Kubernetes Custom Resources. It includes support for:

- **ACL Management** - Roles, Rules, and Users
- **Active-Active Databases** - Multi-region deployments
- **Cloud Accounts** - AWS and GCP integrations
- **Essentials Tier** - Free and fixed plans
- **Pro Subscriptions** - Full-featured Redis deployments
- **Private Connectivity** - VPC peering and private endpoints
- **Transit Gateways** - AWS Transit Gateway attachments

## Getting Started

Follow [USAGE.md](USAGE.md) for a complete walkthrough: install the locally built
provider, configure credentials, create AWS or GCP Pro resources, check status,
and verify cleanup.

### Prerequisites

1. [Kubernetes](https://kubernetes.io/) cluster compatible with the Crossplane version you install
2. [Crossplane](https://crossplane.io/) installed
3. RedisCloud API credentials

### Installation

This provider has no published package. Do not install
`xpkg.crossplane.io/RedisLabs/provider-rediscloud:v0.1.0`. That image does not
exist. Build from this repository with the Developing steps below, then load
the image into the cluster you use for testing.

Managed resources are stored and reconciled as `v1beta1`. `v1alpha1` stays served for those kinds. `ProviderConfig` is `v1beta1`. Lookups stay `v1alpha1`.

### Configuration

1. Store credential JSON in a private file outside the repository, using
   `api_key`, `secret_key`, and an explicit `url` for QA. See
   [the credential setup](USAGE.md#2-configure-credentials). Load it into a Secret:

```bash
kubectl create secret generic rediscloud-creds \
  --namespace upbound-system \
  --from-file=credentials="$HOME/.config/rediscloud/example-credentials.json"
```

2. Create a ProviderConfig:

```yaml
apiVersion: rediscloud.redis.io/v1beta1
kind: ProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      name: rediscloud-creds
      namespace: upbound-system
      key: credentials
```

The namespace above matches `make local-deploy`. Use the namespace from your own
installation if it differs.

### Usage Example

Create an Essentials subscription:

```yaml
apiVersion: essentials.redis.io/v1beta1
kind: Subscription
metadata:
  name: my-redis-subscription
spec:
  forProvider:
    name: "My Redis Subscription"
    # Copy status.atProvider.id from an EssentialsPlan lookup.
    # The lookup manifest is examples/lookup/essentialsplan.yaml.
    planId: 123456
  providerConfigRef:
    name: default
```

## Developing

### Prerequisites

- Go 1.26 (the CI line)
- Docker or Podman
- Make

### Development Environment

This project uses [devenv](https://devenv.sh/) for development environment management. To enter the development shell:

```bash
devenv shell
```

### Building

Generate CRDs and controllers:

```bash
make generate
```

Build the provider:

```bash
make build
```

### Testing

Run the unit tests:

```bash
make test
```

`make local-deploy` installs the provider into a local control plane in the
`upbound-system` namespace. End-to-end tests create Redis Cloud resources. A
reviewer starts them with a `/test-examples="<path>"` comment on the pull
request. The steps are in [TESTING.md](TESTING.md).

### Local Development

Run against a Kubernetes cluster:

```bash
make run
```

Deploy locally for testing:

```bash
make local-deploy
```

## Documentation

- [Usage guide](USAGE.md) - Complete AWS and GCP Pro examples, credentials, status, and cleanup
- [Terraform Alignment](docs/terraform-alignment.md) - Alignment history and remaining gaps
- [Architecture decisions](docs/adr/ADR-index.md) - Why the provider follows Upjet v2
- [Testing Guide](TESTING.md) - Instructions for testing the provider
- [API Reference](https://doc.crds.dev/github.com/RedisLabs/provider-rediscloud) - Generated CRD documentation

## Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

## License

This project is licensed under the Apache 2.0 License - see the [LICENSE](LICENSE) file for details.

## Report a Bug

For filing bugs, suggesting improvements, or requesting new features, please
open an [issue](https://github.com/RedisLabs/provider-rediscloud/issues).
