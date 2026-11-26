# Hosted fake Pro delete across provider Pod replacement

**TLDR:** Hosted CI installs an exact-source provider package into a disposable Kind cluster, enables the opt-in delete hold and force-deletes its Pod while a fake Cloud DELETE response is withheld.

**Date:** 2026-11-26  
**Status:** Draft; hosted verification pending  
**Ticket:** [RED-225344](https://redislabs.atlassian.net/browse/RED-225344)

The fixture builds the pinned product source and verifies package and OCI manifest hashes and the running provider image/binary against that build. It creates only disposable Kind resources and fixed fake credentials. A fake Cloud accepts one create and one exact database DELETE; the delete response is withheld. The test requires a persisted CR intent and finalizer, force-deletes the original provider Pod, checks its old runtime container is not running, then requires a distinct ready Pod using the same OCI digest. Repeated reads must show the same CR UID, intent/finalizer and one fake delete after the fake response is released.

The fake is a non-forwarding server with a closed request allowlist. A node firewall permits the disposable Pod CIDR only to the fake and Kubernetes API node ports, denies other destinations, and validates denial with independently reachable node and non-node fixtures plus packet accounting for the latter. The test requires IPv4-only Kind and an explicit fixed fake URL and dummy keys; it is not a general network policy proof. Firewall, fake services and node fixture cleanup run on exit.

This is forceful Kubernetes Pod replacement, not an assertion about the exact signal sent by Kubernetes. It does not establish live Redis Cloud behavior, other writer fencing, actual Cloud task completion, safe automatic recovery or approval to deploy the opt-in guard. The guard's trust and administrative resolution requirements remain in [its design](pro-delete-conservative-hold.md).

Validation requires independent review and an exact-head hosted Actions run. Roll back by removing the fixture and dedicated workflow; no live Cloud resources are created. The existing proposed guard ADR applies. No business decision is made.
