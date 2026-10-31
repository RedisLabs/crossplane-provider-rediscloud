# Glossary

Repository vocabulary for `crossplane-provider-rediscloud`. Meanings only.
Scope for Terraform provider alignment is in `docs/terraform-alignment.md`.

## Active-Active

Redis Cloud multi-region subscriptions. Terraform resource names start with
`rediscloud_active_active_`.

Not to be confused with Ultra.

Source: confirmed by Shay, 2026-10-21. See `docs/terraform-alignment.md`.

## bring-your-own-cloud

Redis Cloud running in the customer's cloud account. Covered here only where
`terraform-provider-rediscloud` already covers it.

Alias: BYOC.

Source: confirmed by Shay, 2026-10-21. See `docs/terraform-alignment.md`.

## Essentials

Redis Cloud fixed tier on shared clusters. Terraform resources:
`rediscloud_essentials_subscription`, `rediscloud_essentials_database`.

Source: confirmed by Shay, 2026-10-21. See `docs/terraform-alignment.md`.

## GA

For this provider, GA means a Redis-published package whose CRDs match a
named Terraform provider version, Redis owns the repo, and one real
subscription plus database has been applied on a non-production Redis Cloud
account. Resource coverage alone is not GA.

Not to be confused with a preview tag, or with Ultra.

Source: `docs/terraform-alignment.md`. Confirmed by Shay, 2026-10-21.

## native provider

A Crossplane provider written by hand against the Redis Cloud API. Out of
scope. This repository is the Upjet provider.

Not to be confused with this Upjet provider.

Source: confirmed by Shay, 2026-10-21. See `docs/terraform-alignment.md`.

## passwordless

On the Pro database, `spec.forProvider.enablePasswordless` is the generated
field for the Terraform attribute `enable_passwordless` on
`rediscloud_subscription_database`. True asks that provider to write an empty
password. Omitting the field leaves the password key out of the Terraform
parameters.

Not to be confused with a Cloud API boolean, or with Active-Active
`globalEnablePasswordless`.

Source: pinned Terraform schema. Confirmed by Shay, 2026-10-30. See
`docs/design/RED-223310.md`.

## Pro

Redis Cloud dedicated tier. Public Terraform names remain
`rediscloud_subscription` and `rediscloud_subscription_database`.

Source: confirmed by Shay, 2026-10-21. See `docs/terraform-alignment.md`.

## provider-rediscloud

This Upjet-generated Crossplane provider for current Redis Cloud. Repository
`crossplane-provider-rediscloud`. Go module
`github.com/RedisLabs/provider-rediscloud`. It tracks
`terraform-provider-rediscloud` and calls the Redis Cloud API through that
provider.

Not to be confused with Redis-internal Crossplane usage, or with Ultra.

Aliases: crossplane-provider-rediscloud, the Crossplane provider.

Source: confirmed by Shay, 2026-10-21. See `docs/terraform-alignment.md`.

## Ultra

A different control plane from current Redis Cloud. Out of scope. This
provider cannot target it.

Source: confirmed by Shay, 2026-10-21. See `docs/terraform-alignment.md`.

## Upjet

Crossplane generator that turns a Terraform provider schema into managed
resources and controllers. This repository is an Upjet provider.

Source: repository README. Confirmed by Shay, 2026-10-21.
