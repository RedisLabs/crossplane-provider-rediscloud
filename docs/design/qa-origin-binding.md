# No-network QA origin comparison

Date: 2026-11-26. Status: Proposed. Ticket: RED-225344.

## TLDR

Compare the existing protected credential URL to an independently protected SHA-256 of an exact observed QA HTTPS origin without making requests or logging either input. A match is only origin binding; it is not an API read, account identity, runtime digest, response completeness, selector validation, or authorization to run QA.

## Behavior and security

An owner-only manual dispatch of the dedicated branch runs a Python standard-library comparison. The credential URL must be canonical HTTPS, have no userinfo, port, query or fragment and have exactly `/v1` or `/v1/` path. Invalid data or nonmatching digest fails closed. Input values, digest and exceptions are not printed. The expected digest must be established independently from verified topology and protected as `QA_TARGET_ORIGIN_SHA256`; this change does not configure it. Repository admins and anyone who can change the branch, workflow or secrets remain trusted; this is not a security boundary against them.

## Validation and rollout

Credential-free CI tests canonical matching, mismatch and malformed inputs; no Cloud credentials or network are needed for those tests. An independently reviewed exact commit and authorized protected target digest are prerequisites to dispatch. The manual step does not issue HTTP requests. Even a successful comparison does not establish current server image bytes, response completeness or safe selector discovery. Keep all live qualification gates in place. Roll back by disabling the dedicated job and removing its protected target after use; do not alter active cleanup runs.

## Open questions

How to independently bind observed tag to running image digest and protect the expected target digest; how to establish account-bound Pro identity and complete unique ACL selectors; and whether a version-specific API contract is acceptable to the responsible owner.

## Proposed bounded candidate comparison

When multiple independently observed origins need comparison, a separate no-network job may read at most 16 protected entries, each with a unique SHA-256 origin digest and a fresh opaque 128-bit random token. It refuses malformed/duplicate inputs and zero or multiple matches. It prints only the winning opaque token; the private token-to-origin mapping must remain outside the repository and public Actions logs. A token is pseudonymous, not encryption: anyone holding its map can infer the corresponding origin. Candidate identities must be independently observed and reviewed before configuring the secret or running the job. A matched candidate proves only URL-origin equality, not account ownership, live deployment, inventory completeness, or QA authorization. No candidate list or secret is configured by this proposal.
