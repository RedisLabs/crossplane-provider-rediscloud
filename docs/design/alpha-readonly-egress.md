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

## Operations and rollback

The fake-only guard cannot reach QA and has no credential path. Rollback removes the draft workflow and fixture. Production deployment and publication are out of scope. No shared-account lock is claimed; GET-only QA requires no Cloud writer, but any future mutation is blocked by the existing serialization and recovery requirements. Open questions: approved hosted CNI and image pins; enforced policy for this environment; verified QA origin; safe read-only selector and retention/redaction of status.
