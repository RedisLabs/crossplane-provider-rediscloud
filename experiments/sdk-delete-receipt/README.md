# SDK delete receipt experiment

This test-only branch verifies a local upstream SDK proposal against public
SDK commit `2cb6b15f705ae2969253d6e3da7d870e7d22cc44`. The patch comes from
local commit `1c107b24a3abfa45f2a6cb650976e962946c1211`; its SHA-256 is
`280cf9dfdc653300124832ace1cbc4789ccc0755517417738cfffeb700d9ca27`.
The credential-free pull-request workflow fetches that immutable base and
base64-encoded patch, verifies the decoded hash, and runs only fake-transport
and loopback tests. It has no Cloud
credentials, no live Cloud calls and no write permissions. This branch does
not propose changing the Crossplane provider or publishing an SDK release.

The proposal exposes a single-attempt Pro database or PSC endpoint DELETE
and its received task ID. It is not replay protection: lost receipts, durable
journaling, account/target identity, old and external writer fencing, rollback,
and authoritative PSC absence remain unresolved. Do not enable it as a
production mitigation or treat fake results as live qualification.
