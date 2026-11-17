# Lookups for Terraform data sources

Date: 23 October 2026. Updated 17 November 2026.
Status: Proposed. The payment method, Essentials plan, and region lookups match this design. Those three became Ready on the test cluster on 23 October 2026, and deleting them left the account unchanged. The 17 November 2026 lookups follow the same controller pattern. Their filters are covered by credential-free tests against a local HTTP server. They have not been run against a live Redis Cloud account.
Owner: Shay Berman.
Ticket: none.
Design: this file.
ADR: none. This implements the lookup decision already recorded in `docs/terraform-alignment.md` gap 8.
BDR: none.

## TLDR

Read-only custom resources in `lookup.redis.io/v1alpha1` call the Redis Cloud API and write the looked-up value into status. Twenty-four of the twenty-nine Terraform v2.20.0 data sources have a lookup. Five stay without one because a single list-and-filter read would not represent them. A database subscription id and an Essentials plan id stay numbers, so those values are still copied by hand.

## Problem

Terraform Redis Cloud v2.19.2 has data sources for a payment method, an Essentials plan, and regions. This provider has no object that performs those lookups, so a subscription author pastes the ids.

## Goals

- `PaymentMethod`, `EssentialsPlan`, and `Regions` become Ready when the Redis Cloud API returns a match.
- The match rules follow the Terraform data sources. The original three follow v2.19.2. The lookups added on 17 November 2026 follow v2.20.0.
- Deleting the object deletes nothing in Redis Cloud.
- Examples contain no account ids.

## Non-goals

- Lookups for the five data sources that fan out across several APIs or return a secret. Those are listed below.
- A request field the Terraform data source does not send. `wait_for_tgw_timeout` and `wait_for_invitations_timeout` default to one read, so they are not spec fields. The controller requeues instead of blocking. `includeTerraformGcpScript` is hardcoded true by Terraform, so the controller passes true and does not accept it as a field.
- A Crossplane reference into a numeric field. Essentials `planId` and Essentials `paymentMethodId` stay hand copies.
- Regenerating the Upjet subscription types in this change.
- A Terraform CLI, Upjet, or `rediscloud-go-api` upgrade. The client stays v0.52.1.
- A live Cloud run for the lookups added on 17 November 2026.

## Behavior

All three are cluster-scoped, group `lookup.redis.io`, version `v1alpha1`. Each names a `ProviderConfig` and reads `api_key`, `secret_key`, and optional `url` from that config. The controller uses `github.com/RedisLabs/rediscloud-go-api` v0.52.1, the same client Terraform Redis Cloud v2.19.2 uses.

A successful reconcile sets Ready and Synced. Zero matches or more than one match on `PaymentMethod` and `EssentialsPlan` sets Ready false and Synced false. `Regions` returns the list and is Ready when that list is not empty. Nothing is created in Redis Cloud, and delete has no finalizer.

| Kind | Spec filters | Status |
| --- | --- | --- |
| `PaymentMethod` | `cardType`, `lastFourNumbers`, `excludeExpired` (default true) | `atProvider.id`, `cardType`, `lastFourNumbers` |
| `EssentialsPlan` | `id`, `name`, `size`, `sizeMeasurementUnit`, `cloudProvider`, `region`, `availability`, `supportDataPersistence`, `supportReplication`, `subscriptionId` | the matching plan, including `id`, price, limits, connections, capability flags, supported alerts, and customer support |
| `Regions` | `providerName` (`AWS` or `GCP`), optional `name` | `regions[]` with `regionId`, `name`, `providerName` |

`PaymentMethod` and `EssentialsPlan` require exactly one match, as the Terraform data sources do. `Regions` returns every region that passes the filter. `cloudProvider` accepts `AWS`, `GCP`, and `Azure` in any case. The plan list query is the trimmed value uppercased, matching the Terraform data source, so Azure is sent as `AZURE`. Status `cloudProvider` stays the value the API returns, which is `Azure`. Matching ignores case.

Pro and Active-Active `paymentMethodId` are strings. This change does not add `paymentMethodIdRef`. The author copies `status.atProvider.id` onto the subscription. Essentials `planId` and Essentials `paymentMethodId` are numbers, so those stay hand copies too.

## Validation

Unit tests cover the filters: expired cards dropped, zero matches, multiple matches, one match, provider and name filters for regions, and one Essentials plan. The Essentials plan list query uppercases Azure to `AZURE`, and status copies the capability fields the API client already returns, including a false flag.

On the test cluster, against the non-production Redis Cloud account:

- A `PaymentMethod` becomes Ready and its id matches the payment method the API returns for the same filter.
- A `Regions` object for GCP includes the region used for Pro subscriptions.
- An `EssentialsPlan` filter that matches one plan becomes Ready and status.id is that plan id.
- Deleting the three objects leaves the account unchanged.

## Additional lookups

The same rules apply to the lookups added on 17 November 2026. Each is cluster-scoped, names a ProviderConfig, and deletes nothing. A successful reconcile sets Ready and Synced and requeues after 10 minutes. A failed reconcile clears `atProvider`, sets Ready false and Synced false, and requeues after 1 minute. Numeric filters are strings on the CRD and are parsed before the API call. An empty catalog, an empty peering list, and an empty invitation list are Ready. A lookup that must return one object fails on zero matches and on more than one match. Active-Active regions fail when the region list is empty.

| Kind | Terraform data source | Spec | Ready when |
| --- | --- | --- | --- |
| `ACLRole`, `ACLRule`, `ACLUser` | `rediscloud_acl_role`, `rediscloud_acl_rule`, `rediscloud_acl_user` | required `name` | exactly one exact name |
| `CloudAccount` | `rediscloud_cloud_account` | optional `excludeInternalAccount`, `providerType`, `name` | exactly one. Internal account id 1 is dropped only when `excludeInternalAccount` is true |
| `DataPersistence` | `rediscloud_data_persistence` | none | the catalog is returned, including an empty catalog. Status id is `ALL` |
| `DatabaseModules` | `rediscloud_database_modules` | none | the catalog is returned, including an empty catalog. Status id is `ALL` |
| `EssentialsSubscription` | `rediscloud_essentials_subscription` | optional `id` and `name`. `id` `0` is unset | exactly one. A missing payment method id is `0` |
| `PrivateLink`, `ActiveActivePrivateLink` | `rediscloud_private_link`, `rediscloud_active_active_private_link` | `subscriptionId`, plus `regionId` for Active-Active | the API returns the link |
| `PrivateLinkEndpointScript` | `rediscloud_private_link_endpoint_script` | `subscriptionId` | the subscription, the private link, and the script read succeed. An empty script is Ready |
| `ActiveActivePrivateLinkEndpointScript` | `rediscloud_active_active_private_link_endpoint_script` | `subscriptionId`, `regionId` | the script read succeeds. An empty script is Ready |
| `PrivateServiceConnect`, `ActiveActivePrivateServiceConnect` | `rediscloud_private_service_connect`, `rediscloud_active_active_private_service_connect` | `subscriptionId`, plus `regionId` for Active-Active | the service id is present. Status id is `subscription/service` |
| `PrivateServiceConnectEndpoints`, `ActiveActivePrivateServiceConnectEndpoints` | `rediscloud_private_service_connect_endpoints`, `rediscloud_active_active_private_service_connect_endpoints` | `subscriptionId`, `privateServiceConnectServiceId`, plus `regionId` for Active-Active | the endpoint list is returned. Rejected and deleted endpoints do not request a creation script. An empty list is Ready |
| `SubscriptionPeerings` | `rediscloud_subscription_peerings` | `subscriptionId`, optional `status` | the peering list is returned. Status id is `ALL` or the status filter |
| `TransitGateway`, `ActiveActiveTransitGateway` | `rediscloud_transit_gateway`, `rediscloud_active_active_transit_gateway` | `subscriptionId`, optional `tgwId` and `awsTgwUid`, plus `regionId` for Active-Active | exactly one attachment. Status id is `subscription/tgw` |
| `TransitGatewayInvitations`, `ActiveActiveTransitGatewayInvitations` | `rediscloud_transit_gateway_invitations`, `rediscloud_active_active_transit_gateway_invitations` | `subscriptionId`, plus `regionId` for Active-Active | the invitation list is returned, including an empty list |
| `ActiveActiveSubscriptionRegions` | `rediscloud_active_active_subscription_regions` | required `subscriptionName` | one Active-Active subscription has that name and its region list is not empty |

## Skipped data sources

These five are not one list or one get plus a client-side filter. A lookup of that shape would invent calls or put a secret in status.

| Data source | Why it is skipped |
| --- | --- |
| `rediscloud_database` | The read lists and gets the database, then reads the latest backup and the latest import. The password is sensitive. |
| `rediscloud_essentials_database` | The read lists and gets the fixed database, then reads the fixed latest backup and the fixed latest import. The password is sensitive. |
| `rediscloud_active_active_subscription_database` | The read lists and gets the Active-Active database, then reads the latest backup per region, the latest import, and the certificate. The password and TLS certificate are sensitive. |
| `rediscloud_subscription` | After the subscription list, the read calls maintenance and pricing. Those are separate APIs, and the status would be the whole subscription document. |
| `rediscloud_active_active_subscription` | Same maintenance and pricing fan-out as the Pro subscription data source. |

## Rollout

The CRDs install with the provider package. The package manager grants the provider access to those CRDs. Existing managed resources are unchanged. Rollback is reverting the pull request. Already-applied lookup objects become unknown kinds after the CRDs are removed.
