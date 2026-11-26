# PSC endpoint delayed uncertain delete fixture

**TLDR:** A hosted loopback fixture checks whether the actual managed PSC endpoint reconciler avoids another DELETE after a fake accepted request returns 503 while the endpoint remains listed.

**Date:** 2026-11-26  
**Status:** Draft; hosted result pending  
**Ticket:** [RED-225344](https://redislabs.atlassian.net/browse/RED-225344)

The existing endpoint fixture covers normal deletion and uncertain creation, not a lost delete response with a still-visible endpoint. This adds a named CI-required case using the actual managed reconciler, pinned Terraform provider and fake Cloud. It requires the failed delete callback, retained managed finalizer, a provider-issued authenticated reread of the endpoint task/collection, exact one fake DELETE across bounded reconciles, and independently authenticated fake reads identifying the endpoint, parent PSC service and subscription. The modeled endpoint is collection-based; no individual-child 404 is claimed. Newly scheduled asynchronous operations must settle before counters are checked.

The fake retains the endpoint and gives the first DELETE an accepted-then-503 response. The exact Cloud semantics and the result of this hypothetical fault are unverified until hosted CI runs. A second DELETE is a failing safety observation, not a successful cleanup or live qualification. This case does not prove process restart, real Cloud task correlation, other writer fencing or safe automatic replay. It uses dummy credentials and loopback only. The existing Pro-only administrative hold does not cover PSC endpoints.

Validation is through the named hosted Actions receipt and independent review; no local acceptance or live Cloud access. Removing the test and workflow receipt is the rollback. No new architectural or business decision is made.
