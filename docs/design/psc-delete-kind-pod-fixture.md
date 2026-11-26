# PSC endpoint hold during provider pod replacement

TLDR: A hosted-only fake PSC API and disposable Kind cluster exercise the
installed opt-in PSC guard while its provider pod is replaced. Status: pending
hosted validation; 2026-11-26. Related: RED-225344 and
[PSC hold design](psc-endpoint-delete-conservative-hold.md).

The fixture pins the provider source and verifies its locally built package,
OCI digest and running provider binary. It uses only dummy credentials and a
node-local fake subscription, service, endpoint collection and task API. After
the fake accepts the exact initialized endpoint DELETE without responding, it
checks the pod-network source, held CR UID, exact finalizer and full marker,
and separately reads the authenticated fake parent and collection. It force
deletes the original pod, requires the old container absent and a replacement
with the same installed digest ready, then requires fresh replacement logs and
CR-scoped event/condition evidence. Bounded post-replacement checks require the
same marker and exactly one fake DELETE. Firewall probes are scoped to this
disposable test and do not establish complete egress isolation.

This deliberately does not claim a specific termination signal, live Cloud or
GCP cleanup, authorization against other writers, trusted markers, or a safe
recovery procedure. All fake resources are in the disposable runner; cleanup
removes test containers and rules. Rollback is to omit this test-only workflow.
Review and exact-head GitHub Actions validation are required before any claim.
