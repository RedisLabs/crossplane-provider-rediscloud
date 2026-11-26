# Pro database source CIDR coverage

**TLDR:** Exercise explicit Pro database source CIDRs through the actual managed reconciler and pinned Terraform provider against a loopback fake API. This is simulated coverage, not Cloud qualification or network enforcement.

**Date:** 2026-11-26

**Status:** Draft

**Tracking:** RED-225344

## Scope

Terraform's Pro database CRUDI and source-IP acceptance cases exercise customer configuration against Redis Cloud. The Crossplane alpha lifecycle fixture exercises creation, update and deletion, but had no explicit source CIDR assertion. The added named hosted case sends a documentation-only CIDR at create, checks the fake Cloud request, updates it to a different documentation-only CIDR, checks the update request, and observes status after a fresh authenticated fake read. The fixture uses pinned Terraform 1.5.7 and Redis Cloud provider 2.20.0 and dummy credentials. Provider schema maps `source_ips`; the SDK writes `sourceIp` and observes `security.sourceIps`.

The fixture follows the existing lifecycle's exact fake child deletion and surviving fake parent checks. Its synthetic response is controlled by the test: it does not establish real Cloud defaults, endpoint exposure, network isolation, or whether any real Cloud resource was created or deleted. Terraform's private/public default-source-IP acceptance cases remain a live coverage gap. No Cloud account or network infrastructure is modified.

## Related coverage

Terraform Pro subscription/database CRUDI maps only partially to the #114 managed parent/database lifecycle and accepted-error fake cases. ACL rule/role/user CRUDI, PSC service/endpoint and PrivateLink map to separate #114 simulated fixtures. Region/payment data-source reads map to hosted simulated cases and separately guarded GET-only QA lookups. GCP/AWS Pro selection has fake request checks. Import, real provider/server behavior, full field coverage and live cleanup are not implied by these mappings. Shared-account credential authority and uncertain-delete recovery remain unresolved as documented in [alpha delivery integration](alpha-delivery-integration.md).

## Validation and rollout

The separate candidate workflow builds the unpublished Terraform fix at exact source commit `268f8ae4e7625e429bbe41325cf65d0543c05a7e`, records the binary SHA-256 and requires the same bytes before and after the managed fixture. It uses Terraform 1.5.7 and the fixture's 2.20.0 local mirror coordinate solely to satisfy the Upjet fixture. The binary is an unreleased candidate built from the 2.21.0 branch, not the released 2.20.0 package. The original 2.20.0 fixture failure remains valid. Even a passing candidate is not live Cloud qualification or a published release.

Run the named case and the inherited integration checks in GitHub Actions; require the named receipt so a skipped case cannot pass. Review independently. This adds no runtime behavior, account permissions, or deployment. Roll back by reverting the test and receipt. ADR: none, test coverage only. BDR: none.
