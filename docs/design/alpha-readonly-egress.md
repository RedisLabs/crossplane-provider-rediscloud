# Alpha read-only lookup egress design

**TLDR:** The current route guard is fake-only and binds only loopback; it cannot authorize a QA lookup. Real installed-controller QA remains blocked until an enforced network boundary and its negative tests prove the controller cannot bypass the proxy.

- Date: 2026-11-25
- Status: proposed; no live QA credentials or requests
- Ticket: [RED-225344](https://redislabs.atlassian.net/browse/RED-225344)

## Problem and scope

Exercise Regions and PaymentMethod through the installed Crossplane package without permitting resource creation or mutation. Allowed Cloud operations are authenticated GET requests to exact `/v1/regions` and `/v1/payment-methods` routes. Existing account and credentials are inputs; account management and all live Cloud writes are excluded.

## Proposed boundary

The included prototype accepts only fake sentinel credentials, listens on `127.0.0.1`, and sends requests exclusively to a fake `127.0.0.1` upstream. It denies non-GET methods, alternate paths, query strings, bodies, non-sentinel credentials and non-200 responses; it never follows or forwards redirects. No headers, bodies, account IDs or credentials are logged. Hosted negative tests validate the proxy itself; they do not exercise the installed provider or network isolation.

A real-QA implementation needs a distinct, reviewed mode with a fixed independently verified QA HTTPS origin, credentials kept in a Kubernetes Secret, and no response/header/credential logging. It must never follow redirects, accept arbitrary destinations or forward disallowed methods, paths, or queries. A route guard alone is not an egress security boundary: a compromised or misconfigured provider could connect elsewhere.

## Mandatory hosted isolation proof before QA

1. Build and install the pinned #77 package in GitHub Actions Kind and record source, OCI digest, runtime image ID and artifact. A default Kind cluster alone is not evidence that NetworkPolicy is enforced.
2. Install a pinned network-policy-capable CNI with documented enforcement and prove its readiness. Configure provider-pod egress to the Kubernetes API and the reviewed proxy only; explicitly account for DNS, IPv4 and IPv6, proxy target IP changes, host networking and pod replacement. Permit proxy egress only to the verified QA origin. Deny everything else, including alternate Cloud origins and direct Cloud connections.
3. From the actual installed provider pod's network identity, prove the allowed proxy path works against a fake endpoint while direct fake-Cloud access, alternate egress and IPv6 bypass fail. Use controlled negative tests for POST, PUT, PATCH, DELETE, extra paths, encodings, queries, body, redirects and forbidden destinations. Confirm all deny results come from the enforcement boundary rather than an unavailable endpoint. Test policy after pod restart and replacement; no secrets are needed.
4. Validate the proxy's fixed-origin TLS, redirect denial, credential isolation, bounded errors and redaction using dummy data; independently verify complete request tracing. Only after all negative tests pass may a separately reviewed, GET-only QA run use the existing account. No resource lifecycle, cleanup API, or account management request is permitted.
5. Record exact source SHA, OCI digest, Actions run, CNI/version and sanitized request counts, negative results and controller status. Distinguish fake evidence from authenticated QA reads. Missing evidence, policy drift or uncertain network isolation means blocked.

### Node and rollout bypasses

Kubernetes explicitly permits traffic between a pod and its resident node regardless of NetworkPolicy; policies selecting a pod also combine additively. Neither a default-deny policy nor a policy-enforcing CNI alone proves that a provider cannot use a node-local relay. See the [Kubernetes NetworkPolicy semantics and limitations](https://kubernetes.io/docs/concepts/services-networking/network-policies/). The [Kubernetes SIG policy controller](https://kube-network-policies.sigs.k8s.io/docs/quick-start/) can enforce policies on a Kind cluster, but that capability is not proof that this exception is closed.

Before live credentials, the hosted proof must install an independently enforced node boundary, or establish another reviewed isolation mechanism, that denies provider-to-node relay traffic while preserving only the required API and proxy paths. The mechanism must be in place before the provider starts and must remain effective through policy convergence, node/pod restarts and replacement; it must not depend on an unverified interval after a pod is already running. Restrict who can change selectors, add permissive policies, use host networking, alter the proxy or modify the enforcement rules. Prove the provider pod's actual network identity and test a reachable node-local listener before and after enforcement, including alternate addresses and IPv6. A failure to connect is meaningful only if an independent control proves the listener is reachable. Also prove allowed proxy/API traffic still works. Preserve deny evidence without credentials or response content.

The Kubernetes API allowance is itself a potential relay: review and test the provider service account's effective permissions, including pod creation, exec, port-forward and service/pod proxy subresources, and prevent it from changing the policy or proxy. A successful network test does not establish those API authorization properties. A candidate node firewall must classify the provider before any credential is present, enforce both forwarded and node-local traffic, and fail closed if the pod IP, node, address family or firewall state changes. Static IP-only rules installed after startup do not satisfy this requirement.

The existing package test's ephemeral registry runs as a sidecar in the Crossplane pod. It must not be mistaken for a trusted general-purpose relay, and this installation topology cannot by itself establish the node boundary. Positive and negative probes, installed-provider identity, route enforcement and restart checks must all succeed in the same hosted run before any separate real-QA mode is proposed. The included fake proxy remains loopback-only and cannot be used as an installed-provider proxy as it stands.

The hosted OCI install workflow includes an initial, credential-free node-relay experiment. An ephemeral container in the installed provider pod uses its network namespace to reach a separately checked node listener; a host INPUT rule then blocks that one IPv4 listener port, and service-account relay permissions are queried. This deliberately tests only an individual port and current pod identity. It is not an allowlist, startup or replacement proof, IPv6 test, policy-controller test, API-permission denial gate, proxy test, or authorization to use QA credentials. Any failed probe fails the job; an allowed API permission is reported as an open risk, not a successful security boundary.

## Operations and rollback

The fake-only guard cannot reach QA and has no credential path. Rollback removes the draft workflow and fixture. Production deployment and publication are out of scope. No shared-account lock is claimed; GET-only QA requires no Cloud writer, but any future mutation is blocked by the existing serialization and recovery requirements. Open questions: approved hosted CNI and image pins; enforced policy for this environment; verified QA origin; safe read-only selector and retention/redaction of status.
