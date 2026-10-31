# Test suites

This file describes the tests in this Crossplane provider: which suite runs when, and what each case verifies. Where the Terraform Redis Cloud provider already runs a live case this provider does not, that gap is named under the case. Terraform test names link to the test at [v2.19.3](https://github.com/RedisLabs/terraform-provider-rediscloud/releases/tag/v2.19.3). Crossplane test names link to the test on `main`.

## Contents

- [When](#when)
- [Terms](#terms)
- [CI](#ci)
  - [Jobs](#jobs)
  - [Unit cases](#unit-cases)
- [Acceptance smoke](#acceptance-smoke)
  - [Cases this provider runs](#cases-this-provider-runs)
  - [Gaps against the Terraform smoke](#gaps-against-the-terraform-smoke)
- [Acceptance](#acceptance)
  - [Gaps against Terraform on main](#gaps-against-terraform-on-main)
  - [Further Terraform cases](#further-terraform-cases)
- [Uptest](#uptest)
- [Wanted next](#wanted-next)

## When

| Suite | When this provider runs it | Terraform today |
| --- | --- | --- |
| [CI](#ci) | Every pull request. Required check [`ci-success`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/.github/workflows/ci.yml). No Redis Cloud account. | Every non-draft pull request, before smoke. Also no Redis Cloud account. |
| [Acceptance smoke](#acceptance-smoke) | Nightly at 06:17 UTC, and from Actions → Run workflow. [`scripts/qa_live.py`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/scripts/qa_live.py). One ACL rule, one Pro subscription, one Pro database. Create and delete. | Every pull request that changes code, unless it is a draft or labeled `skip-tests`. The slice is wider. See [gaps](#gaps-against-the-terraform-smoke). |
| [Acceptance](#acceptance) | Not scheduled. | Every push to `main`, plus a manual run. Wider than smoke. See [gaps](#gaps-against-terraform-on-main). |
| [Uptest](#uptest) | A write or admin comment on a pull request: `/test-examples="<path>"`. [`uptest-trigger.yaml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/.github/workflows/uptest-trigger.yaml). | Terraform has no Uptest. It uses the acceptance tests linked below. |

A red nightly run does not unmerge. The same choice is recorded in [testing-gate.md](testing-gate.md).

## Terms

- **CI.** Checks that do not call Redis Cloud.
- **Acceptance.** A test that creates real Redis Cloud resources.
- **Smoke.** The short acceptance slice. Here that is the nightly script.
- **CRUDI.** Terraform's name for one acceptance test that creates, reads, updates, deletes, and imports. This provider's smoke creates and deletes.
- **Uptest.** Applies the YAML named in a pull-request comment, waits until Ready, then deletes. Details are in [CI-TESTING.md](../CI-TESTING.md).

## CI

When: every pull request and every push to `main`. Required. No Redis Cloud account.

Workflow: [`.github/workflows/ci.yml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/.github/workflows/ci.yml). The unit cases run from `make test`.

### Jobs

- `lint` runs `golangci-lint`.
- `check-diff` checks that generated code matches `make generate`.
- `unit-tests` runs the [unit cases](#unit-cases).
- `local-deploy` installs the provider package on a local cluster.
- `check-examples` checks example YAML against the CRDs. [`scripts/check-examples.py`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/scripts/check-examples.py). It does not call Redis Cloud.
- `report-breaking-changes` checks CRD changes that would break an existing manifest.
- `publish-artifacts` builds the provider package.
- `govulncheck` fails when called code is affected by a known vulnerability.
- `ci-success` is the required check. A docs-only pull request skips the jobs above and still passes. The rule is tested by [`scripts/test_ci_success.py`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/scripts/test_ci_success.py).

### Unit cases

- [TestManagedResourceList](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/coverage_test.go) checks that all 27 managed resources are registered. A resource added in the Terraform provider has to be added here too.
- [TestNewResourcesReferenceASubscription](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/coverage_test.go) checks that the six resources added after Terraform v2.1.5 point at a parent subscription. Terraform's live tests for those resources are under [Further Terraform cases](#further-terraform-cases).
- [TestControllersUseThirtyMinuteTimeout](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/alignment_guard_test.go) checks that a generate run keeps the 30 minute controller timeout. Terraform has no Crossplane reconcile timeout.
- [TestActiveActiveTLSInitIsSecretRefList](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/alignment_guard_test.go) checks that Active-Active `clientTlsCertificatesSecretRef` stays a list of secret references. Gap: Terraform runs the live TLS cases in [TestAccResourceRedisCloudSubscriptionTls](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_pro_tls_test.go) on push to `main`. This provider has the unit check only.
- [TestDatabasesHaveNoSubscriptionIDRef](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/alignment_guard_test.go) and [TestStringChildrenKeepSubscriptionIDRef](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/alignment_guard_test.go) check that a database subscription id stays a number, and that string children such as peering keep `subscriptionIdRef`. Terraform interpolates the id in HCL and has no reference field.
- [TestSubscriptionExampleParameters](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/subscription_example_test.go) checks that the Pro subscription example includes a payment method id, a dataset size, and `operations-per-second`. The shape Redis Cloud rejected on 27 October 2026 fails this test. The live create is [TestAccResourceRedisCloudProSubscription_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_pro_subscription_test.go), which Terraform runs in pull-request smoke. This provider runs the live create in the [nightly smoke](#pro-subscription).
- [TestDatabaseConnectionDetails](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/configure_test.go) and [TestDatabaseConnectionDetailsWithoutPort](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/configure_test.go) check that the connection secret publishes `public_endpoint` and `private_endpoint`, and publishes `port` only when Terraform already returned it. The nightly smoke checks the same two key names on a real secret. Terraform reads the endpoints inside [TestAccResourceRedisCloudProDatabase_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_pro_database_test.go).
- [TestOmittedEnablePasswordlessSendsNoPassword](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/database_passwordless_test.go) and [TestEnablePasswordlessTrueSendsTheAttributeOnly](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/database_passwordless_test.go) check the Pro passwordless parameter map. Omitting the field sends no password key. Explicit `true` sends only `enable_passwordless`. Gap: Terraform's live cases are [TestAccRedisCloudProDatabase_Passwordless](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/pro/rediscloud_pro_database_passwordless_test.go) and [TestAccActiveActiveDatabase_Passwordless](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/activeactive/active_active_database_passwordless_test.go). Those are not in Terraform's pull-request smoke. This provider has no live passwordless run.
- [TestMatchPaymentMethod](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/internal/lookup/paymentmethod_test.go), [TestMatchEssentialsPlan](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/internal/lookup/essentialsplan_test.go), [TestMatchRegions](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/internal/lookup/regions_test.go), and [TestPaymentMethodReconcileWritesID](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/internal/controller/lookup/reconcile_test.go) check that the three lookups match an id and that an HTML error body is hidden. Terraform's live data sources are [TestAccDataSourceRedisCloudPaymentMethod_basic](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/paymentmethod/datasource_payment_method_test.go), [TestAccDataSourceRedisCloudEssentialsPlan_basic](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/essentialsplan/datasource_essentials_plan_test.go), and [TestAccDataSourceRedisCloudRegions_all](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/regions/datasource_regions_test.go), on push to `main`. This provider also waits for the three lookups to become Ready in the nightly smoke.
- [`scripts/test_qa_live.py`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/scripts/test_qa_live.py) checks the nightly script with a fake API: refuse a second `xp-ci-` subscription, delete the subscription when the database step fails, and require Cloud `active`, the two secret key names, and HTTP 404. Terraform has no equivalent script.
- [`scripts/test_track_terraform_release.py`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/scripts/test_track_terraform_release.py) checks release tracking: the same pin opens nothing, a newer tag would change only `TERRAFORM_PROVIDER_VERSION`, and a 3 minute timeout does not open a pull request. That workflow exists only in this repository.

## Acceptance smoke

When: nightly [`scripts/qa_live.py`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/scripts/qa_live.py), and by hand before a change to subscription or database behavior. Workflow: [`.github/workflows/qa-live.yaml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/.github/workflows/qa-live.yaml). Not a pull-request check.

Terraform runs its smoke on every code pull request. A Terraform case named CRUDI creates, reads, updates, deletes, and imports. The cases below create and delete.

### Cases this provider runs

- <a id="acl-rule"></a>**ACL rule.** Create a rule `+@read ~*`, see Cloud `active` and Ready, delete it, and see the name gone. Terraform runs the fuller lifecycle in pull-request smoke: [TestAccResourceRedisCloudAclRule_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_acl_rule_test.go). This provider covers create and delete. Update and import are a gap.
- <a id="pro-subscription"></a>**Pro subscription.** Create one Pro subscription with a payment method from the lookup, a dataset size, and `operations-per-second`. See Cloud `active` and Ready. Delete it and see HTTP 404. Terraform runs [TestAccResourceRedisCloudProSubscription_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_pro_subscription_test.go) in pull-request smoke. This provider covers create and delete.
- <a id="pro-database"></a>**Pro database.** Copy the numeric subscription id, create one database, see Cloud `active` and Ready, require secret keys `public_endpoint` and `private_endpoint`, delete it, and see HTTP 404. Terraform runs [TestAccResourceRedisCloudProDatabase_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_pro_database_test.go) in pull-request smoke. This provider covers create and delete. Uptest cannot copy the numeric id. The nightly script does.
- <a id="smoke-lookups"></a>**Lookups.** Payment method, Essentials plan, and region become Ready before the creates. They only read. Terraform runs [TestAccDataSourceRedisCloudPaymentMethod_basic](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/paymentmethod/datasource_payment_method_test.go), [TestAccDataSourceRedisCloudEssentialsPlan_basic](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/essentialsplan/datasource_essentials_plan_test.go), and [TestAccDataSourceRedisCloudRegions_all](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/regions/datasource_regions_test.go) on push to `main`, not in pull-request smoke.

### Gaps against the Terraform smoke

Terraform runs these on every code pull request. This provider does not run them.

- **Active-Active subscription.** Create, read, update, delete, and import one Active-Active subscription. [TestAccResourceRedisCloudActiveActiveSubscription_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_active_active_subscription_test.go). Needs more than one region.
- **Active-Active database.** The same lifecycle for one Active-Active database. [TestAccResourceRedisCloudActiveActiveDatabase_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/activeactive/rediscloud_active_active_database_test.go).
- **Active-Active regions.** Add or change a region on an Active-Active subscription. [TestAccResourceRedisCloudActiveActiveSubscriptionRegions_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_active_active_subscription_regions_test.go).
- **Active-Active default user.** Turn the default user on or off. [TestAccResourceRedisCloudActiveActiveDatabase_enableDefaultUser](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_active_active_database_enable_default_user_test.go).
- **Essentials subscription.** Create an Essentials subscription, including a free plan and paid plans. [TestAccResourceRedisCloudEssentialsSubscription](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_essentials_subscription_test.go). The QA account is enough.
- **Essentials database.** Create, read, update, delete, and import one Essentials database. [TestAccResourceRedisCloudEssentialsDatabase_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_essentials_database_test.go). The QA account is enough.
- **Private Service Connect.** Create a Private Service Connect service on a Pro subscription. [TestAccResourceRedisCloudPrivateServiceConnect_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_private_service_connect_test.go). Needs a GCP project.
- **Pro database Redis 8 upgrade.** Upgrade a Pro database to Redis 8. [TestAccResourceRedisCloudProDatabase_Redis8_Upgrade](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_pro_database_redis_8_test.go). The QA account is enough.
- **Block public endpoints.** Block public endpoints on a Pro database and on an Active-Active database. [TestAccRedisCloudProDatabase_BlockPublicEndpoints](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_pro_database_block_public_endpoints_test.go) and [TestAccActiveActiveSubscriptionDatabase_BlockPublicEndpoints](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_active_active_database_block_public_endpoints_test.go).

## Acceptance

When: not scheduled in this provider. Terraform runs the groups below on every push to `main`.

This provider's nightly smoke already covers one Pro subscription create and delete, one Pro database create and delete, and one ACL rule create and delete. The rows below are the rest of Terraform's `main` run.

### Gaps against Terraform on main

- **Databases.** Pro, Essentials, and Active-Active database acceptance tests, including data sources. The nightly case is [Pro database](#pro-database). Terraform's group starts at [TestAccResourceRedisCloudProDatabase_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_pro_database_test.go), [TestAccResourceRedisCloudEssentialsDatabase_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_essentials_database_test.go), and [TestAccResourceRedisCloudActiveActiveDatabase_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/activeactive/rediscloud_active_active_database_test.go).
- **Subscriptions.** Pro, Essentials, and Active-Active subscription acceptance tests. The nightly case is [Pro subscription](#pro-subscription). Terraform's group starts at [TestAccResourceRedisCloudProSubscription_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_pro_subscription_test.go), [TestAccResourceRedisCloudEssentialsSubscription](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_essentials_subscription_test.go), and [TestAccResourceRedisCloudActiveActiveSubscription_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_active_active_subscription_test.go).
- **TLS.** Enable TLS with a valid certificate, and reject an empty or invalid certificate. [TestAccResourceRedisCloudSubscriptionTls](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_pro_tls_test.go). This provider checks the secret-reference shape in [TestActiveActiveTLSInitIsSecretRefList](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/alignment_guard_test.go) and has no live TLS run.
- **Essentials plan.** The plan data source finds a plan, including an impossible filter. [TestAccDataSourceRedisCloudEssentialsPlan_basic](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/essentialsplan/datasource_essentials_plan_test.go). This provider's [TestMatchEssentialsPlan](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/internal/lookup/essentialsplan_test.go) and the [nightly lookup](#smoke-lookups) cover a successful match.
- **Persistence, modules, regions, and ACL.** Data-persistence and module data sources, region data sources, and ACL role, rule, and user. This provider runs the [ACL rule](#acl-rule) create and delete. Terraform also runs [TestAccResourceRedisCloudAclRole_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_acl_role_test.go) and [TestAccResourceRedisCloudAclUser_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_acl_user_test.go).
- **Cloud account.** Register a cloud account and read it back. [TestAccResourceRedisCloudCloudAccount_basic](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/cloudaccount/resource_cloud_account_test.go) and [TestAccDataSourceRedisCloudCloudAccount_basic](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/cloudaccount/datasource_cloud_account_test.go). Needs real cloud access keys.
- **Payment method.** The data source returns an id. [TestAccDataSourceRedisCloudPaymentMethod_basic](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/paymentmethod/datasource_payment_method_test.go). This provider's lookup becomes Ready in the [nightly smoke](#smoke-lookups).

### Further Terraform cases

These tests exist in the Terraform provider. A push to Terraform `main` does not select them, because the job pattern does not match the test name. This provider does not run them either.

- **Passwordless.** Empty password when passwordless is on, and a conflict when a password is also set. [TestAccRedisCloudProDatabase_Passwordless](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/pro/rediscloud_pro_database_passwordless_test.go) and [TestAccActiveActiveDatabase_Passwordless](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/activeactive/active_active_database_passwordless_test.go). This provider's unit cases are [TestOmittedEnablePasswordlessSendsNoPassword](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/database_passwordless_test.go) and [TestEnablePasswordlessTrueSendsTheAttributeOnly](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/config/database_passwordless_test.go).
- **Private link.** Create the private link for Pro and for Active-Active, including port consistency on Pro. [TestAccResourceRedisCloudPrivateLink_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_private_link_test.go) and [TestAccResourceRedisCloudActiveActivePrivateLink_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_active_active_private_link_test.go). Needs AWS.
- **VPC peering.** Pro and Active-Active, on AWS and on GCP. [TestAccResourceRedisCloudSubscriptionPeering_aws](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_subscription_peering_test.go) and [TestAccResourceRedisCloudActiveActiveSubscriptionPeering_aws](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_active_active_subscription_peering_test.go). Needs AWS or GCP.
- **Transit Gateway.** One test accepts the invitation and also creates the attachment and the route. [TestAccResourceRedisCloudTransitGatewayInvitationAcceptor_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_transit_gateway_invitation_acceptor_test.go) and [TestAccResourceRedisCloudActiveActiveTransitGatewayInvitationAcceptor_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_active_active_transit_gateway_invitation_acceptor_test.go). Needs an AWS invitation that already exists.
- **Private Service Connect endpoint and accepter.** Create the endpoint, and accept it, for Pro and for Active-Active. [TestAccResourceRedisCloudPrivateServiceConnectEndpoint_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_private_service_connect_endpoint_test.go) and [TestAccResourceRedisCloudPrivateServiceConnectEndpointAccepter_Create](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_private_service_connect_endpoint_accepter_test.go). Needs GCP.

## Uptest

When: a write or admin comment on a pull request, `/test-examples="<path>"`. Workflow: [`.github/workflows/uptest-trigger.yaml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/.github/workflows/uptest-trigger.yaml). Not required.

A passing path verifies that the provider installs, the manifest is accepted, the controller reaches Ready, and delete removes the object.

- [`examples/rediscloud/acl-rule.yaml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/examples/rediscloud/acl-rule.yaml). Same use case as the nightly [ACL rule](#acl-rule). Terraform: [TestAccResourceRedisCloudAclRule_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_acl_rule_test.go).
- [`examples/lookup/`](https://github.com/RedisLabs/crossplane-provider-rediscloud/tree/main/examples/lookup). Read-only payment method, Essentials plan, and region. Same use case as the [nightly lookups](#smoke-lookups).
- [`examples/rediscloud/subscription.yaml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/examples/rediscloud/subscription.yaml). Pro subscription. The payment method id in git is a placeholder, so Redis Cloud rejects it until that value is real. The nightly [Pro subscription](#pro-subscription) uses the id from the lookup. Terraform: [TestAccResourceRedisCloudProSubscription_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_pro_subscription_test.go).
- [`examples/rediscloud/database.yaml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/examples/rediscloud/database.yaml). Pro database. `subscriptionId` is the placeholder `123456`. The nightly [Pro database](#pro-database) copies the real id. Terraform: [TestAccResourceRedisCloudProDatabase_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_pro_database_test.go).
- [`examples/rediscloud/acl-user.yaml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/examples/rediscloud/acl-user.yaml). ACL user. The password comes from a Secret the example does not create. Terraform runs [TestAccResourceRedisCloudAclUser_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_acl_user_test.go) on push to `main`.
- [`examples/rediscloud/cloud-account.yaml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/examples/rediscloud/cloud-account.yaml). Cloud account. Needs real access keys. Terraform: [TestAccResourceRedisCloudCloudAccount_basic](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/cloudaccount/resource_cloud_account_test.go).
- [`examples/rediscloud/subscription-peering.yaml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/examples/rediscloud/subscription-peering.yaml). VPC peering. Needs AWS. Terraform: [TestAccResourceRedisCloudSubscriptionPeering_aws](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_subscription_peering_test.go).
- [`examples/private/link.yaml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/examples/private/link.yaml) and [`examples/active/private-link.yaml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/examples/active/private-link.yaml). Private link. The API server accepts the files. Terraform still requires `shareName` and one `principal` (`principal` and `principalType`). Active-Active also requires `regionId`. Terraform: [TestAccResourceRedisCloudPrivateLink_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_private_link_test.go) and [TestAccResourceRedisCloudActiveActivePrivateLink_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_active_active_private_link_test.go).
- [`examples/transit/`](https://github.com/RedisLabs/crossplane-provider-rediscloud/tree/main/examples/transit) and the Active-Active transit-gateway examples. Invitation and CIDR routes. The API server accepts the files. Terraform still requires `action` and `tgwInvitationId` on the acceptor, and `cidrs` and `tgwId` on the route. Active-Active also requires `regionId`. Terraform: [TestAccResourceRedisCloudTransitGatewayInvitationAcceptor_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_transit_gateway_invitation_acceptor_test.go).
- [`examples/providerconfig/providerconfig.yaml`](https://github.com/RedisLabs/crossplane-provider-rediscloud/blob/main/examples/providerconfig/providerconfig.yaml). Sample credential object. Leave it out of an Uptest path. Setup already creates `default` from `upbound-system/provider-secret`. This file would replace that and point at `crossplane-system/example-creds`. `secret.yaml.tmpl` is not selected, because the workflow only applies `*.yaml`.

## Wanted next

Keep the current when. [CI](#ci) on every pull request. The three-object [smoke](#acceptance-smoke) nightly, not on pull requests.

The next live cases that need only the QA account, in this order, if a later change asks for them:

- Essentials subscription and Essentials database. Terraform already runs both in pull-request smoke: [TestAccResourceRedisCloudEssentialsSubscription](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_essentials_subscription_test.go) and [TestAccResourceRedisCloudEssentialsDatabase_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/resource_rediscloud_essentials_database_test.go).
- ACL role and ACL user. Terraform runs them on push to `main`: [TestAccResourceRedisCloudAclRole_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_acl_role_test.go) and [TestAccResourceRedisCloudAclUser_CRUDI](https://github.com/RedisLabs/terraform-provider-rediscloud/blob/v2.19.3/provider/rediscloud_acl_user_test.go).
- Pro database Redis 8 upgrade, block public endpoints, and passwordless. Links are in [Gaps against the Terraform smoke](#gaps-against-the-terraform-smoke) and [Further Terraform cases](#further-terraform-cases).
- Active-Active subscription and database. Terraform runs both in pull-request smoke. This costs a multi-region subscription.

Peering, private link, Transit Gateway, and Private Service Connect stay out until an AWS or GCP account is part of the run. The Terraform tests are linked under [Further Terraform cases](#further-terraform-cases). The examples in this repository are not enough to pass them.
