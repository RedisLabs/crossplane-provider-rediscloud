# Contributing to provider-rediscloud

We welcome contributions to the RedisCloud Crossplane Provider! This document provides guidelines for contributing to the project.

## Getting Started

1. Fork the repository on GitHub
2. Clone your fork locally
3. Create a new branch for your feature or fix
4. Make your changes
5. Test your changes
6. Submit a pull request

## Development Setup

See the [README.md](README.md#developing) for development setup instructions.

## Code Standards

- Follow Go best practices and idioms
- Run `go fmt` and `go vet` before committing
- Add tests for new functionality
- Update documentation as needed

## Testing

Before submitting a pull request:

1. Run the unit tests: `make test`
2. Deploy locally when the change needs a cluster: `make local-deploy`
3. Regenerate when CRDs or controllers change: `make generate`
4. Validate examples when manifests change: `make check-examples`

`scripts/test-provider.sh` is the old podman harness. It creates and deletes a
Kind cluster named `rediscloud-test`.

End-to-end tests create real Redis Cloud resources. A reviewer starts them on
a pull request with `/test-examples="<path>"`. See [TESTING.md](TESTING.md).

## Submitting Changes

1. Push your changes to your fork
2. Submit a pull request to the main repository
3. Describe your changes in detail
4. Link any related issues

## Code of Conduct

Please be respectful and professional in all interactions. We strive to maintain a welcoming and inclusive community.

## Questions?

If you have questions, please open an issue or reach out on the Crossplane Slack channel.

Thank you for contributing!