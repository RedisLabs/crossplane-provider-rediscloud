# ADR-0001: Follow Upjet v2 for the provider line

Status: Proposed.
Date: 28 October 2026.
Owner: Shay Berman.
Ticket: [RED-222728](https://redislabs.atlassian.net/browse/RED-222728).
Design: [provider-upgrade-plan.md](../design/provider-upgrade-plan.md).

## Context

The managed resources already match Terraform Redis Cloud provider 2.19.2. The controller binary was still Crossplane 1.16 and Upjet v1.4.1. Crossplane stopped maintaining 1.16 in February 2025. Go 1.24 stopped on 10 February 2026. The Kubernetes 1.29 clients stopped on 28 February 2025.

Upjet v2.5.1, read again on 28 October 2026, is the current GA module. It does not compile against the v1.4.1 module set. Its `go.mod` requires Go 1.26.8, crossplane-runtime v2.4.0, Crossplane APIs v2.4.1, controller-runtime v0.23.3, and Kubernetes clients v0.35.3. The compiler used to build that line is Go 1.26.10, the current 1.26 patch.

## Decision

Follow Upjet v2.5.1 and move Go, crossplane-runtime, the Crossplane APIs, controller-runtime, and the Kubernetes clients with it. Install Crossplane 2.4.3 on the test cluster. Keep Terraform CLI 1.5.7 and Terraform Redis Cloud provider 2.19.2.

Stay cluster-scoped for this pull request. `pipeline.Run` accepts a nil namespaced provider and then generates the existing 27 resources into `apis/` and `internal/controller/`. Managed APIs stay `v1alpha1`. ProviderConfig stays `v1beta1`. External Secret Store support is removed because Crossplane v2 dropped it.

The `up` CLI is not part of this build. The current Crossplane build scripts install the Crossplane CLI instead. This pull request uses Crossplane CLI v2.4.1, the latest published CLI on the 2.4 line. Core 2.4.3 does not have a CLI release.

## Alternatives

Stay on Upjet v1.4.1 and bump only Go. That was already the boundary of the previous line: Upjet v2 does not build there, and Go 1.24 is past end of support.

Generate namespaced managed resources in this same pull request. Upjet v2 supports that, and it would double the CRDs under `apis/namespaced` before the planned `v1beta1` promotion. This activity promotes the existing 27 kinds in the next pull request. Adding a second scope here would make clients change shape twice.

## Consequences

The provider package declares SafeStart. Clusters already running Crossplane 2.4 keep the cluster-scoped `v1alpha1` resources. `spec.publishConnectionDetailsTo` is not generated. Rollback is reverting this merge. The previous `main` is the Crossplane 1.16 provider.

## Revisit

Revisit namespaced managed resources when a consumer needs them, or when the `v1beta1` promotion is on `main` and a follow-up explicitly asks for the namespaced API group.
