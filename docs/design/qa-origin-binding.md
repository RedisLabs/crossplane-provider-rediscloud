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

## Proposed ciphertext-only comparison

To avoid create-or-update races on a new repository secret, a separate manual job could compare the credential origin with the existing protected `QA_VERIFIED_ORIGIN_HASHES`, then encrypt the unique digest together with purpose, run ID, run attempt and exact source SHA using a reviewed RSA-3072 public key with OAEP SHA-256 and MGF1 SHA-256. **The secret-bearing job is disabled** until a tested authenticated reader is implemented and reviewed. The matching private key stays in a local file with restricted permissions; the job would emit only ciphertext as a one-day Actions artifact named with run and attempt. It never prints an origin, hash, plaintext or exception and makes no QA call. OAEP provides confidentiality, not sender authentication: anyone with the public key can construct ciphertext. The reader must independently obtain authenticated GitHub metadata and verify workflow identity, repository, actor, dispatch event, exact head SHA, run ID, attempt, job success, unique artifact ID and artifact download integrity before decryption, then compare the decrypted context to that independently verified metadata. The public key fingerprint must independently match the local private key before dispatch. Any mismatch or missing evidence blocks use. This relies on key integrity, private-key custody, branch/workflow protection, Actions artifacts and OpenSSL; ciphertext exposure is not an authorization boundary. A separate credential-free test generates a disposable key and checks round trip and randomized ciphertext. Security review, exact-head hosted tests and an independently authorized dispatch precede secret access.

If authorized later, privately decrypt once and compare against independently extracted origins from one pinned `cloud-apps-deployment` commit `48f6cb9e79ad00cbe0a43d991ecce68402a5df39`, requiring secured ingress, normal ingress and configured external host to agree and each candidate to be unique. The mapping remains private. A match identifies only a configured-origin candidate, not runtime version, account identity, response completeness, or permission to use QA; no match or ambiguous mapping blocks further steps.

The proposed `qa_origin_reader.py` uses authenticated GitHub API requests to verify the exact expected run, attempt, actor and triggering actor, branch, workflow, source SHA, successful job, unique artifact ID, metadata, and downloaded archive digest before checking the local private key and decrypted context. The reader opens the key without following a final-component symlink, requires a regular file owned by the caller with no group or world permissions, and passes the open file descriptor to OpenSSL. Parent directory integrity and independent review of the expected SHA remain operational prerequisites. It prints only verification status, not the digest; candidate mapping remains a separate unimplemented step. Its synthetic tests cover mismatched metadata, duplicate artifacts, altered archives, key permissions and ownership, wrong key, modified ciphertext and substituted contexts. The producer remains disabled until this reader and the full hosted checks receive independent review.
