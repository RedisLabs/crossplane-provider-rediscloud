# ADR-0002: One-shot fences for QA recovery DELETEs

**Status:** Proposed. **Date:** 2026-11-24. **Decision owner:** Not yet established. **Ticket:** RED-225344.

## Context

A runner can disappear before, during, or after sending DELETE; task lists can lag and an ordinary rerun reuses the run ID. We must not infer that retrying the DELETE is safe from an absent task or an active resource. Crossplane's direct recovery is currently disabled.

## Proposed decision

Use an atomic, durable, per-account and per-resource one-shot fence acquired before each DELETE. A marker is never automatically cleared, including after success. A missing or ambiguous response fails closed. A separate marker is required for the child and parent; a parent DELETE additionally requires independently verified empty child inventory. Preserve manual recovery when a marker exists or the store's integrity cannot be verified. See [design](../design/strict-essentials-recovery.md).

GitHub refs are a candidate store, not an accepted production dependency: confirm atomic create semantics, readback, restrictions on update/delete and privileged bypass, authorized token scope, audit retention, key rotation, migration and recovery of old runs. The HMAC key and account binding must remain stable for the full fence lifetime. GitHub Actions artifacts expire and listing visibility can lag; they cannot alone provide a permanent atomic one-shot fence. A transactional external store with conditional create and independently enforced immutable retention is an alternative if GitHub cannot enforce those constraints.

GitHub's [ref API documentation](https://docs.github.com/en/rest/git/refs) requires repository Contents write permission to create a ref. Its [ruleset documentation](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets) supports tag update and deletion restrictions but allows configured bypass actors. A read-only repository ruleset inventory on 2026-11-24 returned one active branch ruleset and no visible tag ruleset. This observation is not proof of all organization-level policy or future configuration and does not establish the needed boundary. The proposed ref store must remain disconnected from DELETE until its effective policy, bypass actors and service identity are independently verified.

The documented rules are separately selectable: a tag ruleset could leave creation unrestricted while restricting updates and deletion to bypass actors. With no bypass, the writer should be unable to update or delete matching refs. This requires verification against effective rules, rather than assuming a no-bypass ruleset prevents an administrator from changing the ruleset itself. Ref creation still requires repository-wide Contents write permission, not a ref-scoped permission. A dedicated repository and dedicated GitHub App, or an equivalent conditional-write store, would isolate that authority; independently audited policy administration and durable tamper evidence remain necessary. The current QA recovery workflow explicitly requests read-only repository permissions; no credential or policy change is included in this proposal.

## Consequences and revisit

This favors safety over automatic cleanup: a crash after reservation but before DELETE may leave a paid QA resource requiring manual investigation. It creates an operational retention and audit obligation. Reject or replace this proposal if immutable retention, stable identities and least-privilege creation cannot be proved; do not enable direct recovery on the basis of the prototype or fake-store tests.
