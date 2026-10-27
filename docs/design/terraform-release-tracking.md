# Track Terraform provider releases

Date: 26 October 2026. Updated 27 October 2026 for the review fixes.
Status: Proposed. Implementation matches this design.
Owner: Shay Berman.
Ticket: none.
Design: this file.
ADR: none. This implements gap 9 in `docs/terraform-alignment.md`.
BDR: none.

## TLDR

A daily workflow compares `TERRAFORM_PROVIDER_VERSION` with the latest release of `RedisLabs/terraform-provider-rediscloud`. The same version opens nothing. A newer stable release updates that pin, runs `make generate`, and opens a pull request. The workflow does not merge, and it does not change the Crossplane, Go, or Upjet versions.

## Problem

The provider is pinned to one Terraform Redis Cloud release. Nothing notices when a later release is published, so the pin stays where a person last set it.

## Goals

- A published release newer than `TERRAFORM_PROVIDER_VERSION` opens one pull request.
- That pull request changes the pin and regenerates.
- Regeneration still rewrites the controller timeout to 30 minutes.
- The current release, v2.19.2, opens nothing.

## Non-goals

- Merging the generated pull request.
- Bumping Crossplane, Go, Upjet, or Terraform CLI.
- Tracking pre-release tags.
- Creating Redis Cloud resources from this workflow.

## Behavior

`.github/workflows/terraform-release.yaml` runs every day at 04:17 UTC and from Actions → Run workflow. `scripts/track_terraform_release.py` reads the pin from the Makefile and the latest stable release from GitHub. Drafts and pre-releases are not selected. `v` is ignored, so `v2.19.2` and `2.19.2` are the same pin.

When the candidate is not newer, the script prints `decision=none` and exits. A manual run with `dry_run` does the same comparison and does not edit the tree. A manual `tag` substitutes for the latest release, which is how a fixture such as `v2.20.0` is tested.

When the candidate is newer and this is not a dry run, the script continues only if HEAD is `main` and the worktree is clean. It refuses to continue when an open pull request already exists for `track/terraform-provider-<version>`. If that branch is already on the remote and its Makefile pin is the candidate, the next run opens the pull request from that branch and does not generate again. That is how a push that did not finish opening the pull request is retried. If the remote branch exists with a different pin, the replacement commit is pushed with `--force-with-lease`.

Otherwise it updates only `TERRAFORM_PROVIDER_VERSION`, runs `make generate`, and checks every `zz_controller.go` for `30 * time.Minute`. A controller that still has the 3 minute timeout stops the job before a pull request is opened. The same stop happens if generate changes `CROSSPLANE_VERSION`, `TERRAFORM_VERSION`, the Go or toolchain lines, the Upjet module, crossplane-runtime, or the Go version in CI.

The workflow is two jobs. `prepare` can read the repository and list pull requests. It cannot push. Before `make generate`, the script removes `GH_TOKEN`, `GITHUB_TOKEN`, and `GITHUB_ENTERPRISE_TOKEN` from the environment, and `make` starts with that environment. `publish` is the only job that can push and open a pull request, and it does not run `make generate`. The script then commits in `prepare`, and `publish` pushes `track/terraform-provider-<version>` and opens a pull request against `main`. It never calls `gh pr merge`.

`make generate` already runs `scripts/fix_generated_types.py`, which is what rewrites the 3 minute timeout. The check after generate is the guard that the rewrite happened.

GitHub does not start CI for a pull request that this workflow opens with `GITHUB_TOKEN`. The pull request body says to push a commit from a user account if checks do not appear.

## Validation

`make test` runs `scripts/test_track_terraform_release.py`.

- A dry run against the published v2.19.2 release prints `decision=none` and leaves the Makefile unchanged.
- A dry run with fixture tag `v2.20.0` prints `decision=would-open` and does not call `gh`.
- Fixture tag `v2.20.0` in a local repository opens a pull request whose commit changes `TERRAFORM_PROVIDER_VERSION` to `2.20.0` and does not change `CROSSPLANE_VERSION`.
- A generate step that leaves a 3 minute timeout does not open a pull request.
- A generate step that changes `CROSSPLANE_VERSION` does not open a pull request.
- A remote branch that already has the candidate pin opens a pull request without another commit.
- A remote branch with a different commit is updated with `--force-with-lease`.
- `make generate` does not see `GH_TOKEN` or `GITHUB_TOKEN`.
- The live dry run follows the current pin and the published release. A newer published release prints `decision=would-open`. The same version prints `decision=none`.

## Rollout

Merging this pull request turns the daily job on. Rollback is reverting that merge. A pull request the job already opened stays open until someone closes or merges it. This job does not install the provider anywhere.
