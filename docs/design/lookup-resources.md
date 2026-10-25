# Lookups for payment method, plan, and region

Date: 23 October 2026.
Status: Proposed. Implementation matches this design. Unit tests passed, and the three lookup objects became Ready on the test cluster on 23 October 2026. Status ids matched the Redis Cloud API for the same filters. Deleting the objects left the account unchanged.
Owner: Shay Berman.
Ticket: none.
Design: this file.
ADR: none. This implements the lookup decision already recorded in `docs/terraform-alignment.md` gap 8.
BDR: none.

## TLDR

Three read-only custom resources call the Redis Cloud API and write the looked-up id into status. They cover payment method, Essentials plan, and region. The other 26 Terraform data sources stay pasted ids. A database subscription id and an Essentials plan id stay numbers, so those values are still copied by hand.

## Problem

Terraform Redis Cloud v2.19.2 has data sources for a payment method, an Essentials plan, and regions. This provider has no object that performs those lookups, so a subscription author pastes the ids.

## Goals

- `PaymentMethod`, `EssentialsPlan`, and `Regions` become Ready when the Redis Cloud API returns a match.
- The match rules follow the Terraform data sources in v2.19.2.
- Deleting the object deletes nothing in Redis Cloud.
- Examples contain no account ids.

## Non-goals

- Lookups for the other 26 data sources.
- A Crossplane reference into a numeric field. Essentials `planId` and Essentials `paymentMethodId` stay hand copies.
- Regenerating the Upjet subscription types in this change.
- A Terraform CLI or Upjet upgrade.

## Behavior

All three are cluster-scoped, group `lookup.redis.io`, version `v1alpha1`. Each names a `ProviderConfig` and reads `api_key`, `secret_key`, and optional `url` from that config. The controller uses `github.com/RedisLabs/rediscloud-go-api` v0.52.1, the same client Terraform Redis Cloud v2.19.2 uses.

A successful reconcile sets Ready and Synced. Zero matches or more than one match on `PaymentMethod` and `EssentialsPlan` sets Ready false and Synced false. `Regions` returns the list and is Ready when that list is not empty. Nothing is created in Redis Cloud, and delete has no finalizer.

| Kind | Spec filters | Status |
| --- | --- | --- |
| `PaymentMethod` | `cardType`, `lastFourNumbers`, `excludeExpired` (default true) | `atProvider.id`, `cardType`, `lastFourNumbers` |
| `EssentialsPlan` | `id`, `name`, `size`, `sizeMeasurementUnit`, `cloudProvider`, `region`, `availability`, `supportDataPersistence`, `supportReplication`, `subscriptionId` | the matching plan, including `id` |
| `Regions` | `providerName` (`AWS` or `GCP`), optional `name` | `regions[]` with `regionId`, `name`, `providerName` |

`PaymentMethod` and `EssentialsPlan` require exactly one match, as the Terraform data sources do. `Regions` returns every region that passes the filter. `cloudProvider` accepts `AWS`, `GCP`, and `Azure` in any case. The API call uses `AWS`, `GCP`, or `Azure`, and the match ignores case.

Pro and Active-Active `paymentMethodId` are strings. This change does not add `paymentMethodIdRef`. The author copies `status.atProvider.id` onto the subscription. Essentials `planId` and Essentials `paymentMethodId` are numbers, so those stay hand copies too.

## Validation

Unit tests cover the filters: expired cards dropped, zero matches, multiple matches, one match, provider and name filters for regions, and one Essentials plan.

On the test cluster, against the non-production Redis Cloud account:

- A `PaymentMethod` becomes Ready and its id matches the payment method the API returns for the same filter.
- A `Regions` object for GCP includes the region used for Pro subscriptions.
- An `EssentialsPlan` filter that matches one plan becomes Ready and status.id is that plan id.
- Deleting the three objects leaves the account unchanged.

## Rollout

The new CRDs install with the provider package. The package manager grants the provider access to those CRDs. Existing managed resources are unchanged. Rollback is reverting the pull request. Already-applied lookup objects become unknown kinds after the CRDs are removed.
