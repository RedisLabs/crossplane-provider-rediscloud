# Redis Cloud Crossplane provider, unreleased

Draft notes for the first package. No official package has been published, and this note is the candidate for that publication.
Compared with the provider already in the repo: main `6128cb7`, 9 September 2025.
Current main is `a2b716c`, 31 October 2026.

## Publication

There is no official publication yet.

- The repository has no git tag and no GitHub release.
- The publish workflow exists and has never been run.
- CI builds the package and uploads it as an Actions artifact. It does not push an image to a registry.
- There is no Upbound Marketplace listing, and [doc.crds.dev](https://doc.crds.dev/github.com/RedisLabs/provider-rediscloud) has not indexed this repository.
- `xpkg.crossplane.io/RedisLabs/provider-rediscloud:v0.1.0` does not exist. Do not install it.

This unreleased note is the set of changes that release would publish.

## Motivation

The provider that was already in the repo had one commit, on 9 September 2025, and no further changes until 22 October 2026, about 13 months. The versions it was built on had left support. This update moves each of them onto a current release and closes the feature gap with the current Terraform provider.

- Terraform Redis Cloud provider from v2.1.5 to v2.19.3.
- Upjet from v1.4.1 to v2.5.1.
- Crossplane from 1.16.0 (end of life) to 2.4.3.
- Go from 1.24 (end of life) to 1.26.8.
- Kubernetes clients from 1.29 (end of life) to 0.35.3.
- Managed API storage from `v1alpha1` to `v1beta1`.

In short, the new work adds:

- Six resources that were missing, and the fields added on the original 21.
- Private link and Transit Gateway invitations and CIDR routes, for Pro and for Active-Active.
- Lookups for payment method, Essentials plan, and region.
- A 30 minute controller timeout, secret references, connection endpoints, and Pro database passwordless.
- A workflow that opens a pull request when a newer Terraform release exists.
- A required CI check, a vulnerability scan, and a nightly create-and-delete on the QA Redis Cloud account.

## Custom resources

Compared with the provider at `6128cb7` (9 September 2025). A resource that already existed is stored as `v1beta1`, and `v1alpha1` is still accepted. The controller timeout on each managed resource is 30 minutes. The column names the fields and behavior that are new for that resource. The three lookups are new and stay `v1alpha1`. `ProviderConfig` was already `v1beta1`. `StoreConfig` is not in this release.

| Custom resource | What's new in this release |
| --- | --- |
| [Role](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/acl.redis.io_roles.yaml) (`acl.redis.io`) | No new fields. |
| [Rule](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/acl.redis.io_rules.yaml) (`acl.redis.io`) | No new fields. |
| [User](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/acl.redis.io_users.yaml) (`acl.redis.io`) | No new fields. The password was already a secret reference. |
| [Subscription](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/rediscloud.redis.io_subscriptions.yaml) (`rediscloud.redis.io`) | Pro subscription. `customerManagedKey`, `customerManagedKeyEnabled`, `customerManagedKeyDeletionGracePeriod`, and `publicEndpointAccess`. `cloudProvider.resourceTags` sets cloud tags. Status includes `prometheusEndpoint`. |
| [Database](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/subscription.redis.io_databases.yaml) (`subscription.redis.io`) | Pro database. `enablePasswordless`, `redisVersion`, `autoMinorVersionUpgrade`, and `ramPercentage` (RAM share on a ram-and-flash subscription). The connection secret publishes `public_endpoint` and `private_endpoint`. `subscriptionId` stays a number. |
| [Peering](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/subscription.redis.io_peerings.yaml) (`subscription.redis.io`) | `subscriptionIdRef` and `subscriptionIdSelector` select the Pro subscription. |
| [Link](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/private.redis.io_links.yaml) (`private.redis.io`) | Introduced. Private link for a Pro subscription. `subscriptionIdRef` selects that subscription. |
| [ServiceConnect](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/private.redis.io_serviceconnects.yaml) (`private.redis.io`) | `subscriptionIdRef` and `subscriptionIdSelector` select the Pro subscription. |
| [ServiceConnectEndpoint](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/private.redis.io_serviceconnectendpoints.yaml) (`private.redis.io`) | `subscriptionIdRef` and `subscriptionIdSelector` select the Pro subscription. |
| [ServiceConnectEndpointAccepter](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/private.redis.io_serviceconnectendpointaccepters.yaml) (`private.redis.io`) | `subscriptionIdRef` and `subscriptionIdSelector` select the Pro subscription. |
| [GatewayAttachment](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/transit.redis.io_gatewayattachments.yaml) (`transit.redis.io`) | `subscriptionIdRef` and `subscriptionIdSelector` select the Pro subscription. |
| [GatewayInvitationAcceptor](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/transit.redis.io_gatewayinvitationacceptors.yaml) (`transit.redis.io`) | Introduced. Accepts or rejects a Transit Gateway invitation for a Pro subscription. |
| [GatewayRoute](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/transit.redis.io_gatewayroutes.yaml) (`transit.redis.io`) | Introduced. Sets the Transit Gateway CIDR routes for a Pro subscription. |
| [Subscription](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/essentials.redis.io_subscriptions.yaml) (`essentials.redis.io`) | No new fields. |
| [Database](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/essentials.redis.io_databases.yaml) (`essentials.redis.io`) | `redisVersion`. The connection secret publishes `public_endpoint` and `private_endpoint`. |
| [Account](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/cloud.redis.io_accounts.yaml) (`cloud.redis.io`) | No new fields. The access key and console password were already secret references. |
| [ActiveSubscription](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/active.redis.io_activesubscriptions.yaml) (`active.redis.io`) | `customerManagedKey`, `customerManagedKeyEnabled`, `customerManagedKeyDeletionGracePeriod`, `publicEndpointAccess`, and `resourceTags`. Status includes `prometheusEndpoint`. |
| [ActiveSubscriptionDatabase](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/active.redis.io_activesubscriptiondatabases.yaml) (`active.redis.io`) | `redisVersion`, `autoMinorVersionUpgrade`, `globalEnableDefaultUser`, and `globalEnablePasswordless`. Status includes `redisVersionActual`. Client certificates are `clientSslCertificateSecretRef` and `clientTlsCertificatesSecretRef`. The connection secret publishes `public_endpoint.<region>` and `private_endpoint.<region>`. |
| [ActiveSubscriptionPeering](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/active.redis.io_activesubscriptionpeerings.yaml) (`active.redis.io`) | `subscriptionIdRef` and `subscriptionIdSelector` select the Active-Active subscription. |
| [ActiveSubscriptionRegions](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/active.redis.io_activesubscriptionregions.yaml) (`active.redis.io`) | `subscriptionIdRef` and `subscriptionIdSelector` select the Active-Active subscription. |
| [ActivePrivateLink](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/active.redis.io_activeprivatelinks.yaml) (`active.redis.io`) | Introduced. Private link for one region of an Active-Active subscription. |
| [ActivePrivateServiceConnect](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/active.redis.io_activeprivateserviceconnects.yaml) (`active.redis.io`) | `subscriptionIdRef` and `subscriptionIdSelector` select the Active-Active subscription. |
| [ActivePrivateServiceConnectEndpoint](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/active.redis.io_activeprivateserviceconnectendpoints.yaml) (`active.redis.io`) | `subscriptionIdRef` and `subscriptionIdSelector` select the Active-Active subscription. |
| [ActivePrivateServiceConnectEndpointAccepter](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/active.redis.io_activeprivateserviceconnectendpointaccepters.yaml) (`active.redis.io`) | `subscriptionIdRef` and `subscriptionIdSelector` select the Active-Active subscription. |
| [ActiveTransitGatewayAttachment](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/active.redis.io_activetransitgatewayattachments.yaml) (`active.redis.io`) | `subscriptionIdRef` and `subscriptionIdSelector` select the Active-Active subscription. |
| [ActiveTransitGatewayInvitationAcceptor](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/active.redis.io_activetransitgatewayinvitationacceptors.yaml) (`active.redis.io`) | Introduced. Accepts or rejects a Transit Gateway invitation for one Active-Active region. |
| [ActiveTransitGatewayRoute](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/active.redis.io_activetransitgatewayroutes.yaml) (`active.redis.io`) | Introduced. Sets the Transit Gateway CIDR routes for one Active-Active region. |
| [PaymentMethod](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/lookup.redis.io_paymentmethods.yaml) (`lookup.redis.io`) | Introduced. Reads a payment method id. Stored as `v1alpha1`. |
| [EssentialsPlan](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/lookup.redis.io_essentialsplans.yaml) (`lookup.redis.io`) | Introduced. Reads an Essentials plan id. Stored as `v1alpha1`. |
| [Regions](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/lookup.redis.io_regions.yaml) (`lookup.redis.io`) | Introduced. Reads a region id. Stored as `v1alpha1`. |
| [ProviderConfig](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/package/crds/rediscloud.redis.io_providerconfigs.yaml) (`rediscloud.redis.io`) | No field change. It was already `v1beta1`. |

## What changed

1. This Crossplane provider uses the [Terraform Redis Cloud provider](https://github.com/RedisLabs/terraform-provider-rediscloud), aligned from v2.1.5 to v2.19.3
   - It calls Redis Cloud through that Terraform provider. It was aligned to [v2.1.5](https://github.com/RedisLabs/terraform-provider-rediscloud/releases/tag/v2.1.5), and it is now aligned to [v2.19.3](https://github.com/RedisLabs/terraform-provider-rediscloud/releases/tag/v2.19.3).
   - v2.19.3 is the newest Terraform Redis Cloud release, published 28 October 2026.
   - Managed resources went from 21 to 27. Each one is in [Custom resources](#custom-resources).
   - The original 21 were regenerated, so fields added after v2.1.5 are on those CRDs.
   - Pro subscription gained resource tags, a Prometheus endpoint, and customer-managed key fields.
   - Active-Active database gained Redis version, the actual version, auto minor upgrade, and global passwordless.
   - Pro database gained `enablePasswordless`.
   - v2.19.3 itself added no managed resource. It changed how two subscription data sources are read.

2. Six resources that were missing, and the feature each one enables
   - Private link (`Link`). A [private link](https://redis.io/docs/latest/operate/rc/security/aws-privatelink/) to a Pro subscription, including which principals and databases can use it.
   - Active-Active private link (`ActivePrivateLink`). The same [private link](https://redis.io/docs/latest/operate/rc/security/aws-privatelink/) for one region of an Active-Active subscription.
   - Transit gateway invitation acceptor (`GatewayInvitationAcceptor`). [Accepts or rejects](https://redis.io/docs/latest/operate/rc/security/aws-transit-gateway/#accept-resource-share) an AWS Transit Gateway resource-share invitation for a Pro subscription. The invitation is created in AWS.
   - Active-Active transit gateway invitation acceptor (`ActiveTransitGatewayInvitationAcceptor`). The same [accept-or-reject](https://redis.io/docs/latest/operate/rc/security/aws-transit-gateway/#accept-resource-share) step for one region of an Active-Active subscription.
   - Transit gateway route (`GatewayRoute`). Sets the [Transit Gateway CIDR routes](https://redis.io/docs/latest/operate/rc/security/aws-transit-gateway/#add-consumer-cidrs) for a Pro subscription.
   - Active-Active transit gateway route (`ActiveTransitGatewayRoute`). Sets those [CIDR routes](https://redis.io/docs/latest/operate/rc/security/aws-transit-gateway/#add-consumer-cidrs) for one region of an Active-Active subscription.

3. Data sources from no lookups to three lookup objects
   - `PaymentMethod`, `EssentialsPlan`, and `Regions` can be read from the cluster.
   - The other 26 Terraform data sources stay pasted IDs.

4. API version from `v1alpha1` storage to `v1beta1` storage
   - `v1alpha1` stays served, so a cluster that already stored that version can still accept the package.

5. Runtime from [Upjet v1.4.1](https://github.com/crossplane/upjet/releases/tag/v1.4.1) and [Crossplane 1.16.0 (end of life)](https://github.com/crossplane/crossplane/releases/tag/v1.16.0) to [Upjet v2.5.1](https://github.com/crossplane/upjet/releases/tag/v2.5.1) and [Crossplane 2.4.3](https://github.com/crossplane/crossplane/releases/tag/v2.4.3)
   - Go in the module went from [1.24 (end of life)](https://go.dev/doc/devel/release#go1.24.0) to [1.26.8](https://go.dev/doc/devel/release#go1.26.8).
   - [Crossplane 1.16 (end of life)](https://github.com/crossplane/crossplane/releases/tag/v1.16.0) stopped being maintained in February 2025, [Go 1.24 (end of life)](https://go.dev/doc/devel/release#go1.24.0) stopped on 10 February 2026, and the Kubernetes 1.29 clients (end of life) stopped on 28 February 2025, so the provider moved to [Upjet v2.5.1](https://github.com/crossplane/upjet/releases/tag/v2.5.1), [Crossplane 2.4.3](https://github.com/crossplane/crossplane/releases/tag/v2.4.3), and [Go 1.26.8](https://go.dev/doc/devel/release#go1.26.8), which is the current Upjet line and does not build on those old versions. Go's support rule is in the [Go release history](https://go.dev/doc/devel/release).

6. Controller timeout from 3 minutes to 30 minutes
   - A live create is no longer canceled at 180 seconds.

7. Secrets from plain attributes to Kubernetes secret references
   - Database password, Active-Active global password and client certificates, ACL user password, and cloud-account access key and console password.

8. Connection secret from no endpoint to `public_endpoint` and `private_endpoint`
   - The password stays on the secret key it already had.

9. Examples from YAML that could drift, to a checker over 16 examples
   - An unknown `forProvider` field is rejected.
   - The six new resources and the three lookups have examples.

10. Active-Active TLS from a mismatched `initProvider` to the same secret-reference list on create and update

11. Pro database passwordless
    - Redis Cloud documents [passwordless authentication for the default user](https://redis.io/docs/latest/operate/rc/security/database-security/block-public-endpoints/#turn-on-passwordless-authentication-for-the-default-user) on a Pro subscription whose public endpoint is blocked.
    - A nil field sends no `enable_passwordless` key and no password key.
    - Explicit true sends only `enable_passwordless`.

12. Later Terraform releases open a pull request
    - The workflow changes the pin and regenerates.
    - It does not merge, and it does not bump Crossplane, Go, or Upjet.
    - v2.19.3 arrived that way.

13. Testing from CI that never called Redis Cloud to a required check plus a nightly run on the QA account
    - The old CI already linted, ran unit tests, and checked examples. It did not call Redis Cloud, and a red CI could still merge.
    - `ci-success` is now the required check on main.
    - `make test` adds alignment guards, the subscription example Redis Cloud had rejected, connection-secret keys, passwordless behavior, lookup tests, and checks for the release-tracking, nightly, and `ci-success` scripts.
    - `govulncheck` runs in CI and fails when called code is affected by a vulnerability.
    - A nightly workflow builds the provider on a kind cluster and runs against the QA Redis Cloud account. It creates and deletes one ACL rule, one Pro subscription, and one database. It is not a required pull-request check.
    - That same live path was run by hand first: three ACL cycles, three Pro subscription cycles, and one database. Each was created, became Ready, and was deleted.
    - A coverage-percentage gate was not added. Generated files would dominate that number.

## Unchanged on purpose

- A database subscription id stays a number, so the author pastes it. Peering, private link, Private Service Connect, and transit gateway already use `subscriptionIdRef`.
- The other 26 data-source IDs stay pasted.

## Checked

- Generate against v2.19.2 produced 27 resources and 29 data sources. Unit tests, the provider build, lookup tests, and the example checker passed.
- `PaymentMethod`, `EssentialsPlan`, and `Regions` became Ready on a test cluster. Deleting them left the account payment methods unchanged.
- An ACL rule, a Pro subscription, and a Pro database were created, became Ready, and were deleted.
- A recount on 31 October 2026 with Terraform CLI 1.5.7 matched v2.19.3. There is no newer Terraform release.

The alignment doc that records this comparison is `docs/terraform-alignment.md`. Pull request 19 is merged. It updated that doc only, and it did not publish a package.
