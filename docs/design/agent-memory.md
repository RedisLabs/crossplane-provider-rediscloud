# Agent Memory resources

Date: 6 November 2026.
Status: Proposed. Generated from Terraform provider v2.20.0. Unit tests cover the resource list. No live Redis Cloud create has been run.
Owner: Shay Berman.
Ticket: none.
Design: this file.
ADR: none. The Terraform CLI stays 1.5.7, the choice already recorded in `docs/design/provider-upgrade-plan.md`.
BDR: none.

## TLDR

`Memory` and `MemoryAPIKey` are generated managed resources for Terraform `rediscloud_agent_memory` and `rediscloud_agent_memory_api_key`. The provider pin is v2.20.0. Terraform CLI stays 1.5.7.

## Problem

Terraform provider v2.20.0 added Agent Memory. The release-tracking pull request bumped the pin and regenerated the existing include list, so the new resources were in `config/schema.json` and absent from the CRDs.

## Goals

- Both Agent Memory resources have CRDs, controllers, and `IdentifierFromProvider`.
- The controller timeout stays 30 minutes.
- The schema resource set and `ExternalNameConfigs` stay the same set.

## Non-goals

- A nightly create of Agent Memory.
- Raising Terraform CLI to 1.11. The Makefile rejects 1.6 and newer because of the BSL license.
- A write-only credential field. On Terraform 1.5.7 the model API key is a sensitive string, generated as `apiKeySecretRef`.
- Serving `v1alpha1` for these new kinds. They have no previously stored version.

## Behavior

`agent.redis.io/v1beta1` stores `Memory` and `MemoryAPIKey`. Update is the Upjet apply loop. Import uses `crossplane.io/external-name` when the management policy is Observe. Active-Active database regions also gain `localReadOperationsPerSecond` and `localWriteOperationsPerSecond`.

## Validation

`go test ./config/` checks the 29 names, the 30 minute timeout, and that every schema resource is configured. `python3 -m unittest discover -s scripts -p 'test_*.py'` checks the release tracker and the nightly script. No Redis Cloud call is part of this change.

## Rollout

The CRDs install with the provider package. Rollback is reverting the merge. Nothing in Redis Cloud is created by the merge.
