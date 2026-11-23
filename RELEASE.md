# Release

No provider package is published today.

## Tag

`workflow_dispatch` of [`.github/workflows/tag.yaml`](.github/workflows/tag.yaml) creates the tag. The workflow inputs are the version and the tag message. Creating the tag does not publish a package.

## Package

`workflow_dispatch` of [`.github/workflows/publish-provider-package.yml`](.github/workflows/publish-provider-package.yml) publishes the package. The workflow inputs are an optional version string and the Go version used when a build is required.

That workflow reads these repository secrets:

- `XPKG_MIRROR_TOKEN`
- `XPKG_MIRROR_ACCESS_ID`

Do not copy the secret values into a workflow file, a pull request, or this document. They stay in the repository secret store.

## Terraform pin

[`.github/workflows/terraform-release.yaml`](.github/workflows/terraform-release.yaml) only opens a pull request that changes the Terraform provider pin and regenerates from that pin. It runs every day and from `workflow_dispatch`. A manual run can compare a fixture tag or print the decision without opening a pull request.

That workflow does not create a tag and does not publish a package.
